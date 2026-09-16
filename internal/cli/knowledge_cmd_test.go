package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

// TRELLIS_PROJECT is the first branch of resolve.Identify, so it needs neither
// a git repository nor a .trellis pin. TRELLIS_HOME keeps the vault and the
// database inside the test's temp directory.
//
// Setting TRELLIS_PROJECT is not by itself enough to run a command, though:
// currentBoard (internal/cli/root.go) treats any named project -- --project
// or TRELLIS_PROJECT -- as a selection, not a resolution. It calls
// core.ProjectByKey, which errors on a miss, rather than resolve.Identify +
// EnsureProject, which is the only path that creates a project (and its
// seeded default board). resolve.Identify's own env branch is therefore
// never reached through the CLI in this mode -- it only fires on the
// cwd-resolution path, which a named project skips entirely. So the project
// has to be seeded here, through the identity resolve.Identify would itself
// produce for TRELLIS_PROJECT, before any command runs.
func projectEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	t.Setenv("TRELLIS_PROJECT", "TEST")

	path, err := home.DBPath()
	if err != nil {
		t.Fatalf("home.DBPath: %v", err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	// dir is unused: TRELLIS_PROJECT is already set, so Identify short-circuits
	// before it ever looks at the filesystem.
	id, err := resolve.Identify(".")
	if err != nil {
		db.Close()
		t.Fatalf("resolve.Identify: %v", err)
	}
	if _, err := core.New(db, core.RealClock{}, "test").EnsureProject(context.Background(), id); err != nil {
		db.Close()
		t.Fatalf("EnsureProject: %v", err)
	}
	// Closed before any command opens its own handle on the same file: an
	// open handle here would keep the database locked, and on Windows would
	// keep the temp directory from being removed during t.TempDir() cleanup.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

func TestKnowledgeNewPrivateFlag(t *testing.T) {
	projectEnv(t)

	out := runCmd(t, "knowledge", "new", "--title", "Staging credentials", "--private", "--json")
	if !strings.Contains(out, `"private":true`) {
		t.Errorf("output did not report the flag:\n%s", out)
	}
}

// The renderer is tested directly: Emit picks JSON when stdout is captured, so
// asserting the marker through the command would assert nothing.
func TestRenderKnowledgeListMarksPrivate(t *testing.T) {
	got := renderKnowledgeList([]core.Knowledge{
		{Slug: "staging-credentials", DocType: "reference", Title: "Staging credentials", Private: true},
		{Slug: "recall-ranking", DocType: "decision", Title: "Recall ranking"},
	})

	var secret, open string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "staging-credentials") {
			secret = line
		}
		if strings.Contains(line, "recall-ranking") {
			open = line
		}
	}
	if secret == "" || open == "" {
		t.Fatalf("both rows must render:\n%s", got)
	}
	if !strings.Contains(secret, "private") {
		t.Errorf("private entry is not marked:\n%s", secret)
	}
	if strings.Contains(open, "private") {
		t.Errorf("ordinary entry is marked private:\n%s", open)
	}
}

// newEntry creates an entry through the CLI and returns its file path.
func newEntry(t *testing.T, args ...string) string {
	t.Helper()
	out := runCmd(t, append([]string{"knowledge", "new", "--json"}, args...)...)
	var doc struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.Path == "" {
		t.Fatalf("knowledge new output %q: path %q, err %v", out, doc.Path, err)
	}
	return doc.Path
}

// markPrivateByHand sets the flag the way an author with an editor would,
// without any trellis command reading the entry in between.
func markPrivateByHand(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	marked := strings.Replace(string(raw), "title:", "private: true\ntitle:", 1)
	if err := os.WriteFile(path, []byte(marked), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func lineWith(text, needle string) string {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

// A private pin is a pointer, ref and title. The brief shows the title, since
// there is no recap, and never calls the pointer stale, since there is no
// recap to be out of date. The brief is read twice: staleness was computed
// from the row before the refresh purged recap_hash, so the first read and the
// second used to disagree about an entry marked private by hand.
func TestBriefShowsAPrivatePinAsAPointer(t *testing.T) {
	projectEnv(t)

	newEntry(t, "--title", "Staging credentials", "--private", "--body", "hunter2 opens staging\n")
	runCmd(t, "knowledge", "pin", "staging-credentials")

	path := newEntry(t, "--title", "Deploy runbook", "--body", "ship it\n")
	runCmd(t, "knowledge", "pin", "deploy-runbook", "--recap", "swordfish deploys")
	markPrivateByHand(t, path)

	for read := 1; read <= 2; read++ {
		brief := runCmd(t, "board", "show", "--brief")
		for slug, title := range map[string]string{
			"staging-credentials": "Staging credentials",
			"deploy-runbook":      "Deploy runbook",
		} {
			line := lineWith(brief, slug)
			if line == "" {
				t.Fatalf("read %d: %s is missing from the brief:\n%s", read, slug, brief)
			}
			if !strings.Contains(line, title) {
				t.Errorf("read %d: pointer %q does not carry the title %q", read, line, title)
			}
			if strings.Contains(line, "(stale)") {
				t.Errorf("read %d: pointer %q is marked stale; it has no recap to be stale", read, line)
			}
		}
		for _, secret := range []string{"hunter2", "swordfish"} {
			if strings.Contains(brief, secret) {
				t.Errorf("read %d: the brief carries %q:\n%s", read, secret, brief)
			}
		}
	}
}

// A pin with a real recap still shows the recap, and still says when it has
// gone stale.
func TestBriefStillMarksAStaleRecap(t *testing.T) {
	projectEnv(t)

	path := newEntry(t, "--title", "Deploy runbook", "--body", "ship it\n")
	runCmd(t, "knowledge", "pin", "deploy-runbook", "--recap", "ships on green")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(path, append(raw, "\nship it twice\n"...), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read twice. Staleness is computed from the row before the refresh that
	// notices the edit, so an ordinary recap shows as stale from the second
	// read on. That lag predates the private flag and is not what this checks.
	var line string
	for range 2 {
		line = lineWith(runCmd(t, "board", "show", "--brief"), "deploy-runbook")
	}
	if !strings.Contains(line, "ships on green") || !strings.Contains(line, "(stale)") {
		t.Errorf("pin line = %q, want the recap marked stale", line)
	}
}
