package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
)

func errCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	te, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("error = %v, want *core.Error", err)
	}
	return te.Code
}

func readPinFile(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, resolve.PinFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func projectCount(t *testing.T, c *Core) int {
	t.Helper()
	var n int
	if err := c.db.Get(&n, `SELECT count(*) FROM project`); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateProjectMakesTheDefaultBoard(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), " xpsctl ", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "XPSCTL" || p.Name != "XPSCTL" {
		t.Errorf("project = %+v, want key and name XPSCTL", p)
	}
	boards, err := c.ListBoards(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 1 || boards[0].Name != "xpsctl" || !boards[0].IsDefault {
		t.Errorf("boards = %+v, want one default board named xpsctl", boards)
	}
}

func TestCreateProjectRefusesBadReservedAndTakenKeys(t *testing.T) {
	c := testCore(t)
	for key, want := range map[string]string{
		"":       "bad_key",
		"1ABC":   "bad_key",
		"MY_APP": "bad_key",
		"GLOBAL": "reserved_key",
		"global": "reserved_key",
	} {
		_, err := c.CreateProject(t.Context(), key, false)
		if got := errCode(t, err); got != want {
			t.Errorf("CreateProject(%q) code = %s, want %s", key, got, want)
		}
	}
	if _, err := c.CreateProject(t.Context(), "TAKEN", false); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateProject(t.Context(), "taken", false)
	if got := errCode(t, err); got != "key_collision" {
		t.Errorf("code = %s, want key_collision", got)
	}
}

func TestCreateProjectSeedsLabelsOnlyWithPreset(t *testing.T) {
	c := testCore(t)
	with, err := c.CreateProject(t.Context(), "WITH", true)
	if err != nil {
		t.Fatal(err)
	}
	without, err := c.CreateProject(t.Context(), "WITHOUT", false)
	if err != nil {
		t.Fatal(err)
	}
	for id, wantAny := range map[string]bool{with.ID: true, without.ID: false} {
		var n int
		if err := c.db.Get(&n, `SELECT count(*) FROM label WHERE project_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if (n > 0) != wantAny {
			t.Errorf("project %s has %d labels, want any=%v", id, n, wantAny)
		}
	}
}

func TestBoardBySlug(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), "ALPHA", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "API Work", true); err != nil {
		t.Fatal(err)
	}
	b, err := c.BoardBySlug(t.Context(), p.ID, "api-work")
	if err != nil || b.Name != "API Work" {
		t.Fatalf("BoardBySlug = %+v, %v", b, err)
	}
	_, err = c.BoardBySlug(t.Context(), p.ID, "web")
	if got := errCode(t, err); got != "unknown_board" {
		t.Errorf("code = %s, want unknown_board", got)
	}
	if !strings.Contains(err.Error(), "alpha, api-work") {
		t.Errorf("error %q should list the slugs that exist", err)
	}
}

func TestInitProjectCreatesAndPins(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "alpha", Preset: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !res.Wrote || res.Project.Key != "ALPHA" || res.Board != nil {
		t.Errorf("result = %+v", res)
	}
	if res.PinPath != filepath.Join(dir, resolve.PinFile) {
		t.Errorf("pin path = %s", res.PinPath)
	}
	if got := readPinFile(t, dir); got != "/ALPHA\n" {
		t.Errorf("pin = %q, want /ALPHA", got)
	}
}

func TestInitProjectJoinsOnlyWhenAsked(t *testing.T) {
	c := testCore(t)
	if _, err := c.CreateProject(t.Context(), "ALPHA", false); err != nil {
		t.Fatal(err)
	}

	inferred := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: inferred, Key: "ALPHA"})
	if got := errCode(t, err); got != "key_collision" {
		t.Fatalf("code = %s, want key_collision", got)
	}
	if _, statErr := os.Stat(filepath.Join(inferred, resolve.PinFile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a refused init wrote a pin: %v", statErr)
	}
	if !strings.Contains(err.Error(), "trellis init --key ALPHA") {
		t.Errorf("error %q should say how to join deliberately", err)
	}

	explicit := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: explicit, Key: "ALPHA", Join: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !res.Wrote {
		t.Errorf("result = %+v, want joined and written", res)
	}
	if projectCount(t, c) != 1 {
		t.Errorf("joining created a project")
	}
}

func TestInitProjectPinsANamedBoard(t *testing.T) {
	c := testCore(t)
	p, err := c.CreateProject(t.Context(), "ALPHA", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateBoard(t.Context(), p.ID, "API Work", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "ALPHA", Join: true, BoardName: "API Work"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Board == nil || res.Board.Slug != "api-work" {
		t.Errorf("board = %+v", res.Board)
	}
	if got := readPinFile(t, dir); got != "/ALPHA/boards/api-work\n" {
		t.Errorf("pin = %q", got)
	}
}

// A failure after the project row is inserted must leave nothing behind.
func TestInitProjectIsOneTransaction(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "NEW", BoardName: "missing", Preset: true})
	if got := errCode(t, err); got != "board_not_found" {
		t.Fatalf("code = %s, want board_not_found", got)
	}
	if projectCount(t, c) != 0 {
		t.Error("the project survived a failed init")
	}
	var boards, labels int
	if err := c.db.Get(&boards, `SELECT count(*) FROM board`); err != nil {
		t.Fatal(err)
	}
	if err := c.db.Get(&labels, `SELECT count(*) FROM label`); err != nil {
		t.Fatal(err)
	}
	if boards != 0 || labels != 0 {
		t.Errorf("left %d boards and %d labels behind", boards, labels)
	}
}

func TestInitProjectNeverWritesAnUnparsablePin(t *testing.T) {
	c := testCore(t)
	for key, want := range map[string]string{"MY_APP": "bad_key", "GLOBAL": "reserved_key"} {
		dir := t.TempDir()
		_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: key, Join: true})
		if got := errCode(t, err); got != want {
			t.Errorf("join %s: code = %s, want %s", key, got, want)
		}
		if _, statErr := os.Stat(filepath.Join(dir, resolve.PinFile)); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("join %s wrote a pin", key)
		}
	}
}

func existingPin(t *testing.T, dir, content string) *resolve.Pin {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, resolve.PinFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pin, err := resolve.ReadPin(filepath.Join(dir, resolve.PinFile))
	if err != nil {
		t.Fatal(err)
	}
	return &pin
}

// A fresh clone on a new machine: the pin is committed, the database is empty.
func TestInitProjectMaterializesAnExistingPin(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	res, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: existingPin(t, dir, "/BETA\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Wrote || res.Project.Key != "BETA" {
		t.Errorf("result = %+v", res)
	}
	if got := readPinFile(t, dir); got != "/BETA\n" {
		t.Errorf("pin was rewritten: %q", got)
	}
}

func TestInitProjectDoesNotCreateAPinnedBoard(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: existingPin(t, dir, "/BETA/boards/api\n")})
	if got := errCode(t, err); got != "unknown_board" {
		t.Fatalf("code = %s, want unknown_board", got)
	}
	if !strings.Contains(err.Error(), "trellis board new") {
		t.Errorf("error %q should point at board new", err)
	}
	if projectCount(t, c) != 0 {
		t.Error("the project survived a failed init")
	}
}

func TestInitProjectRefusesFlagsThatContradictThePin(t *testing.T) {
	c := testCore(t)
	dir := t.TempDir()
	pin := existingPin(t, dir, "/BETA\n")

	_, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Key: "GAMMA", Join: true, Existing: pin})
	if got := errCode(t, err); got != "pin_exists" {
		t.Errorf("--key GAMMA: code = %s, want pin_exists", got)
	}

	if _, err := c.InitProject(t.Context(), InitRequest{Dir: dir, Existing: pin}); err != nil {
		t.Fatal(err)
	}
	_, err = c.InitProject(t.Context(), InitRequest{Dir: dir, BoardName: "beta", Existing: pin})
	if got := errCode(t, err); got != "pin_exists" {
		t.Errorf("--board beta against /BETA: code = %s, want pin_exists", got)
	}
	if got := readPinFile(t, dir); got != "/BETA\n" {
		t.Errorf("pin changed: %q", got)
	}
}

// Another init can publish the pin between this init's commit and its write.
func TestInitProjectNeverOverwritesAPinWrittenMeanwhile(t *testing.T) {
	c := testCore(t)

	same := t.TempDir()
	existingPin(t, same, "/ALPHA\n")
	res, err := c.InitProject(t.Context(), InitRequest{Dir: same, Key: "ALPHA", Join: true})
	if err != nil {
		t.Fatalf("identical pin: %v", err)
	}
	if res.Wrote {
		t.Error("reported writing a pin that was already there")
	}

	other := t.TempDir()
	existingPin(t, other, "/OTHER\n")
	_, err = c.InitProject(t.Context(), InitRequest{Dir: other, Key: "FRESH", Join: true})
	if got := errCode(t, err); got != "pin_exists" {
		t.Fatalf("code = %s, want pin_exists", got)
	}
	if !strings.Contains(err.Error(), "FRESH was created") {
		t.Errorf("error %q should say the project it created remains", err)
	}
	if got := readPinFile(t, other); got != "/OTHER\n" {
		t.Errorf("pin was overwritten: %q", got)
	}
}
