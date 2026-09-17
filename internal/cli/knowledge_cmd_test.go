package cli

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// projectEnv names the project through TRELLIS_PROJECT, so a command needs no
// pin. A named project is looked up, never created, so it is seeded first.
func projectEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TRELLIS_HOME", t.TempDir())
	t.Setenv("TRELLIS_PROJECT", "TEST")
	seedProject(t, "TEST")
}

func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	out, err := execCmd(args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out
}

func execCmd(args ...string) (string, error) {
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
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
		{Slug: "staging-credentials", Template: "reference", Title: "Staging credentials", Private: true},
		{Slug: "recall-ranking", Template: "decision", Title: "Recall ranking"},
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

// Listing is not reading. Agents always receive the JSON form of `knowledge
// ls`, so anything in it goes to the model: no entry's body is listed, and a
// private entry's summary and recap are withheld too. The flag is set by hand
// and the listing is the very next command, so the decision has to come from
// the file; each mode gets a fresh project so neither refreshes for the other.
func TestKnowledgeLsDisclosesNoContent(t *testing.T) {
	for name, args := range map[string][]string{
		"ls":      {"knowledge", "ls"},
		"ls-cold": {"knowledge", "ls", "--cold"},
	} {
		t.Run(name, func(t *testing.T) {
			projectEnv(t)

			newEntry(t, "--title", "Recall ranking", "--summary", "ranks are fused",
				"--body", "fusion is positional\n")
			runCmd(t, "knowledge", "pin", "recall-ranking", "--recap", "fused, not scored")

			path := newEntry(t, "--title", "Staging credentials", "--summary", "swordfish opens staging",
				"--body", "hunter2 is the password\n")
			runCmd(t, "knowledge", "pin", "staging-credentials", "--recap", "rotated quarterly")
			markPrivateByHand(t, path)

			out := runCmd(t, args...)
			for _, secret := range []string{"fusion is positional", "hunter2", "swordfish", "rotated quarterly"} {
				if strings.Contains(out, secret) {
					t.Errorf("the listing carries %q:\n%s", secret, out)
				}
			}

			var listing struct {
				Knowledge []map[string]any `json:"knowledge"`
			}
			if err := json.Unmarshal([]byte(out), &listing); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			entries := map[string]map[string]any{}
			for _, e := range listing.Knowledge {
				slug, _ := e["slug"].(string)
				entries[slug] = e
				if _, ok := e["body"]; ok {
					t.Errorf("%s: the listing carries a body", slug)
				}
			}

			open, secret := entries["recall-ranking"], entries["staging-credentials"]
			if open == nil || secret == nil {
				t.Fatalf("both entries must be listed:\n%s", out)
			}
			if open["summary"] != "ranks are fused" || open["recap"] != "fused, not scored" {
				t.Errorf("ordinary entry = %v, want its summary and recap", open)
			}
			if secret["private"] != true || secret["title"] != "Staging credentials" {
				t.Errorf("private entry = %v, want it listed as private with its title", secret)
			}
			for _, field := range []string{"summary", "recap"} {
				if v, ok := secret[field]; ok {
					t.Errorf("private entry carries %s %q", field, v)
				}
			}
		})
	}
}

// One malformed flag in any vault file fails every command, because the CLI
// sweeps the vault before running one. The failure is deliberate; not saying
// which file caused it is not. `card ls` never touches knowledge, which is
// what makes the missing path so hard to act on.
func TestABadPrivateValueNamesTheFileInEveryCommand(t *testing.T) {
	projectEnv(t)

	path := newEntry(t, "--title", "Staging credentials")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	bad := strings.Replace(string(raw), "title:", "private: \"true\"\ntitle:", 1)
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = execCmd("card", "ls")
	coreErr, ok := errors.AsType[*core.Error](err)
	if !ok || coreErr.Code != "bad_frontmatter" {
		t.Fatalf("card ls = %v, want bad_frontmatter", err)
	}
	if !strings.Contains(coreErr.Msg, path) {
		t.Errorf("message %q does not name %s", coreErr.Msg, path)
	}
}
