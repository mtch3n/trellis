package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

type shownBoard struct {
	Project string `json:"project"`
	Slug    string `json:"slug"`
}

func showBoard(t *testing.T, args ...string) shownBoard {
	t.Helper()
	var v shownBoard
	out := runCmd(t, append([]string{"board", "show", "--json"}, args...)...)
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("board show output: %v\n%s", err, out)
	}
	return v
}

func TestCommandsRefuseWithoutAMarker(t *testing.T) {
	markerEnv(t, "loose")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "unresolved" || !strings.Contains(ce.Fix, "trellis init --key") {
		t.Errorf("error = %+v", ce)
	}
}

// The SessionStart hook runs this everywhere; with no marker it must say
// nothing.
func TestBriefIsSilentWithoutAMarker(t *testing.T) {
	markerEnv(t, "loose")
	out, err := execCmd("board", "show", "--brief")
	if err != nil || out != "" {
		t.Errorf("brief = %q, %v; want nothing", out, err)
	}
}

func TestBriefFailsWhenTheMarkersProjectIsMissing(t *testing.T) {
	dir := markerEnv(t, "clone")
	writeMarker(t, dir, "/GHOST\n")
	_, err := execCmd("board", "show", "--brief")
	ce := coreErr(t, err)
	if ce.Code != "project_not_found" || !strings.Contains(ce.Msg, ".trellis") || ce.Exit == 0 {
		t.Errorf("error = %+v", ce)
	}
}

func TestMalformedMarkerIsAUsageError(t *testing.T) {
	dir := markerEnv(t, "old")
	writeMarker(t, dir, "TRELLIS\n")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "bad_pin" || !strings.Contains(ce.Msg, "old bare-key format") {
		t.Errorf("error = %+v", ce)
	}
}

func TestProjectPrecedence(t *testing.T) {
	dir := markerEnv(t, "app")
	seedProject(t, "MARKED")
	seedProject(t, "ENVIRON")
	seedProject(t, "FLAGGED")
	writeMarker(t, dir, "/MARKED\n")

	if got := showBoard(t).Project; got != "MARKED" {
		t.Errorf("marker: project = %s", got)
	}
	t.Setenv("TRELLIS_PROJECT", "environ")
	if got := showBoard(t).Project; got != "ENVIRON" {
		t.Errorf("env over marker: project = %s", got)
	}
	if got := showBoard(t, "--project", "flagged").Project; got != "FLAGGED" {
		t.Errorf("flag over env: project = %s", got)
	}
}

func TestBoardPrecedence(t *testing.T) {
	dir := markerEnv(t, "mono")
	seedProject(t, "MONO", "API", "Web")
	writeMarker(t, dir, "/MONO/boards/api\n")

	if got := showBoard(t).Slug; got != "api" {
		t.Errorf("marker board: slug = %s", got)
	}
	t.Setenv("TRELLIS_BOARD", "Web")
	if got := showBoard(t).Slug; got != "web" {
		t.Errorf("env over marker: slug = %s", got)
	}
	if got := showBoard(t, "--board", "mono").Slug; got != "mono" {
		t.Errorf("flag over env: slug = %s", got)
	}
	t.Setenv("TRELLIS_BOARD", "")
	if got := showBoard(t, "--project", "MONO").Slug; got != "mono" {
		t.Errorf("--project must ignore the marker's board: slug = %s", got)
	}
}

func TestMissingMarkerBoardNamesTheMarker(t *testing.T) {
	dir := markerEnv(t, "mono")
	seedProject(t, "MONO")
	writeMarker(t, dir, "/MONO/boards/api\n")
	_, err := execCmd("card", "ls")
	ce := coreErr(t, err)
	if ce.Code != "unknown_board" || !strings.Contains(ce.Msg, ".trellis") {
		t.Errorf("error = %+v", ce)
	}
}
