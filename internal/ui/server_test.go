package ui

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestServerCardLifecycleAndEmbeddedSPA(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	p, err := c.CreateProject(context.Background(), "UITEST", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	created := request(http.MethodPost, "/api/p/UITEST/b/default/cards", `{"title":"API card","body":"context","priority":1}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	var card core.Card
	if err := json.Unmarshal(created.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.Ref != "UITEST-1" {
		t.Fatalf("created ref = %q, want UITEST-1", card.Ref)
	}
	detail := request(http.MethodGet, "/api/p/UITEST/b/default/cards/UITEST-1", "")
	if detail.Code != http.StatusOK || !bytes.Contains(detail.Body.Bytes(), []byte(`"activity"`)) {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body)
	}
	projectDetail := request(http.MethodGet, "/api/p/UITEST/cards/UITEST-1", "")
	if projectDetail.Code != http.StatusOK || !bytes.Contains(projectDetail.Body.Bytes(), []byte(`"card"`)) {
		t.Fatalf("project detail status = %d, body = %s", projectDetail.Code, projectDetail.Body)
	}

	updated := request(http.MethodPatch, "/api/p/UITEST/b/default/cards/UITEST-1", `{"title":"Updated","body":"new context","if_version":1}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body)
	}

	moved := request(http.MethodPost, "/api/p/UITEST/b/default/cards/UITEST-1/move", `{"column":"done"}`)
	if moved.Code != http.StatusOK {
		t.Fatalf("move status = %d, body = %s", moved.Code, moved.Body)
	}

	stale := request(http.MethodPatch, "/api/p/UITEST/b/default/cards/UITEST-1", `{"title":"stale","if_version":1}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update status = %d, want %d, body = %s", stale.Code, http.StatusConflict, stale.Body)
	}

	if rec := request(http.MethodPost, "/api/p/UITEST/b/default/cards", `{"title":"a mistake"}`); rec.Code != http.StatusCreated {
		t.Fatalf("second create status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodDelete, "/api/p/UITEST/b/default/cards/UITEST-2", `{}`); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/p/UITEST/cards/UITEST-2", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted card still answers: %d", rec.Code)
	}
	if rec := request(http.MethodDelete, "/api/p/UITEST/b/default/cards/UITEST-2", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}

	page := request(http.MethodGet, "/", "")
	if page.Code != http.StatusOK || !bytes.Contains(page.Body.Bytes(), []byte("<div id=\"root\">")) {
		t.Fatalf("SPA response = %d, body = %s", page.Code, page.Body.String())
	}
}

func TestServerVaultGraphLabelsAndStealRoutes(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 2_000_000}, "ui-p5-test", dir)
	p, err := c.CreateProject(context.Background(), "P5TEST", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLabel(context.Background(), p.ID, "old", "legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateLabel(context.Background(), p.ID, "new", "current"); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	created := request(http.MethodPost, "/api/p/P5TEST/b/default/knowledge", `{"title":"Concurrency","summary":"leases","body":"first"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("entry create status = %d, body = %s", created.Code, created.Body)
	}
	var entry core.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Slug == "" || entry.Version == 0 {
		t.Fatalf("created entry = %+v", entry)
	}

	listed := request(http.MethodGet, "/api/p/P5TEST/b/default/knowledge", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"slug":"`+entry.Slug+`"`)) {
		t.Fatalf("entry list status = %d, body = %s", listed.Code, listed.Body)
	}
	projectEntries := request(http.MethodGet, "/api/p/P5TEST/knowledge", "")
	if projectEntries.Code != http.StatusOK || !bytes.Contains(projectEntries.Body.Bytes(), []byte(`"slug":"`+entry.Slug+`"`)) {
		t.Fatalf("project entry status = %d, body = %s", projectEntries.Code, projectEntries.Body)
	}
	globalEntries := request(http.MethodGet, "/api/global/knowledge", "")
	if globalEntries.Code != http.StatusOK {
		t.Fatalf("global entry status = %d, body = %s", globalEntries.Code, globalEntries.Body)
	}
	edited := request(http.MethodPatch, "/api/p/P5TEST/b/default/knowledge/"+entry.Slug, `{"body":"second","version":1}`)
	if edited.Code != http.StatusOK || !bytes.Contains(edited.Body.Bytes(), []byte("second")) {
		t.Fatalf("entry edit status = %d, body = %s", edited.Code, edited.Body)
	}
	graph := request(http.MethodGet, "/api/p/P5TEST/b/default/graph/"+entry.Slug, "")
	if graph.Code != http.StatusOK || !bytes.Contains(graph.Body.Bytes(), []byte(`"nodes"`)) {
		t.Fatalf("graph status = %d, body = %s", graph.Code, graph.Body)
	}
	search := request(http.MethodGet, "/api/search?q=Concurrency", "")
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte(`"kind":"knowledge"`)) {
		t.Fatalf("search status = %d, body = %s", search.Code, search.Body)
	}
	activity := request(http.MethodGet, "/api/activity?limit=10", "")
	if activity.Code != http.StatusOK || !bytes.Contains(activity.Body.Bytes(), []byte(`"entity_type":"entry"`)) {
		t.Fatalf("activity status = %d, body = %s", activity.Code, activity.Body)
	}

	labels := request(http.MethodGet, "/api/p/P5TEST/labels", "")
	if labels.Code != http.StatusOK || !bytes.Contains(labels.Body.Bytes(), []byte(`"name":"old"`)) {
		t.Fatalf("labels status = %d, body = %s", labels.Code, labels.Body)
	}
	merged := request(http.MethodPost, "/api/p/P5TEST/labels/merge", `{"from":"old","into":"new"}`)
	if merged.Code != http.StatusOK {
		t.Fatalf("label merge status = %d, body = %s", merged.Code, merged.Body)
	}
	if _, err := c.GetLabel(context.Background(), p.ID, "old"); err == nil {
		t.Fatal("merged label still exists")
	}

	board, err := c.SelectBoard(context.Background(), p.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(context.Background(), p.ID, board.ID, core.NewCard{Title: "Claimed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(context.Background(), card.ID, 30*60*1000, false, ""); err != nil {
		t.Fatal(err)
	}
	stolen := request(http.MethodPost, "/api/p/P5TEST/b/default/cards/"+card.Ref+"/steal", `{"reason":"claimant is inactive"}`)
	if stolen.Code != http.StatusOK {
		t.Fatalf("steal status = %d, body = %s", stolen.Code, stolen.Body)
	}
}

func TestServerDeletesAProjectOnlyWhenTheKeyIsRetyped(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 3_000_000}, "ui-delete-test", dir)
	for _, key := range []string{"GONE", "KEPT"} {
		p, err := c.CreateProject(context.Background(), key, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
			t.Fatal(err)
		}
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := request(http.MethodPost, "/api/p/GONE/b/default/cards", `{"title":"on the doomed board"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodPost, "/api/p/KEPT/b/default/cards", `{"title":"on the kept board"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body)
	}

	scoped := request(http.MethodGet, "/api/activity?project=KEPT", "")
	if scoped.Code != http.StatusOK || bytes.Contains(scoped.Body.Bytes(), []byte("doomed")) ||
		!bytes.Contains(scoped.Body.Bytes(), []byte("kept board")) {
		t.Fatalf("activity scoped to KEPT = %d, body = %s", scoped.Code, scoped.Body)
	}

	if rec := request(http.MethodDelete, "/api/p/GONE", `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/p/GONE/boards", ""); rec.Code != http.StatusOK {
		t.Fatalf("a malformed delete request must leave the project: %d", rec.Code)
	}

	if rec := request(http.MethodDelete, "/api/p/GONE", `{"confirm":"gone-ish"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("mistyped confirmation status = %d, want 400, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/p/GONE/boards", ""); rec.Code != http.StatusOK {
		t.Fatalf("a refused delete must leave the project: %d", rec.Code)
	}

	if rec := request(http.MethodDelete, "/api/p/GONE", `{"confirm":"GONE"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/p/GONE/boards", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted project still answers: %d", rec.Code)
	}
	if rec := request(http.MethodDelete, "/api/p/GONE", `{"confirm":"GONE"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", rec.Code)
	}
	projects := request(http.MethodGet, "/api/projects", "")
	if bytes.Contains(projects.Body.Bytes(), []byte(`"GONE"`)) || !bytes.Contains(projects.Body.Bytes(), []byte(`"KEPT"`)) {
		t.Fatalf("projects after delete = %s", projects.Body)
	}
}

// TestActivityScopedToProjectIncludesDeletedLabelAndCommentEvents guards
// against computing the activity feed's project filter from live-row joins:
// a deleted card has no row left to join, and label and comment events are
// never joined at all, so a naive filter drops all three from a
// project-scoped feed even though the event rows themselves carry the
// project.
func TestActivityScopedToProjectIncludesDeletedLabelAndCommentEvents(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-activity-test", dir)
	p, err := c.CreateProject(context.Background(), "SCOPE", false)
	if err != nil {
		t.Fatal(err)
	}
	board, err := c.CreateBoard(context.Background(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	card, err := c.CreateCard(context.Background(), p.ID, board.ID, core.NewCard{Title: "gone soon"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(http.MethodDelete, "/api/p/SCOPE/b/default/cards/"+card.Ref, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete card status = %d, body = %s", rec.Code, rec.Body)
	}

	if rec := request(http.MethodPost, "/api/p/SCOPE/labels", `{"name":"old"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create label old status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodPost, "/api/p/SCOPE/labels", `{"name":"new"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create label new status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodPost, "/api/p/SCOPE/labels/merge", `{"from":"old","into":"new"}`); rec.Code != http.StatusOK {
		t.Fatalf("label merge status = %d, body = %s", rec.Code, rec.Body)
	}

	other, err := c.CreateCard(context.Background(), p.ID, board.ID, core.NewCard{Title: "commented"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := request(http.MethodPost, "/api/p/SCOPE/b/default/cards/"+other.Ref+"/comments", `{"body":"noted"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create comment status = %d, body = %s", rec.Code, rec.Body)
	}

	scoped := request(http.MethodGet, "/api/activity?project=SCOPE", "")
	if scoped.Code != http.StatusOK {
		t.Fatalf("scoped activity status = %d, body = %s", scoped.Code, scoped.Body)
	}
	body := scoped.Body.Bytes()
	if !bytes.Contains(body, []byte(`"action":"deleted"`)) {
		t.Errorf("scoped activity is missing the deleted card event: %s", body)
	}
	if !bytes.Contains(body, []byte(`"entity_type":"label"`)) {
		t.Errorf("scoped activity is missing the label merge event: %s", body)
	}
	if !bytes.Contains(body, []byte(`"entity_type":"comment"`)) {
		t.Errorf("scoped activity is missing the comment event: %s", body)
	}
}

func TestServerBoardListsCardsByPriorityThenRank(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 4_000_000}, "ui-order-test", dir)
	p, err := c.CreateProject(context.Background(), "ORDER", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(context.Background(), p.ID, "default", true); err != nil {
		t.Fatal(err)
	}
	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}
	// Created low, normal, urgent, normal: rank order is creation order.
	for _, card := range []string{`{"title":"low","priority":3}`, `{"title":"first normal","priority":2}`,
		`{"title":"urgent","priority":0}`, `{"title":"second normal","priority":2}`} {
		if rec := request(http.MethodPost, "/api/p/ORDER/b/default/cards", card); rec.Code != http.StatusCreated {
			t.Fatalf("create = %d: %s", rec.Code, rec.Body)
		}
	}

	rec := request(http.MethodGet, "/api/p/ORDER/b/default/cards", "")
	var columns []struct {
		Cards []struct {
			Title     string `json:"title"`
			CreatedAt int64  `json:"created_at"`
			UpdatedAt int64  `json:"updated_at"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &columns); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, card := range columns[0].Cards {
		titles = append(titles, card.Title)
		if card.CreatedAt <= 0 || card.UpdatedAt < card.CreatedAt {
			t.Errorf("%s: created_at %d, updated_at %d; the list must carry both times", card.Title, card.CreatedAt, card.UpdatedAt)
		}
	}
	want := []string{"urgent", "first normal", "second normal", "low"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %v, want %v: priority first, the manual rank within it", titles, want)
	}
}

func TestServerProjectEventsPageThroughCardAndEntryHistory(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 5_000_000}, "ui-events-test", dir)
	p, err := c.CreateProject(ctx, "EVT", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(ctx, p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, core.NewCard{Title: "tracked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveCard(ctx, p.ID, b.ID, core.CardRef{Seq: card.Seq}, "in-progress"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(ctx, card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(ctx, p.ID, core.NewEntry{Title: "Findings", Body: "first secret\n"})
	if err != nil {
		t.Fatal(err)
	}
	body := "second secret\n"
	if _, err := c.EditEntryFields(ctx, p.ID, entry.Slug, core.EntryEdit{Body: &body, IfVersion: &entry.Version}); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	type page struct {
		Events []core.FeedEvent `json:"events"`
		Next   *int64           `json:"next"`
	}
	get := func(path string) (page, []byte) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
		}
		var got page
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got, rec.Body.Bytes()
	}

	all, raw := get("/api/p/EVT/events")
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("events leak entry text: %s", raw)
	}
	var moved, claimed, edited bool
	for i, event := range all.Events {
		if i > 0 && event.Seq <= all.Events[i-1].Seq {
			t.Fatalf("events out of order: %v", all.Events)
		}
		if event.Field != "column" && (event.Old != "" || event.New != "") {
			t.Errorf("%s %s %s carries values %q -> %q; only column moves may", event.Kind, event.Action, event.Field, event.Old, event.New)
		}
		switch {
		case event.Kind == "card" && event.Action == "moved":
			moved = event.Ref == "EVT-1" && event.Old == "backlog" && event.New == "in-progress"
		case event.Kind == "card" && event.Action == "claimed":
			claimed = event.Ref == "EVT-1" && event.Actor == "ui-events-test"
		case event.Kind == "entry" && event.Action == "edited":
			edited = event.Ref == "/EVT/vault/"+entry.Slug
		}
	}
	if !moved || !claimed || !edited {
		t.Fatalf("moved=%v claimed=%v edited=%v in %+v", moved, claimed, edited, all.Events)
	}
	if all.Next == nil || *all.Next != all.Events[len(all.Events)-1].Seq {
		t.Fatalf("next = %v, want the last seq", all.Next)
	}

	// Two at a time, following next, walks the same history.
	var walked []int64
	after := int64(0)
	for range len(all.Events) + 1 {
		got, _ := get("/api/p/EVT/events?limit=2&after=" + strconv.FormatInt(after, 10))
		if len(got.Events) > 2 {
			t.Fatalf("limit=2 returned %d events", len(got.Events))
		}
		if len(got.Events) == 0 {
			if got.Next != nil {
				t.Fatalf("an empty page has next = %d", *got.Next)
			}
			break
		}
		for _, event := range got.Events {
			walked = append(walked, event.Seq)
		}
		after = *got.Next
	}
	if len(walked) != len(all.Events) {
		t.Fatalf("paged through %d events, want %d", len(walked), len(all.Events))
	}
	for i, seq := range walked {
		if seq != all.Events[i].Seq {
			t.Fatalf("page order %v differs from %v", walked, all.Events)
		}
	}

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/p/NOPE/events", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project = %d", rec.Code)
	}
}

func TestServerClaimCardValidatesJSON(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "claim-test", dir)
	p, err := c.CreateProject(context.Background(), "CLAIMTEST", false)
	if err != nil {
		t.Fatal(err)
	}
	board, err := c.CreateBoard(context.Background(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(context.Background(), p.ID, board.ID, core.NewCard{Title: "Test Card"})
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Test malformed JSON returns 400 and card remains unclaimed
	malformed := request(http.MethodPost, "/api/p/CLAIMTEST/b/default/cards/"+card.Ref+"/claim", "{")
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status = %d, want %d, body = %s", malformed.Code, http.StatusBadRequest, malformed.Body)
	}
	detail := request(http.MethodGet, "/api/p/CLAIMTEST/b/default/cards/"+card.Ref, "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}
	var cardDetail core.Card
	if err := json.Unmarshal(detail.Body.Bytes(), &cardDetail); err != nil {
		t.Fatal(err)
	}
	if cardDetail.ClaimUntil != nil {
		t.Fatalf("card claimed after malformed JSON request; ClaimUntil = %v", cardDetail.ClaimUntil)
	}

	// Test empty body succeeds and claims the card
	empty := request(http.MethodPost, "/api/p/CLAIMTEST/b/default/cards/"+card.Ref+"/claim", "")
	if empty.Code != http.StatusOK {
		t.Fatalf("empty body status = %d, want %d, body = %s", empty.Code, http.StatusOK, empty.Body)
	}
	var claimedCard core.Card
	if err := json.Unmarshal(empty.Body.Bytes(), &claimedCard); err != nil {
		t.Fatal(err)
	}
	if claimedCard.ClaimUntil == nil {
		t.Fatalf("card not claimed after successful request")
	}
}
