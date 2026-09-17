package cli

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type initOutput struct {
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
	Board *struct {
		Slug string `json:"slug"`
	} `json:"board"`
	MarkerPath    string   `json:"pin_path"`
	Created       bool     `json:"created"`
	MarkerWritten bool     `json:"pin_written"`
	Notes         []string `json:"notes"`
}

func runInit(t *testing.T, args ...string) initOutput {
	t.Helper()
	out := runCmd(t, append([]string{"init", "--json"}, args...)...)
	var v initOutput
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("init output: %v\n%s", err, out)
	}
	return v
}

func markerContent(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".trellis"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitCreatesAProjectFromTheDirectoryName(t *testing.T) {
	dir := markerEnv(t, "my app")
	got := runInit(t)
	if got.Project.Key != "MY-APP" || !got.Created || !got.MarkerWritten {
		t.Errorf("init = %+v", got)
	}
	if markerContent(t, dir) != "/MY-APP\n" {
		t.Errorf("marker = %q", markerContent(t, dir))
	}
	if !strings.Contains(strings.Join(got.Notes, "\n"), "commit .trellis") {
		t.Errorf("notes = %v, want the commit reminder", got.Notes)
	}
	// The marker it wrote is what later commands resolve through.
	if showBoard(t).Project != "MY-APP" {
		t.Error("a command after init did not resolve to the new project")
	}
}

func TestInitNeverJoinsOnAnInferredKey(t *testing.T) {
	dir := markerEnv(t, "alpha")
	seedProject(t, "ALPHA")
	_, err := execCmd("init")
	if ce := coreErr(t, err); ce.Code != "key_collision" {
		t.Fatalf("error = %+v", ce)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".trellis")); statErr == nil {
		t.Error("a refused init wrote a marker")
	}
}

func TestInitJoinsANamedProject(t *testing.T) {
	dir := markerEnv(t, "checkout-2")
	seedProject(t, "ALPHA")
	got := runInit(t, "--key", "alpha")
	if got.Created || got.Project.Key != "ALPHA" {
		t.Errorf("init = %+v", got)
	}
	if markerContent(t, dir) != "/ALPHA\n" {
		t.Errorf("marker = %q", markerContent(t, dir))
	}
}

func TestInitReadsACommittedMarker(t *testing.T) {
	dir := markerEnv(t, "fresh-clone")
	writeMarker(t, dir, "/BETA\n")
	got := runInit(t)
	if !got.Created || got.MarkerWritten || got.Project.Key != "BETA" {
		t.Errorf("init = %+v", got)
	}
	if markerContent(t, dir) != "/BETA\n" {
		t.Error("init rewrote an existing marker")
	}
}

func TestInitRefusesAKeyThatContradictsTheMarker(t *testing.T) {
	dir := markerEnv(t, "app")
	writeMarker(t, dir, "/BETA\n")
	_, err := execCmd("init", "--key", "GAMMA")
	if ce := coreErr(t, err); ce.Code != "pin_exists" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitBoardFlagPutsTheSlugInTheMarker(t *testing.T) {
	dir := markerEnv(t, "api")
	seedProject(t, "MONO", "API Work")
	got := runInit(t, "--key", "MONO", "--board", "API Work")
	if got.Board == nil || got.Board.Slug != "api-work" {
		t.Errorf("init = %+v", got)
	}
	if markerContent(t, dir) != "/MONO/boards/api-work\n" {
		t.Errorf("marker = %q", markerContent(t, dir))
	}
}

func TestInitRefusesTheProjectFlag(t *testing.T) {
	markerEnv(t, "app")
	_, err := execCmd("init", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "init_project_flag" || !strings.Contains(ce.Fix, "--key ALPHA") {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesTheHomeDirectory(t *testing.T) {
	dir := markerEnv(t, "home")
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	_, err := execCmd("init", "--key", "HOME")
	if ce := coreErr(t, err); ce.Code != "init_location" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesATrellisDirectory(t *testing.T) {
	dir := markerEnv(t, "app")
	if err := os.Mkdir(filepath.Join(dir, ".trellis"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := execCmd("init", "--key", "APP")
	if ce := coreErr(t, err); ce.Code != "bad_pin" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitRefusesAnUnusableDirectoryName(t *testing.T) {
	markerEnv(t, "___")
	_, err := execCmd("init")
	if ce := coreErr(t, err); ce.Code != "missing_key" {
		t.Errorf("error = %+v", ce)
	}
}

func TestInitWarnsWhenTrellisProjectIsSet(t *testing.T) {
	markerEnv(t, "app")
	seedProject(t, "OTHER")
	t.Setenv("TRELLIS_PROJECT", "OTHER")
	got := runInit(t, "--key", "APP")
	if !strings.Contains(strings.Join(got.Notes, "\n"), "TRELLIS_PROJECT=OTHER") {
		t.Errorf("notes = %v", got.Notes)
	}
}

func TestInitNotesTheParentMarkerItOverrides(t *testing.T) {
	parent := markerEnv(t, "mono")
	writeMarker(t, parent, "/MONO\n")
	api := filepath.Join(parent, "api")
	if err := os.Mkdir(api, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(api)
	got := runInit(t, "--key", "API")
	if !strings.Contains(strings.Join(got.Notes, "\n"), "overrides /MONO") {
		t.Errorf("notes = %v", got.Notes)
	}
}
