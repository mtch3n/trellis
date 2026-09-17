package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateLsListsTheBuiltins(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "template", "ls", "--json")
	for _, name := range []string{"decision", "finding", "glossary", "reference", "research", "runbook"} {
		if !strings.Contains(out, `"`+name+`"`) {
			t.Errorf("ls does not list %s:\n%s", name, out)
		}
	}
	if strings.Contains(out, `"note"`) {
		t.Errorf("ls should not list note (removed):\n%s", out)
	}
}

func TestTemplateShowReturnsRulesAndSkeleton(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "template", "show", "runbook")
	if !strings.Contains(out, "## Preconditions") {
		t.Errorf("show did not print the skeleton:\n%s", out)
	}
}

func TestTemplateNewRefusesAnExistingName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "custom")
	if _, err := runCmdErr(t, "knowledge", "template", "new", "custom"); cliErrCode(err) != "template_exists" {
		t.Errorf("second new: err = %v, want template_exists", err)
	}
}

// review-knowledge #2 / review-cli #1: a template name is joined straight
// into a filesystem path under <root>/templates. "../" in the name must
// never let template new/edit/rm read, overwrite or delete a file outside
// that directory -- in particular, a knowledge entry's own file.
func TestTemplateNewEditRmRefusePathTraversalNames(t *testing.T) {
	projectEnv(t)
	entryPath := newEntry(t, "--title", "Runbook")
	traversal := "../projects/TEST/vault/runbook"

	if _, err := runCmdErr(t, "knowledge", "template", "new", "../evil"); cliErrCode(err) != "bad_template_name" {
		t.Errorf("template new ../evil: err = %v, want bad_template_name", err)
	}
	home := os.Getenv("TRELLIS_HOME")
	if _, err := os.Stat(filepath.Join(home, "evil.md")); !os.IsNotExist(err) {
		t.Errorf("template new must not have written outside <root>/templates: %v", err)
	}

	if _, err := runCmdErr(t, "knowledge", "template", "edit", traversal, "--body",
		"---\nenforce: warn\n---\npwned\n"); cliErrCode(err) != "bad_template_name" {
		t.Errorf("template edit %s: err = %v, want bad_template_name", traversal, err)
	}
	if _, err := runCmdErr(t, "knowledge", "template", "rm", traversal); cliErrCode(err) != "bad_template_name" {
		t.Errorf("template rm %s: err = %v, want bad_template_name", traversal, err)
	}

	raw, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("the entry file must survive: %v", err)
	}
	if strings.Contains(string(raw), "pwned") {
		t.Errorf("the entry file was overwritten through a template path: %q", raw)
	}
}

func TestTemplateEditThenNewEntryUsesIt(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "checklist")
	runCmd(t, "knowledge", "template", "edit", "checklist", "--body",
		"---\nenforce: warn\n---\n# {{title}}\n\n## Done\n")

	out := runCmd(t, "knowledge", "new", "--title", "Ship it", "--template", "checklist", "--json")
	if !strings.Contains(out, "checklist") {
		t.Errorf("new entry did not use the edited template's type:\n%s", out)
	}
}

func TestTemplateRmThenReinstallRestoresABuiltin(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "rm", "decision")
	if _, err := runCmdErr(t, "knowledge", "template", "show", "decision"); cliErrCode(err) != "unknown_template" {
		t.Fatalf("after rm: err = %v, want unknown_template", err)
	}
	runCmd(t, "knowledge", "template", "reinstall", "decision")
	runCmd(t, "knowledge", "template", "show", "decision")
}

func TestTemplateReinstallRefusesANonBuiltin(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "custom")
	if _, err := runCmdErr(t, "knowledge", "template", "reinstall", "custom"); cliErrCode(err) != "not_builtin" {
		t.Errorf("err = %v, want not_builtin", err)
	}
}

func TestTemplateCheckReportsWithoutBlocking(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "template", "new", "strict")
	runCmd(t, "knowledge", "template", "edit", "strict", "--body",
		"---\nenforce: reject\nrequired: [owner]\n---\n# {{title}}\n")
	runCmd(t, "knowledge", "new", "--title", "Loose")

	out := runCmd(t, "knowledge", "template", "check", "strict", "loose", "--json")
	if !strings.Contains(out, "owner") {
		t.Errorf("check did not report the missing field:\n%s", out)
	}
	// check never blocks: the entry it inspected is untouched.
	runCmd(t, "knowledge", "show", "loose")
}

func TestKnowledgeNewSetRefusesAReservedField(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "knowledge", "new", "--title", "X", "--set", "title=Y"); cliErrCode(err) != "reserved_field" {
		t.Errorf("err = %v, want reserved_field", err)
	}
}

// StringSliceVar splits on commas, so --set note=a,b would land as two
// entries ("note=a" and "b") instead of one field whose value contains a
// comma. --set uses StringArrayVar to keep the value intact.
func TestKnowledgeNewSetPreservesCommas(t *testing.T) {
	projectEnv(t)
	path := newEntry(t, "--title", "X", "--set", "note=a,b")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "note: a,b") {
		t.Errorf("file = %q, want note: a,b intact", string(raw))
	}
}
