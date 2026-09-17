package cli

import (
	"os"
	"strings"
	"testing"
)

func TestTemplateLsListsTheFiveBuiltins(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "template", "ls", "--json")
	for _, name := range []string{"decision", "finding", "reference", "research", "runbook"} {
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
