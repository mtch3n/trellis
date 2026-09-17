package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// cardFixture is a server with one project, one board and one card on it.
type cardFixture struct {
	server  *Server
	core    *core.Core
	project core.Project
	card    core.Card
	request func(method, path, body string) *httptest.ResponseRecorder
}

func newCardFixture(t *testing.T, key string) cardFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "human:test", dir)
	p, err := c.CreateProject(ctx, key, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(ctx, p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, core.NewCard{Title: "deploy the thing"})
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	return cardFixture{
		server: s, core: c, project: p, card: card,
		request: func(method, path, body string) *httptest.ResponseRecorder {
			t.Helper()
			req := httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			return rec
		},
	}
}

func (f cardFixture) cardPath(suffix string) string {
	return "/api/p/" + f.project.Key + "/b/default/cards/" + f.card.Ref + suffix
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not the expected shape: %v\n%s", err, rec.Body)
	}
	return out
}

// An archived card leaves the board and comes back on restore. The archived
// list is its own view, so archived work never sits in a live column.
func TestArchiveAndRestoreCard(t *testing.T) {
	f := newCardFixture(t, "ARCH")

	rec := f.request(http.MethodPost, f.cardPath("/archive"), "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("archive = %d: %s", rec.Code, rec.Body)
	}
	if archived := decode[core.Card](t, rec); archived.ArchivedAt == nil {
		t.Errorf("the archived card has no archived_at: %s", rec.Body)
	}

	live := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/ARCH/b/default/cards", ""))
	for _, column := range live {
		if len(column.Cards) != 0 {
			t.Errorf("an archived card is still on the board: %+v", column)
		}
	}
	shelved := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/ARCH/b/default/cards?archived=1", ""))
	found := 0
	for _, column := range shelved {
		for _, card := range column.Cards {
			if card.Ref == f.card.Ref && card.ArchivedAt != nil {
				found++
			}
		}
	}
	if found != 1 {
		t.Errorf("?archived=1 returned %d copies of the card: %s", found, "expected exactly one")
	}

	if rec := f.request(http.MethodPost, f.cardPath("/restore"), "{}"); rec.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", rec.Code, rec.Body)
	}
	back := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/ARCH/b/default/cards", ""))
	total := 0
	for _, column := range back {
		total += len(column.Cards)
	}
	if total != 1 {
		t.Errorf("the restored card is not back on the board: %+v", back)
	}
}

// A card cites an entry, the detail lists it, and unlinking takes it back off.
func TestLinkCardToEntryAndBack(t *testing.T) {
	f := newCardFixture(t, "CITE")
	if rec := f.request(http.MethodPost, "/api/p/CITE/b/default/vault",
		`{"title":"Rollback runbook"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create entry = %d: %s", rec.Code, rec.Body)
	}

	rec := f.request(http.MethodPost, f.cardPath("/links"), `{"target":"rollback-runbook"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("link = %d: %s", rec.Code, rec.Body)
	}
	links := decode[[]core.CardLink](t, rec)
	if len(links) != 1 || links[0].To == nil || *links[0].To != "/CITE/vault/rollback-runbook" {
		t.Fatalf("link response = %+v", links)
	}

	detail := decode[cardDetail](t, f.request(http.MethodGet, f.cardPath(""), ""))
	if len(detail.Links) != 1 || detail.Links[0].Title != "Rollback runbook" {
		t.Errorf("the card detail does not carry its links: %+v", detail.Links)
	}

	rec = f.request(http.MethodDelete, f.cardPath("/links"), `{"target":"rollback-runbook"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlink = %d: %s", rec.Code, rec.Body)
	}
	if links := decode[[]core.CardLink](t, rec); len(links) != 0 {
		t.Errorf("links left after unlinking: %+v", links)
	}
	// A link that is not there is a refusal, not a silent success.
	if rec := f.request(http.MethodDelete, f.cardPath("/links"), `{"target":"rollback-runbook"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unlinking twice = %d, want 404: %s", rec.Code, rec.Body)
	}
	// An entry that does not exist cannot be cited.
	if rec := f.request(http.MethodPost, f.cardPath("/links"), `{"target":"no-such-entry"}`); rec.Code != http.StatusNotFound {
		t.Errorf("linking a missing entry = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// A plan lands as one transaction: every card, with its dependencies, or none.
func TestImportCards(t *testing.T) {
	f := newCardFixture(t, "PLAN")
	body := `[{"id":"a","title":"first step"},{"id":"b","title":"second step","blocked_by":["a"]}]`
	rec := f.request(http.MethodPost, "/api/p/PLAN/b/default/cards/import", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import = %d: %s", rec.Code, rec.Body)
	}
	cards := decode[[]core.Card](t, rec)
	if len(cards) != 2 || cards[0].Title != "first step" {
		t.Fatalf("import returned %+v", cards)
	}
	blockers, err := f.core.Blockers(context.Background(), cards[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].Ref != cards[0].Ref {
		t.Errorf("the second card is not blocked by the first: %+v", blockers)
	}

	// A refusal halfway down leaves the board as it was.
	before := len(decode[[]core.Card](t, f.request(http.MethodPost, "/api/p/PLAN/b/default/cards/import", `[{"title":"fine"}]`)))
	if before != 1 {
		t.Fatalf("a valid import should land: %d", before)
	}
	if rec := f.request(http.MethodPost, "/api/p/PLAN/b/default/cards/import",
		`[{"title":"ok"},{"title":""}]`); rec.Code != http.StatusBadRequest {
		t.Errorf("an import with a titleless card = %d, want 400: %s", rec.Code, rec.Body)
	}
	live := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/PLAN/b/default/cards", ""))
	total := 0
	for _, column := range live {
		total += len(column.Cards)
	}
	if total != 4 {
		t.Errorf("board holds %d cards, want the 4 that were accepted", total)
	}
}

// An artifact is uploaded as a file, links to a card, and the upload route
// is the one place the boundary accepts something other than JSON.
func TestUploadAndLinkArtifact(t *testing.T) {
	f := newCardFixture(t, "FILES")
	handler := f.server.protectedHandler("127.0.0.1:7788")

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	file, err := writer.CreateFormFile("file", "notes on the outage.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("what happened, and when\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("card", f.card.Ref); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7788/api/p/FILES/artifacts", bytes.NewReader(form.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "http://127.0.0.1:7788")
	req.Header.Set("X-Trellis-Token", f.server.token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body)
	}
	uploaded := decode[artifactItem](t, rec)
	if uploaded.Name != "notes-on-the-outage.txt" || uploaded.URL == "" || uploaded.Kind != "text" {
		t.Fatalf("uploaded artifact = %+v", uploaded)
	}

	detail := decode[cardDetail](t, f.request(http.MethodGet, f.cardPath(""), ""))
	if len(detail.Artifacts) != 1 || detail.Artifacts[0].Name != uploaded.Name {
		t.Fatalf("the card detail does not carry its artifacts: %+v", detail.Artifacts)
	}

	// The bytes come back through the artifact route.
	bytesRec := f.request(http.MethodGet, uploaded.URL, "")
	if bytesRec.Code != http.StatusOK || !strings.Contains(bytesRec.Body.String(), "what happened") {
		t.Errorf("artifact bytes = %d: %s", bytesRec.Code, bytesRec.Body)
	}

	// Unlinking leaves the file stored; the project still lists it.
	rec = f.request(http.MethodDelete, f.cardPath("/artifacts/"+uploaded.Name), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unlink artifact = %d: %s", rec.Code, rec.Body)
	}
	if left := decode[[]artifactItem](t, rec); len(left) != 0 {
		t.Errorf("the card still lists %+v", left)
	}
	stored := decode[[]artifactItem](t, f.request(http.MethodGet, "/api/p/FILES/artifacts", ""))
	if len(stored) != 1 {
		t.Fatalf("the project should still hold the file: %+v", stored)
	}

	// Linking an artifact the project already holds, then deleting it for good.
	rec = f.request(http.MethodPost, f.cardPath("/artifacts"), `{"name":"`+uploaded.Name+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("relink = %d: %s", rec.Code, rec.Body)
	}
	if again := decode[[]artifactItem](t, rec); len(again) != 1 {
		t.Errorf("relinked artifacts = %+v", again)
	}
	if rec := f.request(http.MethodDelete, "/api/p/FILES/artifacts/"+uploaded.Name, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete artifact = %d: %s", rec.Code, rec.Body)
	}
	if stored := decode[[]artifactItem](t, f.request(http.MethodGet, "/api/p/FILES/artifacts", "")); len(stored) != 0 {
		t.Errorf("artifact still stored: %+v", stored)
	}
}

// Only the upload route takes a file; everything else still requires JSON.
func TestMultipartIsRefusedOutsideTheUploadRoute(t *testing.T) {
	f := newCardFixture(t, "ONLY")
	handler := f.server.protectedHandler("127.0.0.1:7788")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7788/api/p/ONLY/b/default/cards", strings.NewReader("--x--"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.Header.Set("Origin", "http://127.0.0.1:7788")
	req.Header.Set("X-Trellis-Token", f.server.token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("a multipart card create = %d, want 415: %s", rec.Code, rec.Body)
	}
}

// The graph walk takes what the CLI takes: how far, which kinds of link, and
// which way. A card blocked by another is a chain one hop cannot see.
func TestGraphWalkOptions(t *testing.T) {
	f := newCardFixture(t, "WALK")
	made := decode[[]core.Card](t, f.request(http.MethodPost, "/api/p/WALK/b/default/cards/import",
		`[{"id":"a","title":"first"},{"id":"b","title":"second","blocked_by":["a"]},{"id":"c","title":"third","blocked_by":["b"]}]`))
	if len(made) != 3 {
		t.Fatalf("import returned %+v", made)
	}
	third := made[2].Ref

	// Outbound from the last card: it is blocked by the second, which is
	// blocked by the first.
	walk := decode[core.Graph](t, f.request(http.MethodGet, "/api/p/WALK/graph/"+third+"?depth=2&rel=blocked_by", ""))
	if len(walk.Nodes) != 3 {
		t.Errorf("depth 2 reached %d nodes, want 3: %+v", len(walk.Nodes), walk.Nodes)
	}
	near := decode[core.Graph](t, f.request(http.MethodGet, "/api/p/WALK/graph/"+third+"?depth=1&rel=blocked_by", ""))
	if len(near.Nodes) != 2 {
		t.Errorf("depth 1 reached %d nodes, want 2: %+v", len(near.Nodes), near.Nodes)
	}
	// A relation nothing uses reaches only the card itself.
	none := decode[core.Graph](t, f.request(http.MethodGet, "/api/p/WALK/graph/"+third+"?rel=cites", ""))
	if len(none.Nodes) != 1 {
		t.Errorf("rel=cites reached %+v, want only the card", none.Nodes)
	}
	// Reverse from the first card answers "what waits on this".
	back := decode[core.Graph](t, f.request(http.MethodGet, "/api/p/WALK/graph/"+made[0].Ref+"?depth=2&reverse=1&rel=blocked_by", ""))
	if len(back.Nodes) != 3 {
		t.Errorf("the reverse walk reached %d nodes, want 3: %+v", len(back.Nodes), back.Nodes)
	}
}

// Search takes a method and a project scope; an unknown method is refused
// rather than quietly ignored.
func TestSearchOptions(t *testing.T) {
	f := newCardFixture(t, "FIND")
	if rec := f.request(http.MethodGet, "/api/search?q=deploy", ""); rec.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", rec.Code, rec.Body)
	}
	hits := decode[[]core.SearchHit](t, f.request(http.MethodGet, "/api/search?q=deploy&project=FIND&method=fts&limit=5", ""))
	if len(hits) != 1 || hits[0].Ref != f.card.Ref {
		t.Errorf("scoped search = %+v", hits)
	}
	// Scoped to another project, the card is not a hit.
	if _, err := f.core.CreateProject(context.Background(), "OTHER", false); err != nil {
		t.Fatal(err)
	}
	if hits := decode[[]core.SearchHit](t, f.request(http.MethodGet, "/api/search?q=deploy&project=OTHER", "")); len(hits) != 0 {
		t.Errorf("another project's search = %+v, want none", hits)
	}
	if rec := f.request(http.MethodGet, "/api/search?q=deploy&method=telepathy", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown method = %d, want 400: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodGet, "/api/search?q=deploy&project=NOPE", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown project = %d, want 404: %s", rec.Code, rec.Body)
	}
}
