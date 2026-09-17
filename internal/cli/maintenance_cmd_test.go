package cli

import (
	"strings"
	"testing"
)

func TestMaintenancePruneRequiresASelector(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "maintenance", "prune"); cliErrCode(err) != "nothing_to_prune" {
		t.Errorf("err = %v, want nothing_to_prune", err)
	}
}

func TestMaintenancePruneRevisionsNeedsNoBefore(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Entry")
	out := runCmd(t, "maintenance", "prune", "--revisions", "--json")
	if !strings.Contains(out, `"deleted"`) {
		t.Fatalf("out = %s, want a deleted count", out)
	}
}

func TestMaintenancePruneLeftoverRevisionsNeedsNoBefore(t *testing.T) {
	projectEnv(t)
	out := runCmd(t, "maintenance", "prune", "--orphan-history", "--json")
	if !strings.Contains(out, `"deleted":0`) {
		t.Fatalf("out = %s, want zero leftovers in a fresh vault", out)
	}
}

func TestMaintenancePruneEventsStillRequiresBefore(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "maintenance", "prune", "--events"); cliErrCode(err) != "invalid_retention" {
		t.Errorf("err = %v, want invalid_retention", err)
	}
}
