package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/resolve"
)

type mergeFixture struct {
	t                   *testing.T
	c                   *Core
	root                string
	api, mono           Project
	apiBoard, monoBoard Board
}

func newMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	root := t.TempDir()
	tc := testCore(t)
	c := New(tc.db, tc.clock, tc.actor, root)
	f := &mergeFixture{t: t, c: c, root: root}
	f.api, f.apiBoard = f.project("API")
	f.mono, f.monoBoard = f.project("MONO")
	return f
}

func (f *mergeFixture) project(key string) (Project, Board) {
	f.t.Helper()
	p, err := f.c.CreateProject(f.t.Context(), key, true)
	if err != nil {
		f.t.Fatal(err)
	}
	b, err := f.c.SelectBoard(f.t.Context(), p.ID, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return p, b
}

func (f *mergeFixture) board(p Project, name string) Board {
	f.t.Helper()
	b, err := f.c.CreateBoard(f.t.Context(), p.ID, name, true)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *mergeFixture) card(p Project, b Board, title string, labels, tags []string) Card {
	f.t.Helper()
	card, err := f.c.CreateCard(f.t.Context(), p.ID, b.ID, NewCard{Title: title, Labels: labels, Tags: tags})
	if err != nil {
		f.t.Fatal(err)
	}
	return card
}

func (f *mergeFixture) merge(opts MergeOptions) MergePlan {
	f.t.Helper()
	plan, err := f.c.MergeProjects(f.t.Context(), "api", "mono", opts)
	if err != nil {
		f.t.Fatalf("MergeProjects: %v", err)
	}
	return plan
}

func (f *mergeFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.c.db.Get(&n, q, args...); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *mergeFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.c.db.Exec(q, args...); err != nil {
		f.t.Fatal(err)
	}
}

// snapshot is every row count a merge could change, and the hash of every
// file under root. A hash, not a length, so a rewrite that swaps one
// same-length reference for another still shows up as a change.
func (f *mergeFixture) snapshot() string {
	f.t.Helper()
	var b strings.Builder
	for _, table := range []string{"project", "board", "column_", "card", "label", "tag", "card_label",
		"card_tag", "entry", "entry_label", "artifact", "link", "event", "merged_project", "project_config"} {
		fmt.Fprintf(&b, "%s=%d ", table, f.count("SELECT count(*) FROM "+table))
	}
	filepath.WalkDir(f.root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "backups" {
			return filepath.SkipDir // an applied merge writes one; it is not state
		}
		if err == nil && !d.IsDir() {
			h, herr := fileHash(path)
			if herr != nil {
				f.t.Fatal(herr)
			}
			fmt.Fprintf(&b, "\n%s %s", path, h)
		}
		return nil
	})
	return b.String()
}

func TestMergeMovesBoardsCardsLabelsAndTags(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	f.board(f.api, "Web")
	f.board(f.mono, "Web")
	if _, err := f.c.CreateLabel(ctx, f.api.ID, "infra", "Infrastructure work"); err != nil {
		t.Fatal(err)
	}
	first := f.card(f.api, f.apiBoard, "first", []string{"bug", "infra"}, []string{"x"})
	f.card(f.api, f.apiBoard, "second", nil, nil)
	f.card(f.mono, f.monoBoard, "mono one", nil, nil)

	plan := f.merge(MergeOptions{Apply: true})

	wantBoards := []BoardMove{
		{Name: "api", Slug: "api", NewName: "api", NewSlug: "api"},
		{Name: "Web", Slug: "web", NewName: "Web (API)", NewSlug: "api-web"},
	}
	if !slices.Equal(plan.Boards, wantBoards) {
		t.Errorf("boards = %+v", plan.Boards)
	}
	if plan.Cards != (CardMoves{Moved: 2, FirstSeq: 2}) {
		t.Errorf("cards = %+v", plan.Cards)
	}
	if !slices.Contains(plan.Labels.Moved, "infra") || !slices.Contains(plan.Labels.Folded, "bug") {
		t.Errorf("labels = %+v", plan.Labels)
	}
	if !slices.Equal(plan.Tags.Moved, []string{"x"}) {
		t.Errorf("tags = %+v", plan.Tags)
	}

	got, err := f.c.GetCard(ctx, f.mono.ID, ParseCardRef("API-1"))
	if err != nil || got.ID != first.ID {
		t.Fatalf("API-1 in MONO = %+v, %v", got, err)
	}
	if !slices.Equal(got.Labels, []string{"bug", "infra"}) || !slices.Equal(got.Tags, []string{"x"}) {
		t.Errorf("labels %v, tags %v", got.Labels, got.Tags)
	}
	if next := f.card(f.mono, f.monoBoard, "after", nil, nil); next.Ref != "MONO-4" {
		t.Errorf("next MONO card = %s, want MONO-4", next.Ref)
	}
	if _, err := f.c.ProjectByKey(ctx, "API"); errCode(t, err) != "project_merged" {
		t.Errorf("API after the merge: %v", err)
	}
	boards, err := f.c.ListBoards(ctx, f.mono.ID)
	if err != nil || len(boards) != 4 {
		t.Fatalf("MONO boards = %+v, %v", boards, err)
	}
	for _, b := range boards {
		if b.IsDefault != (b.Name == "mono") {
			t.Errorf("board %s default = %v", b.Name, b.IsDefault)
		}
	}
	if n := f.count(`SELECT count(*) FROM label WHERE project_id = ? AND name = 'bug'`, f.mono.ID); n != 1 {
		t.Errorf("MONO has %d bug labels", n)
	}
	if n := f.count(`SELECT count(*) FROM label WHERE project_id = ?`, f.api.ID); n != 0 {
		t.Errorf("%d labels still belong to API", n)
	}
}

func TestMergePlanChangesNothingAndMatchesApply(t *testing.T) {
	f := newMergeFixture(t)
	f.board(f.mono, "api") // forces a board rename
	f.card(f.api, f.apiBoard, "first", []string{"bug"}, nil)
	before := f.snapshot()

	plan := f.merge(MergeOptions{})
	if !plan.Ready || plan.Backup != "" || plan.Refused != "" {
		t.Errorf("plan = %+v", plan)
	}
	if after := f.snapshot(); after != before {
		t.Errorf("plan mode changed something:\nbefore %s\nafter  %s", before, after)
	}

	applied := f.merge(MergeOptions{Apply: true})
	if applied.Backup == "" {
		t.Error("apply reported no backup")
	}
	applied.Backup, applied.Warnings = "", nil
	if !reflect.DeepEqual(plan, applied) {
		t.Errorf("plan and apply differ:\nplan    %+v\napplied %+v", plan, applied)
	}
}

func TestMergeRefusals(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()

	plan, err := f.c.MergeProjects(ctx, "API", "api", MergeOptions{})
	if err != nil || plan.Ready || plan.Refused == "" {
		t.Errorf("into itself: %+v, %v", plan, err)
	}
	_, err = f.c.MergeProjects(ctx, "API", "API", MergeOptions{Apply: true})
	if got := errCode(t, err); got != "merge_refused" {
		t.Errorf("into itself, apply: %s", got)
	}
	_, err = f.c.MergeProjects(ctx, "API", "NOPE", MergeOptions{})
	if got := errCode(t, err); got != "project_not_found" {
		t.Errorf("missing DST: %s", got)
	}

	claimed := f.card(f.api, f.apiBoard, "claimed", nil, nil)
	f.exec(`UPDATE card SET claimed_by = 'agent:x', claim_until = ? WHERE id = ?`, f.c.clock.NowMS()+60_000, claimed.ID)
	if plan := f.merge(MergeOptions{}); plan.Ready || !strings.Contains(plan.Refused, "claimed") {
		t.Errorf("active claim: %+v", plan)
	}
	_, err = f.c.MergeProjects(ctx, "API", "MONO", MergeOptions{Apply: true})
	if got := errCode(t, err); got != "merge_refused" {
		t.Errorf("active claim, apply: %s", got)
	}
	if f.count(`SELECT count(*) FROM project WHERE key = 'API'`) != 1 {
		t.Error("a refused merge removed API")
	}
	f.exec(`UPDATE card SET claimed_by = NULL, claim_until = NULL WHERE id = ?`, claimed.ID)

	// A key from before the key grammar, with a card whose ref carries it.
	f.exec(`INSERT INTO project (id, key, name, created_at) VALUES ('odd', 'MY_APP', 'MY_APP', 1)`)
	oddBoard := f.board(Project{ID: "odd", Key: "MY_APP"}, "odd")
	legacy := f.card(Project{ID: "odd", Key: "MY_APP"}, oddBoard, "legacy", nil, nil)
	if legacy.Ref != "MY_APP-1" {
		t.Fatalf("legacy ref = %s", legacy.Ref)
	}
	plan, err = f.c.MergeProjects(ctx, "API", "MY_APP", MergeOptions{})
	if err != nil || !strings.Contains(plan.Refused, "MY_APP") {
		t.Errorf("DST without a key a marker can name: %+v, %v", plan, err)
	}
	if _, err := f.c.MergeProjects(ctx, "MY_APP", "MONO", MergeOptions{Apply: true}); err != nil {
		t.Fatalf("a SRC without a key a marker can name must merge: %v", err)
	}
	for _, ref := range []string{"MY_APP-1", "/MONO/cards/MY_APP-1"} {
		if got, err := f.c.GetCard(ctx, f.mono.ID, ParseCardRef(ref)); err != nil || got.ID != legacy.ID {
			t.Errorf("%s after the merge = %+v, %v", ref, got, err)
		}
	}
}

func TestMergeListsDroppedConfigAndBacksUp(t *testing.T) {
	f := newMergeFixture(t)
	f.exec(`INSERT INTO project_config (project_id, key, value, updated_at) VALUES
	          (?, 'claim.ttl', '10m', 1), (?, 'card.ls_limit', '20', 1), (?, 'search.method', 'fts', 1),
	          (?, 'claim.ttl', '10m', 1), (?, 'card.ls_limit', '50', 1)`,
		f.api.ID, f.api.ID, f.api.ID, f.mono.ID, f.mono.ID)

	plan := f.merge(MergeOptions{Apply: true})

	want := []ConfigDrop{{Key: "card.ls_limit", Src: "20", Dst: "50"}, {Key: "search.method", Src: "fts", Dst: ""}}
	if !slices.Equal(plan.ConfigDropped, want) {
		t.Errorf("config dropped = %+v", plan.ConfigDropped)
	}
	if n := f.count(`SELECT count(*) FROM project_config WHERE project_id = ?`, f.mono.ID); n != 2 {
		t.Errorf("MONO has %d overrides, want its own 2", n)
	}
	if !strings.HasPrefix(plan.Backup, filepath.Join(f.root, "backups", "merge-API-into-MONO-")) {
		t.Errorf("backup = %s", plan.Backup)
	}
	if _, err := os.Stat(filepath.Join(plan.Backup, "trellis.db")); err != nil {
		t.Errorf("backup database: %v", err)
	}
}

func TestMergeRepointsEarlierMerges(t *testing.T) {
	f := newMergeFixture(t)
	f.project("CORE")
	f.merge(MergeOptions{Apply: true})
	if _, err := f.c.MergeProjects(t.Context(), "MONO", "CORE", MergeOptions{Apply: true}); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.ProjectByKey(t.Context(), "API")
	if got := errCode(t, err); got != "project_merged" || !strings.Contains(err.Error(), "CORE") {
		t.Errorf("API after two merges: %v", err)
	}
}

func TestMergePlansMarkerRewrites(t *testing.T) {
	f := newMergeFixture(t)
	f.board(f.api, "Web")
	markers := []resolve.Marker{
		{Path: "/r/api/.trellis", Target: address.Project("API")},
		{Path: "/r/web/.trellis", Target: address.Board("API", "web")},
		{Path: "/r/gone/.trellis", Target: address.Board("API", "gone")},
		{Path: "/r/.trellis", Target: address.Project("MONO")},
	}
	plan := f.merge(MergeOptions{ScanRoot: "/r", Markers: markers, UnreadableMarkers: []string{"/r/bad/.trellis"}})
	want := []MarkerRewrite{
		{Path: "/r/api/.trellis", From: "/API", To: "/MONO/boards/api"},
		{Path: "/r/web/.trellis", From: "/API/boards/web", To: "/MONO/boards/web"},
	}
	if plan.Markers.ScanRoot != "/r" || !slices.Equal(plan.Markers.Rewrite, want) ||
		!slices.Equal(plan.Markers.Left, []string{"/r/gone/.trellis", "/r/bad/.trellis"}) {
		t.Errorf("markers = %+v", plan.Markers)
	}
}

// A merged card keeps its revisions and its relations, and its history moves
// with it: DST's event feed shows what happened to the card before the merge.
func TestMergeCarriesCardRevisionsRelationsAndHistory(t *testing.T) {
	f := newMergeFixture(t)
	ctx := t.Context()
	a := f.card(f.api, f.apiBoard, "a", nil, nil)
	b := f.card(f.api, f.apiBoard, "b", nil, nil)
	if _, err := f.c.EditCard(ctx, f.api.ID, ParseCardRef(a.Ref), CardEdit{Title: new("a, renamed"), IfVersion: &a.Version}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.RelateCards(ctx, f.api.ID, ParseCardRef(a.Ref), "resolved_by", ParseCardRef(b.Ref)); err != nil {
		t.Fatal(err)
	}
	apiEvents := f.count(`SELECT count(*) FROM event WHERE project_id = ?`, f.api.ID)
	if apiEvents == 0 {
		t.Fatal("no API events to carry")
	}
	monoEvents := f.count(`SELECT count(*) FROM event WHERE project_id = ?`, f.mono.ID)

	f.merge(MergeOptions{Apply: true})

	revs, err := f.c.ListCardRevisions(ctx, f.mono.ID, ParseCardRef(a.Ref))
	if err != nil || len(revs) == 0 {
		t.Errorf("revisions of %s after the merge = %+v, %v", a.Ref, revs, err)
	}
	rels, err := f.c.CardRelations(ctx, a.ID)
	if err != nil || len(rels) != 1 || rels[0].Rel != "resolved_by" || rels[0].Ref != b.Ref {
		t.Errorf("relations of %s after the merge = %+v, %v", a.Ref, rels, err)
	}
	if n := f.count(`SELECT count(*) FROM event WHERE project_id = ?`, f.api.ID); n != 0 {
		t.Errorf("%d events still name API", n)
	}
	// Every API event moved, plus the merge's own event on MONO.
	if n := f.count(`SELECT count(*) FROM event WHERE project_id = ?`, f.mono.ID); n < monoEvents+apiEvents+1 {
		t.Errorf("MONO has %d events, want at least %d", n, monoEvents+apiEvents+1)
	}
}

func TestMergeFinishesAfterTheCommit(t *testing.T) {
	f := newMergeFixture(t)
	f.entry(f.api, "Runbook", "x\n")
	repo := t.TempDir()
	markerPath := filepath.Join(repo, "api", ".trellis")
	writeFile(t, markerPath, "/API\n")
	var notified []string
	f.c.SetEntryChanged(func(_ context.Context, id string) error {
		notified = append(notified, id)
		return nil
	})

	plan := f.merge(MergeOptions{Apply: true, ScanRoot: repo,
		Markers: []resolve.Marker{{Path: markerPath, Target: address.Project("API")}}})

	if got := readFile(t, markerPath); got != "/MONO/boards/api\n" {
		t.Errorf("marker = %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.root, "projects", "API")); !os.IsNotExist(err) {
		t.Errorf("API's directory is still in the storage root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plan.Backup, "leftover", "API")); err != nil {
		t.Errorf("API's leftovers were not kept with the backup: %v", err)
	}
	if !slices.Contains(notified, f.mono.ID) {
		t.Errorf("MONO's derived state was not refreshed: %v", notified)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

// A marker changed since the plan is left alone and reported.
func TestMergeLeavesAMarkerThatChanged(t *testing.T) {
	f := newMergeFixture(t)
	repo := t.TempDir()
	markerPath := filepath.Join(repo, "api", ".trellis")
	writeFile(t, markerPath, "/OTHER\n")

	plan := f.merge(MergeOptions{Apply: true, ScanRoot: repo,
		Markers: []resolve.Marker{{Path: markerPath, Target: address.Project("API")}}})

	if got := readFile(t, markerPath); got != "/OTHER\n" {
		t.Errorf("marker = %q, want it untouched", got)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "OTHER") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

func TestMergeReportsAFailedRefresh(t *testing.T) {
	f := newMergeFixture(t)
	f.c.SetEntryChanged(func(context.Context, string) error { return errors.New("embedder down") })
	plan := f.merge(MergeOptions{Apply: true})
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "embedder down") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

func TestMergeDropsDerivedStateBeforeApplying(t *testing.T) {
	f := newMergeFixture(t)
	var dropped []string
	f.c.SetDropDerived(func(_ context.Context, key string) error {
		if f.count(`SELECT count(*) FROM project WHERE key = ?`, key) != 1 {
			t.Errorf("%s was dropped after it was merged away", key)
		}
		dropped = append(dropped, key)
		return errors.New("vector tables busy")
	})

	plan := f.merge(MergeOptions{Apply: true})

	if !slices.Equal(dropped, []string{"API"}) {
		t.Errorf("dropped = %v", dropped)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "vector tables busy") {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

// A comment on a merged card is reported under the card's own ref, not a
// key-seq pair the card does not answer to.
func TestFeedNamesACommentOnAMergedCardByItsRef(t *testing.T) {
	f := newMergeFixture(t)
	card := f.card(f.api, f.apiBoard, "moved", nil, nil)
	f.card(f.mono, f.monoBoard, "already here", nil, nil)
	f.merge(MergeOptions{Apply: true})
	if _, err := f.c.CreateComment(t.Context(), card.ID, "after the merge"); err != nil {
		t.Fatal(err)
	}
	events, _, err := f.c.EventFeed(t.Context(), EventQuery{ProjectID: f.mono.ID, Kinds: []string{"comment"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Ref != card.Ref {
		t.Fatalf("comment events = %+v, want ref %s", events, card.Ref)
	}
}
