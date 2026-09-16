package cli

import (
	"strings"
	"testing"
)

func TestKnowledgeNewInFlagNestsTheSlug(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "knowledge", "new", "--title", "Rollback runbook", "--in", "deployment", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the nested slug:\n%s", out)
	}
}

func TestKnowledgeNewInFlagRefusesAResemblingDirectory(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "First", "--in", "deployment")

	_, err := runCmdErr(t, "knowledge", "new", "--title", "Second", "--in", "deploymnet")
	if err == nil {
		t.Fatal("a directory one typo away from an existing one must be refused")
	}
	if !strings.Contains(err.Error(), "deployment") {
		t.Errorf("the error must name the directory it resembles: %v", err)
	}

	// --new-dir is how the caller says they meant it.
	out := runCmd(t, "knowledge", "new", "--title", "Third", "--in", "deploymnet", "--new-dir", "--json")
	if !strings.Contains(out, `"slug":"deploymnet/third"`) {
		t.Errorf("--new-dir did not create the directory:\n%s", out)
	}
}

func TestKnowledgeMvCommand(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Rollback")
	out := runCmd(t, "knowledge", "mv", "rollback", "deployment/rollback-runbook", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the new slug:\n%s", out)
	}
	show := runCmd(t, "knowledge", "show", "deployment/rollback-runbook")
	if !strings.Contains(show, "deployment/rollback-runbook") {
		t.Errorf("show does not find the moved entry:\n%s", show)
	}
}
