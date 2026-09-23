package ui

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// A board can be renamed, made the one a project opens on, and removed —
// except the last one, which a project always keeps.
func TestBoardLifecycle(t *testing.T) {
	f := newCardFixture(t, "BOARDS")

	if rec := f.request(http.MethodPost, "/api/p/BOARDS/boards", `{"name":"second board"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	renamed := f.request(http.MethodPatch, "/api/p/BOARDS/b/second-board", `{"name":"planning"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", renamed.Code, renamed.Body)
	}
	// The slug is what a `.trellis` marker names, so it survives a rename;
	// only the name a person reads changes.
	if board := decode[boardInfo](t, renamed); board.Name != "planning" || board.Slug != "second-board" {
		t.Errorf("renamed board = %+v", board)
	}

	promoted := f.request(http.MethodPatch, "/api/p/BOARDS/b/second-board", `{"is_default":true}`)
	if promoted.Code != http.StatusOK {
		t.Fatalf("set default = %d: %s", promoted.Code, promoted.Body)
	}
	if board := decode[boardInfo](t, promoted); !board.IsDefault {
		t.Errorf("board is not the default: %+v", board)
	}

	// The board holding the fixture's card needs force; without it, nothing changes.
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/default", ""); rec.Code != http.StatusConflict {
		t.Errorf("delete a board with cards = %d, want 409: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/default?force=1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("forced delete = %d: %s", rec.Code, rec.Body)
	}
	// A project also holds the board it was created with. Once that goes too,
	// one is left, and a project always has somewhere to put a card.
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/boards", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete the project's own empty board = %d: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/BOARDS/b/second-board?force=1", ""); rec.Code != http.StatusConflict {
		t.Errorf("delete the last board = %d, want 409: %s", rec.Code, rec.Body)
	}
}

// Columns are added, renamed, reordered and removed, and a column with cards
// in it only goes when the request says where the cards go.
func TestColumnLifecycle(t *testing.T) {
	f := newCardFixture(t, "COLS")
	columns := func() []core.Column {
		return decode[[]core.Column](t, f.request(http.MethodGet, "/api/p/COLS/b/default/columns", ""))
	}
	names := func() []string {
		var out []string
		for _, column := range columns() {
			out = append(out, column.Name)
		}
		return out
	}
	seeded := names()
	if len(seeded) == 0 {
		t.Fatal("a new board should have seeded columns")
	}

	rec := f.request(http.MethodPost, "/api/p/COLS/b/default/columns", `{"name":"blocked","after":"backlog"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body)
	}
	if added := decode[core.Column](t, rec); added.Name != "blocked" {
		t.Errorf("added column = %+v", added)
	}
	if got := names(); got[1] != "blocked" {
		t.Errorf("columns = %v, want blocked second", got)
	}

	if rec := f.request(http.MethodPatch, "/api/p/COLS/b/default/columns/blocked", `{"name":"waiting"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", rec.Code, rec.Body)
	}
	// Moving is "after this one", so an empty target makes it first.
	if rec := f.request(http.MethodPatch, "/api/p/COLS/b/default/columns/waiting", `{"after":""}`); rec.Code != http.StatusOK {
		t.Fatalf("move = %d: %s", rec.Code, rec.Body)
	}
	if got := names(); got[0] != "waiting" {
		t.Errorf("columns = %v, want waiting first", got)
	}

	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/waiting", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete an empty column = %d: %s", rec.Code, rec.Body)
	}

	// The fixture's card sits in the first seeded column, so that one cannot
	// go without somewhere for the card to land.
	filled := seeded[0]
	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/"+filled, ""); rec.Code != http.StatusConflict {
		t.Errorf("delete a column with cards = %d, want 409: %s", rec.Code, rec.Body)
	}
	if rec := f.request(http.MethodDelete, "/api/p/COLS/b/default/columns/"+filled+"?move_cards_to="+seeded[1], ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete with move_cards_to = %d: %s", rec.Code, rec.Body)
	}
	live := decode[[]columnCardsInfo](t, f.request(http.MethodGet, "/api/p/COLS/b/default/cards", ""))
	moved := 0
	for _, column := range live {
		if column.Name == seeded[1] {
			moved = len(column.Cards)
		}
	}
	if moved != 1 {
		t.Errorf("the card did not move with the column: %+v", live)
	}
}

// The admin reads: who holds what, what the installation looks like, what
// this binary is, and where the backups go.
func TestAdminReads(t *testing.T) {
	f := newCardFixture(t, "ADMIN")
	ctx := context.Background()
	if _, err := f.core.RegisterAgent(ctx, "agent-one", "agent", "/tmp", "host", 42); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.ClaimCard(ctx, f.card.ID, 0, false, ""); err != nil {
		t.Fatal(err)
	}

	agents := decode[[]agentInfo](t, f.request(http.MethodGet, "/api/agents", ""))
	var claimed []claimedCard
	for _, agent := range agents {
		if len(agent.Claimed) > 0 {
			claimed = agent.Claimed
		}
	}
	if len(claimed) != 1 || claimed[0].Ref != f.card.Ref || claimed[0].ProjectKey != "ADMIN" {
		t.Errorf("agents = %+v, want one holding %s", agents, f.card.Ref)
	}

	// Every check the CLI's doctor runs, plus the daemon answering.
	report := decode[struct {
		Checks []struct{ Name, Status string } `json:"checks"`
		Failed int                             `json:"failed"`
	}](t, f.request(http.MethodGet, "/api/doctor", ""))
	names := map[string]string{}
	for _, check := range report.Checks {
		names[check.Name] = check.Status
	}
	for _, want := range []string{"daemon", "binary", "storage root", "database", "config", "project keys", "vector search"} {
		if _, ok := names[want]; !ok {
			t.Errorf("the report has no %q check: %+v", want, names)
		}
	}

	info := decode[versionInfo](t, f.request(http.MethodGet, "/api/version", ""))
	if info.Version == "" || info.OS == "" || info.Latest != "" {
		t.Errorf("version = %+v; the release check has to be asked for", info)
	}

	// Vector search is off by default, which is a state rather than an error.
	status := decode[map[string]any](t, f.request(http.MethodGet, "/api/p/ADMIN/vector", ""))
	if status["enabled"] != false {
		t.Errorf("vector status = %+v", status)
	}
}

// A backup is written where the person at the browser can find it, and
// pruning keeps the newest.
func TestBackupAndPrune(t *testing.T) {
	f := newCardFixture(t, "COPIES")
	first := decode[map[string]any](t, f.request(http.MethodPost, "/api/maintenance/backup", "{}"))
	path, _ := first["path"].(string)
	if !strings.Contains(path, "backups") || !strings.HasSuffix(path, ".db") {
		t.Fatalf("backup = %+v", first)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the backup is not on disk: %v", err)
	}

	listed := decode[struct {
		Directory string           `json:"directory"`
		Backups   []map[string]any `json:"backups"`
	}](t, f.request(http.MethodGet, "/api/backups", ""))
	if len(listed.Backups) != 1 {
		t.Errorf("backups = %+v", listed)
	}

	// A relative directory is refused: the daemon's working directory is not
	// a place the browser can see.
	if rec := f.request(http.MethodPost, "/api/maintenance/backup", `{"directory":"copies"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a relative directory = %d, want 400: %s", rec.Code, rec.Body)
	}

	// Pruning to one keeps the newest and says how many went. Newest is read
	// from the name, and the fixture's clock stands 16 minutes after the
	// epoch, so the older backup is named for the epoch itself. It is written
	// last, so ordering by modification time would keep it instead.
	stale := filepath.Join(filepath.Dir(path), "trellis-backup-19700101-000000.db")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruned := decode[map[string]int](t, f.request(http.MethodPost, "/api/maintenance/backup/prune", `{"keep":1}`))
	if pruned["deleted"] != 1 {
		t.Errorf("prune = %+v, want one deleted", pruned)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the older backup is still there")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the newest backup is gone: %v", err)
	}
	if rec := f.request(http.MethodPost, "/api/maintenance/backup/prune", `{"keep":0}`); rec.Code != http.StatusBadRequest {
		t.Errorf("keep=0 = %d, want 400: %s", rec.Code, rec.Body)
	}
}
