package cli

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
)

// refOf runs a command with --json and returns the "ref" it prints.
func refOf(t *testing.T, args ...string) string {
	t.Helper()
	out := runCmd(t, append(args, "--json")...)
	var v struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return v.Ref
}

// targetEnv is a directory pinned to ALPHA, with BETA beside it and one card
// in each: ALPHA-1 and BETA-1. BETA also has a board named Side.
func targetEnv(t *testing.T) string {
	t.Helper()
	dir := pinEnv(t, "alpha")
	seedProject(t, "ALPHA")
	seedProject(t, "BETA", "Side")
	writePin(t, dir, "/ALPHA\n")
	if ref := refOf(t, "card", "new", "--title", "alpha one"); ref != "ALPHA-1" {
		t.Fatalf("seed card = %s", ref)
	}
	if ref := refOf(t, "card", "new", "--title", "beta one", "--project", "BETA"); ref != "BETA-1" {
		t.Fatalf("seed card = %s", ref)
	}
	return dir
}

func TestAQualifiedCardRefNamesItsProject(t *testing.T) {
	targetEnv(t)
	for arg, want := range map[string]string{
		"BETA-1": "BETA-1", "beta-1": "BETA-1", "1": "ALPHA-1", "ALPHA-1": "ALPHA-1",
		"/BETA/cards/BETA-1": "BETA-1", "/alpha/cards/alpha-1": "ALPHA-1",
	} {
		if got := refOf(t, "card", "show", arg); got != want {
			t.Errorf("card show %s = %s, want %s", arg, got, want)
		}
	}
}

func TestACardAddressNeedsNoPin(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA")
	refOf(t, "card", "new", "--title", "beta one", "--project", "BETA")
	if got := refOf(t, "card", "show", "/BETA/cards/BETA-1"); got != "BETA-1" {
		t.Errorf("card show = %s", got)
	}
	if got := refOf(t, "card", "show", "BETA-1"); got != "BETA-1" {
		t.Errorf("card show BETA-1 = %s", got)
	}
}

func TestAnAddressBeatsTrellisProjectAndConflictsWithTheFlag(t *testing.T) {
	targetEnv(t)
	t.Setenv("TRELLIS_PROJECT", "ALPHA")
	if got := refOf(t, "card", "show", "/BETA/cards/BETA-1"); got != "BETA-1" {
		t.Errorf("address under TRELLIS_PROJECT = %s", got)
	}
	if got := refOf(t, "card", "show", "BETA-1", "--project", "BETA"); got != "BETA-1" {
		t.Errorf("agreeing --project = %s", got)
	}
	_, err := execCmd("card", "show", "/BETA/cards/BETA-1", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("error = %+v", ce)
	}
}

func TestAMisdirectedOrMalformedAddress(t *testing.T) {
	targetEnv(t)
	_, err := execCmd("card", "show", "/BETA/knowledge/notes")
	ce := coreErr(t, err)
	if ce.Code != "wrong_collection" || !strings.Contains(ce.Fix, "trellis knowledge show /BETA/knowledge/notes") {
		t.Errorf("error = %+v", ce)
	}
	_, err = execCmd("card", "show", "/BETA/cards/12")
	if ce := coreErr(t, err); ce.Code != "bad_path" {
		t.Errorf("error = %+v", ce)
	}
}

// Moving another project's card must not drag it onto that project's default
// board: a card named by address is worked on its own board.
func TestMovingANamedCardKeepsItsBoard(t *testing.T) {
	targetEnv(t)
	if ref := refOf(t, "card", "new", "--title", "side card", "--project", "BETA", "--board", "Side"); ref != "BETA-2" {
		t.Fatalf("side card = %s", ref)
	}
	runCmd(t, "card", "move", "BETA-2", "done")
	out := runCmd(t, "board", "show", "--json", "--project", "BETA", "--board", "Side")
	var view struct {
		Columns []struct {
			Name  string `json:"name"`
			Cards []struct {
				Ref string `json:"ref"`
			} `json:"cards"`
		} `json:"columns"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	var done []string
	for _, col := range view.Columns {
		if col.Name != "done" {
			continue
		}
		for _, c := range col.Cards {
			done = append(done, c.Ref)
		}
	}
	if !slices.Contains(done, "BETA-2") {
		t.Errorf("done on Side = %v, want BETA-2 there; columns: %+v", done, view.Columns)
	}
}

// In a pinned directory a relative card means the pinned project, so a --by
// naming another one is a conflict. An address whose project differs from
// its ref's prefix gets through the CLI only when both references name that
// project, and core refuses it there.
func TestABlockerFromAnotherProjectIsRefused(t *testing.T) {
	targetEnv(t)
	for _, by := range []string{"BETA-1", "/BETA/cards/BETA-1", "/BETA/cards/ALPHA-1"} {
		_, err := execCmd("card", "block", "1", "--by", by)
		if ce := coreErr(t, err); ce.Code != "project_conflict" {
			t.Errorf("--by %s: error = %+v", by, ce)
		}
	}
	_, err := execCmd("card", "block", "/ALPHA/cards/ALPHA-1", "--by", "/BETA/cards/BETA-1")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("two addresses, two projects: error = %+v", ce)
	}
	_, err = execCmd("card", "block", "/BETA/cards/BETA-1", "--by", "/BETA/cards/ALPHA-1")
	if ce := coreErr(t, err); ce.Code != "cross_project_block" {
		t.Errorf("ALPHA's ref under BETA's address: error = %+v", ce)
	}
}

// With no pin, a --by address is what names the project.
func TestABlockerAddressNamesTheProject(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA")
	refOf(t, "card", "new", "--title", "first", "--project", "BETA")
	refOf(t, "card", "new", "--title", "second", "--project", "BETA")
	out := runCmd(t, "card", "block", "2", "--by", "/BETA/cards/BETA-1", "--json")
	var v struct {
		Ref       string `json:"ref"`
		BlockedBy []struct {
			Ref string `json:"ref"`
		} `json:"blocked_by"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Ref != "BETA-2" || len(v.BlockedBy) != 1 || v.BlockedBy[0].Ref != "BETA-1" {
		t.Errorf("block = %+v", v)
	}
}

func TestProjectAndBoardSelectorsTakeAddresses(t *testing.T) {
	targetEnv(t)
	if got := showBoard(t, "--project", "/beta").Project; got != "BETA" {
		t.Errorf("--project /beta = %s", got)
	}
	if got := showBoard(t, "--board", "/BETA/boards/side"); got.Project != "BETA" || got.Slug != "side" {
		t.Errorf("--board address = %+v", got)
	}
	_, err := execCmd("board", "show", "--json", "--board", "/BETA/boards/side", "--project", "ALPHA")
	if ce := coreErr(t, err); ce.Code != "project_conflict" {
		t.Errorf("error = %+v", ce)
	}
	t.Setenv("TRELLIS_PROJECT", "/BETA")
	if got := showBoard(t).Project; got != "BETA" {
		t.Errorf("TRELLIS_PROJECT=/BETA = %s", got)
	}
}
