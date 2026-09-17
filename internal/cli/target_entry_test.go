package cli

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// promoteByHand does what a human does at a terminal: escalate is refused to
// agents and needs a TTY, so tests go through core.
func promoteByHand(t *testing.T, key, slug string) {
	t.Helper()
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.RealClock{}, "test", root)
	p, err := c.ProjectByKey(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, slug, "shared"); err != nil {
		t.Fatal(err)
	}
}

// refsIn returns the "ref" of every object in the array under field. The
// payload is read loosely because graph output mixes a string root with its
// arrays.
func refsIn(t *testing.T, out, field string) []string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	items, _ := v[field].([]any)
	var refs []string
	for _, item := range items {
		obj, _ := item.(map[string]any)
		ref, _ := obj["ref"].(string)
		refs = append(refs, ref)
	}
	return refs
}

func TestEveryPrintedEntryRefOpens(t *testing.T) {
	targetEnv(t)
	const want = "/ALPHA/vault/lease-renewal"
	if got := refOf(t, "knowledge", "new", "--title", "Lease renewal", "--body", "A claim starts the lease.\n"); got != want {
		t.Fatalf("created = %s", got)
	}
	refs := refsIn(t, runCmd(t, "search", "lease", "--json"), "results")
	refs = append(refs, refsIn(t, runCmd(t, "recall", "why did the lease expire", "--json"), "results")...)
	if len(refs) != 2 {
		t.Fatalf("refs = %v", refs)
	}
	for _, ref := range refs {
		if got := refOf(t, "knowledge", "show", ref); got != want {
			t.Errorf("knowledge show %s = %s", ref, got)
		}
	}
}

func TestAnEntryAddressNamesItsProject(t *testing.T) {
	targetEnv(t)
	const want = "/BETA/vault/runbook"
	if got := refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA"); got != want {
		t.Fatalf("created = %s", got)
	}
	if got := refOf(t, "knowledge", "show", want); got != want {
		t.Errorf("knowledge show = %s", got)
	}
	_, err := execCmd("knowledge", "show", "runbook")
	if ce := coreErr(t, err); ce.Code != "knowledge_not_found" {
		t.Errorf("a relative slug stays in ALPHA: %+v", ce)
	}
}

// A vault address consults nothing ambient: no pin, broken or stale, and no
// TRELLIS_PROJECT stands between a reader and the vault.
func TestAVaultAddressIgnoresAmbientState(t *testing.T) {
	dir := pinEnv(t, "loose")
	seedProject(t, "ALPHA")
	refOf(t, "knowledge", "new", "--title", "Conventions", "--project", "ALPHA")
	promoteByHand(t, "ALPHA", "conventions")
	const want = "/GLOBAL/vault/conventions"

	_, err := execCmd("knowledge", "show", "conventions")
	if ce := coreErr(t, err); ce.Code != "unresolved" {
		t.Errorf("a relative slug still needs a project: %+v", ce)
	}

	// Each state builds on the one before; the last has a stale pin and an
	// environment naming a project that does not exist.
	for _, state := range []struct {
		name  string
		setup func()
	}{
		{"no pin", func() {}},
		{"malformed pin", func() { writePin(t, dir, "ALPHA\n") }},
		{"stale pin", func() { writePin(t, dir, "/GHOST\n") }},
		{"bad env", func() { t.Setenv("TRELLIS_PROJECT", "NOPE") }},
	} {
		state.setup()
		name := state.name
		if got := refOf(t, "knowledge", "show", want); got != want {
			t.Errorf("%s: knowledge show = %s", name, got)
		}
		var shown struct {
			Version int64 `json:"version"`
		}
		if err := json.Unmarshal([]byte(runCmd(t, "knowledge", "show", want, "--json")), &shown); err != nil {
			t.Fatalf("%s: knowledge show: %v", name, err)
		}
		if got := refOf(t, "knowledge", "edit", want, "--body", "Edited under "+name+".\n",
			"--if-version", strconv.FormatInt(shown.Version, 10)); got != want {
			t.Errorf("%s: knowledge edit = %s", name, got)
		}
		if nodes := refsIn(t, runCmd(t, "graph", want, "--json"), "nodes"); len(nodes) == 0 || nodes[0] != want {
			t.Errorf("%s: graph = %v", name, nodes)
		}
	}
}

// A flag that takes a reference names the command's project as a positional
// one does, so these all work with no pin at all.
func TestReferenceFlagsNameTheProject(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA", "Side")
	refOf(t, "card", "new", "--title", "beta one", "--project", "BETA")
	refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA")

	out := runCmd(t, "knowledge", "new", "--title", "Side notes", "--board", "/BETA/boards/side", "--json")
	var entry struct {
		Ref   string `json:"ref"`
		Board string `json:"board"`
	}
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Ref != "/BETA/vault/side-notes" || entry.Board != "Side" {
		t.Errorf("knowledge new = %+v", entry)
	}

	runCmd(t, "knowledge", "pin", "runbook", "--board", "/BETA/boards/side", "--recap", "roll back first")
	out = runCmd(t, "knowledge", "pins", "--project", "BETA", "--board", "Side", "--json")
	var pins struct {
		Pins []struct {
			Slug  string `json:"slug"`
			Board string `json:"board"`
		} `json:"pins"`
	}
	if err := json.Unmarshal([]byte(out), &pins); err != nil {
		t.Fatal(err)
	}
	if len(pins.Pins) != 1 || pins.Pins[0].Slug != "runbook" || pins.Pins[0].Board != "Side" {
		t.Errorf("pins = %+v", pins.Pins)
	}

	file := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(file, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	const shot = "/BETA/artifacts/shot.png"
	if got := refOf(t, "artifact", "add", file, "--card", "/BETA/cards/BETA-1"); got != shot {
		t.Errorf("artifact add = %s", got)
	}
	second := filepath.Join(t.TempDir(), "other.png")
	if err := os.WriteFile(second, []byte("also not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCmd(t, "artifact", "add", second, "--project", "BETA")
	runCmd(t, "artifact", "link", "other.png", "--card", "/BETA/cards/BETA-1")
	got := refsIn(t, runCmd(t, "artifact", "ls", "--card", "/BETA/cards/BETA-1", "--json"), "artifacts")
	if len(got) != 2 {
		t.Errorf("linked to BETA-1 = %v, want both", got)
	}
}

func TestReferenceFlagsThatDisagreeConflict(t *testing.T) {
	targetEnv(t)
	for _, args := range [][]string{
		// a relative positional means the pinned ALPHA
		{"knowledge", "pin", "runbook", "--board", "/BETA/boards/side", "--recap", "x"},
		{"artifact", "link", "shot.png", "--card", "/BETA/cards/BETA-1"},
		// two references, two projects
		{"knowledge", "pin", "/ALPHA/vault/runbook", "--board", "/BETA/boards/side", "--recap", "x"},
		{"artifact", "link", "/ALPHA/artifacts/shot.png", "--card", "/BETA/cards/BETA-1"},
	} {
		_, err := execCmd(args...)
		if ce := coreErr(t, err); ce.Code != "project_conflict" {
			t.Errorf("%v: error = %+v", args, ce)
		}
	}
}

func TestLinkAndGraphCrossProjects(t *testing.T) {
	targetEnv(t)
	refOf(t, "knowledge", "new", "--title", "Runbook", "--project", "BETA")
	runCmd(t, "link", "1", "/BETA/vault/runbook")
	nodes := refsIn(t, runCmd(t, "graph", "1", "--json"), "nodes")
	found := false
	for _, ref := range nodes {
		found = found || ref == "/BETA/vault/runbook"
	}
	if !found {
		t.Errorf("graph nodes = %v", nodes)
	}
	if got := refsIn(t, runCmd(t, "graph", "BETA-1", "--json"), "nodes"); len(got) == 0 || got[0] != "BETA-1" {
		t.Errorf("graph BETA-1 = %v", got)
	}
	if got := refsIn(t, runCmd(t, "graph", "/BETA/vault/runbook", "--json"), "nodes"); len(got) == 0 {
		t.Errorf("graph from an address = %v", got)
	}
	_, err := execCmd("graph", "/BETA/boards/side")
	if ce := coreErr(t, err); ce.Code != "wrong_collection" {
		t.Errorf("graph from a board: %+v", ce)
	}
}

func TestBoardCommandsTakeAnAddress(t *testing.T) {
	pinEnv(t, "loose")
	seedProject(t, "BETA", "Side")
	runCmd(t, "board", "default", "/BETA/boards/side")
	if got := showBoard(t, "--project", "BETA").Slug; got != "side" {
		t.Errorf("default board = %s, want side", got)
	}
	runCmd(t, "board", "rename", "/BETA/boards/side", "Sidecar")
	if got := showBoard(t, "--project", "BETA"); got.Slug != "side" {
		t.Errorf("rename changed the slug: %+v", got)
	}
}

func TestArtifactsTakeNamesAndAddresses(t *testing.T) {
	targetEnv(t)
	file := filepath.Join(t.TempDir(), "screen shot.png")
	if err := os.WriteFile(file, []byte("not really an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	const want = "/ALPHA/artifacts/screen-shot.png"
	if got := refOf(t, "artifact", "add", file); got != want {
		t.Fatalf("added = %s", got)
	}
	runCmd(t, "artifact", "link", "screen-shot.png", "--card", "/ALPHA/cards/ALPHA-1")
	if got := refsIn(t, runCmd(t, "artifact", "ls", "--card", "ALPHA-1", "--json"), "artifacts"); len(got) != 1 || got[0] != want {
		t.Errorf("linked = %v", got)
	}
	runCmd(t, "artifact", "rm", want)
	if got := refsIn(t, runCmd(t, "artifact", "ls", "--json"), "artifacts"); len(got) != 0 {
		t.Errorf("after rm = %v", got)
	}
}

// The workspace is bound to one project. An address naming another project
// is refused here; a ref's prefix is core's to judge, because after a merge
// MONO holds cards named API-1.
func TestTheWorkspaceStaysInItsProject(t *testing.T) {
	app := &appCtx{Project: core.Project{Key: "ALPHA"}}
	for arg, want := range map[string]core.CardRef{
		"4":                  {Seq: 4},
		"ALPHA-3":            {Seq: 3, ProjectKey: "ALPHA"},
		"API-1":              {Seq: 1, ProjectKey: "API"},
		"/ALPHA/cards/API-1": {Seq: 1, ProjectKey: "API", Project: "ALPHA"},
	} {
		got, err := tuiCardRef(app, arg)
		if err != nil || got != want {
			t.Errorf("tuiCardRef(%q) = %+v, %v", arg, got, err)
		}
	}
	_, err := tuiCardRef(app, "/BETA/cards/BETA-1")
	if ce := coreErr(t, err); ce.Code != "wrong_project" {
		t.Errorf("another project's address: %+v", ce)
	}
	_, err = tuiCardRef(app, "/ALPHA/cards/12")
	if ce := coreErr(t, err); ce.Code != "bad_path" {
		t.Errorf("a malformed address: %+v", ce)
	}
}

// Inside the workspace, core still refuses a prefix naming another project
// until the merge layer stores refs.
func TestTheWorkspaceLeavesPrefixesToCore(t *testing.T) {
	targetEnv(t)
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.RealClock{}, "test", root)
	p, err := c.ProjectByKey(t.Context(), "ALPHA")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := tuiCardRef(&appCtx{Core: c, Project: p}, "BETA-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetCard(t.Context(), p.ID, ref)
	if ce := coreErr(t, err); ce.Code != "wrong_project" {
		t.Errorf("GetCard(BETA-1) in ALPHA: %+v", ce)
	}
}
