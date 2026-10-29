package platform

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"
)

type fakeEmbedder struct {
	failing atomic.Bool
	calls   atomic.Int64
}

var fakeAxes = [][]string{
	{"car", "automobile", "vehicle"},
	{"apple", "fruit"},
	{"banana", "tropical"},
	{"database", "postgres"},
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.calls.Add(1)
	if f.failing.Load() {
		return nil, errors.New("fake provider is down")
	}
	v := make([]float32, len(fakeAxes)+1)
	for word := range strings.FieldsSeq(strings.ToLower(text)) {
		for i, axis := range fakeAxes {
			if slices.Contains(axis, strings.Trim(word, ".,")) {
				v[i]++
			}
		}
	}
	if slices.Max(v) == 0 {
		v[len(fakeAxes)] = 1
	}
	return v, nil
}

func (f *fakeEmbedder) Model() string { return "fake" }

type entrySpec struct {
	owner   Principal
	project uuid.UUID
	scope   string
	slug    string
	title   string
	body    string
	private bool
	verify  *time.Time
}

func (w *world) seedEntry(s entrySpec) uuid.UUID {
	w.t.Helper()
	project := cmp.Or(s.project, w.project)
	scope := cmp.Or(s.scope, ScopePersonal)
	verify := s.verify
	if scope == "global" && verify == nil {
		verify = new(time.Now().Add(90 * 24 * time.Hour))
	}
	var id uuid.UUID
	if err := w.db.Get(&id, `INSERT INTO entry (org_id, project_id, owner_id, scope, slug, title, body, private, verify_by, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		w.org, project, s.owner.MemberID, scope, s.slug, cmp.Or(s.title, s.slug), s.body, s.private, verify, s.owner.Actor()); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func hitSlugs(hits []EntryHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Slug)
	}
	slices.Sort(out)
	return out
}

func TestVault_VAULT_C20_SearchCoversWhatTheCallerSees(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	b := w.join("bob")
	other := w.newProject(w.org, "OPS")
	w.seedEntry(entrySpec{owner: a, slug: "a-note", body: "the deploy pipeline is slow"})
	w.seedEntry(entrySpec{owner: b, scope: ScopeProject, slug: "proj-note", body: "deploy runs at night"})
	w.seedEntry(entrySpec{owner: b, project: other, scope: "global", slug: "global-note", body: "deploy checklist for the org"})
	w.seedEntry(entrySpec{owner: b, slug: "b-note", body: "my deploy secrets"})

	hits := w.must(w.p.Search(t.Context(), a, "APP", "deploy", SearchOpts{}))
	if got, want := hitSlugs(hits), []string{"a-note", "global-note", "proj-note"}; !slices.Equal(got, want) {
		t.Fatalf("hits = %v, want %v", got, want)
	}
}

func TestVault_VAULT_C21_QuerySyntaxIsTakenAsWords(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	w.seedEntry(entrySpec{owner: a, slug: "foo-note", body: "foo and bar together"})
	for _, q := range []string{`foo & !bar | (`, `"unclosed`, `:*`, `&&&`, `!`} {
		hits, err := w.p.Search(t.Context(), a, "APP", q, SearchOpts{})
		if err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		if hits == nil {
			t.Fatalf("query %q: nil list, want a list", q)
		}
	}
}

func TestVault_VAULT_C22_HybridFindsVectorNeighbour(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	emb := &fakeEmbedder{}
	w.p.UseEmbedder(emb)
	w.seedEntry(entrySpec{owner: a, slug: "wheels", body: "an automobile needs new tyres"})
	w.seedEntry(entrySpec{owner: a, slug: "fruit", body: "an apple a day"})
	w.must(w.p.EmbedPending(t.Context()))

	hits := w.must(w.p.Search(t.Context(), a, "APP", "car", SearchOpts{}))
	if got := hitSlugs(hits); !slices.Equal(got, []string{"wheels"}) {
		t.Fatalf("hybrid hits = %v, want [wheels]", got)
	}
	fts := w.must(w.p.Search(t.Context(), a, "APP", "car", SearchOpts{Method: MethodFTS}))
	if len(fts) != 0 {
		t.Fatalf("fts hits = %v, want none", hitSlugs(fts))
	}
}

func TestVault_VAULT_C23_FailingProviderKeepsWritesAndRetries(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	emb := &fakeEmbedder{}
	emb.failing.Store(true)
	w.p.UseEmbedder(emb)
	id := w.seedEntry(entrySpec{owner: a, slug: "pg", body: "postgres vacuum tuning"})

	if _, err := w.p.EmbedPending(t.Context()); err == nil {
		t.Fatal("EmbedPending with a failing provider: want an error")
	}
	var embedErr *string
	if err := w.db.Get(&embedErr, `SELECT embed_error FROM entry WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if embedErr == nil {
		t.Fatal("embed_error is empty after a failed embed")
	}
	hits := w.must(w.p.Search(t.Context(), a, "APP", "vacuum", SearchOpts{}))
	if got := hitSlugs(hits); !slices.Equal(got, []string{"pg"}) {
		t.Fatalf("search while the provider fails = %v, want [pg]", got)
	}

	emb.failing.Store(false)
	if n := w.must(w.p.EmbedPending(t.Context())); n != 1 {
		t.Fatalf("embedded %d entries, want 1", n)
	}
	var row struct {
		Embedded int64   `db:"embedded_version"`
		Version  int64   `db:"version"`
		Err      *string `db:"embed_error"`
	}
	if err := w.db.Get(&row, `SELECT embedded_version, version, embed_error FROM entry WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if row.Embedded != row.Version || row.Err != nil {
		t.Fatalf("after retry: %+v, want embedded at its version and no error", row)
	}
	if n := w.count("entry_chunk"); n == 0 {
		t.Fatal("no chunks after the provider works again")
	}
	hits = w.must(w.p.Search(t.Context(), a, "APP", "database", SearchOpts{Method: MethodVector}))
	if got := hitSlugs(hits); !slices.Equal(got, []string{"pg"}) {
		t.Fatalf("vector hits = %v, want [pg]", got)
	}
}

func TestVault_VAULT_C24_ReplacedBodyDropsOldChunks(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	emb := &fakeEmbedder{}
	w.p.UseEmbedder(emb)
	id := w.seedEntry(entrySpec{owner: a, slug: "snack", body: "an apple"})
	w.must(w.p.EmbedPending(t.Context()))
	var old []int64
	if err := w.db.Select(&old, `SELECT id FROM entry_chunk WHERE entry_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if len(old) == 0 {
		t.Fatal("no chunks after the first embed")
	}
	if got := hitSlugs(w.must(w.p.Search(t.Context(), a, "APP", "fruit", SearchOpts{Method: MethodVector}))); !slices.Equal(got, []string{"snack"}) {
		t.Fatalf("vector hits before the edit = %v, want [snack]", got)
	}

	w.exec(`UPDATE entry SET body = 'a banana', version = version + 1 WHERE id = $1`, id)
	if got := w.must(w.p.Search(t.Context(), a, "APP", "fruit", SearchOpts{Method: MethodVector})); len(got) != 0 {
		t.Fatalf("vector hits for the old text before the job = %v, want none", hitSlugs(got))
	}
	w.must(w.p.EmbedPending(t.Context()))

	var left int
	if err := w.db.Get(&left, `SELECT count(*) FROM entry_chunk WHERE id = ANY($1)`, old); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d old chunks left, want 0", left)
	}
	if got := w.must(w.p.Search(t.Context(), a, "APP", "fruit", SearchOpts{Method: MethodVector})); len(got) != 0 {
		t.Fatalf("vector hits for the old text = %v, want none", hitSlugs(got))
	}
	if got := hitSlugs(w.must(w.p.Search(t.Context(), a, "APP", "tropical", SearchOpts{Method: MethodVector}))); !slices.Equal(got, []string{"snack"}) {
		t.Fatalf("vector hits for the new text = %v, want [snack]", got)
	}
}
