package cli

import (
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestVaultNewInFlagNestsTheSlug(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "vault", "new", "--title", "Rollback runbook", "--in", "deployment", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the nested slug:\n%s", out)
	}
}

func TestVaultNewInFlagRefusesAResemblingDirectory(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "First", "--in", "deployment")

	_, err := runCmdErr(t, "vault", "new", "--title", "Second", "--in", "deploymnet")
	if err == nil {
		t.Fatal("a directory one typo away from an existing one must be refused")
	}
	if !strings.Contains(err.Error(), "deployment") {
		t.Errorf("the error must name the directory it resembles: %v", err)
	}

	// --new-directory is how the caller says they meant it.
	out := runCmd(t, "vault", "new", "--title", "Third", "--in", "deploymnet", "--new-directory", "--json")
	if !strings.Contains(out, `"slug":"deploymnet/third"`) {
		t.Errorf("--new-directory did not create the directory:\n%s", out)
	}
}

func TestVaultMvCommand(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Rollback")
	out := runCmd(t, "vault", "mv", "rollback", "deployment/rollback-runbook", "--json")
	if !strings.Contains(out, `"slug":"deployment/rollback-runbook"`) {
		t.Errorf("output does not carry the new slug:\n%s", out)
	}
	show := runCmd(t, "vault", "show", "deployment/rollback-runbook")
	if !strings.Contains(show, "deployment/rollback-runbook") {
		t.Errorf("show does not find the moved entry:\n%s", show)
	}
}

func TestVaultLsRendersATreeGroupedByDirectory(t *testing.T) {
	entries := []core.Entry{
		{Slug: "recall-ranking", Template: "decision", Title: "Recall ranking"},
		{Slug: "deployment/rollback", Template: "runbook", Title: "Rollback"},
		{Slug: "docs/rollback", Template: "decision", Title: "Rollback (docs)"},
	}
	out := renderEntryList(entries)
	if !strings.Contains(out, "deployment/\n") || !strings.Contains(out, "docs/\n") {
		t.Fatalf("output does not group by directory:\n%s", out)
	}
	if strings.Index(out, "deployment/") > strings.Index(out, "recall-ranking") {
		t.Fatalf("directories must sort alongside their leaf names:\n%s", out)
	}
}

func TestVaultLsDirArgumentScopesToASubtree(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Rollback", "--in", "deployment")
	runCmd(t, "vault", "new", "--title", "Elsewhere", "--in", "docs")
	out := runCmd(t, "vault", "ls", "deployment", "--json")
	if !strings.Contains(out, "deployment/rollback") || strings.Contains(out, "docs/elsewhere") {
		t.Errorf("ls deployment did not scope correctly:\n%s", out)
	}
}

func TestVaultLsTagFlagRequiresAllTags(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Both", "--tag", "a", "--tag", "b")
	runCmd(t, "vault", "new", "--title", "OnlyA", "--tag", "a")
	out := runCmd(t, "vault", "ls", "--tag", "a", "--tag", "b", "--json")
	if !strings.Contains(out, "\"slug\":\"both\"") || strings.Contains(out, "\"slug\":\"onlya\"") {
		t.Errorf("--tag a --tag b did not narrow to the entry with both:\n%s", out)
	}
}
