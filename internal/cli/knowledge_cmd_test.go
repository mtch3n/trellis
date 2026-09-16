package cli

import (
	"bytes"
	"context"
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
