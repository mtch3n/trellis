package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestKnowledgeHistoryAndDiff(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Notes", "--body", "line one")
	runCmd(t, "knowledge", "edit", "notes", "--body", "line one\nline two", "--if-version", "1")

	history := runCmd(t, "knowledge", "history", "notes", "--json")
	if !strings.Contains(history, `"version":2`) || !strings.Contains(history, `"version":1`) {
		t.Fatalf("history = %s, want both versions", history)
	}

	diff := runCmd(t, "knowledge", "diff", "notes", "--json")
	if !strings.Contains(diff, `"from":1`) || !strings.Contains(diff, `"to":2`) {
		t.Fatalf("diff = %s, want from 1 to 2", diff)
	}
	if !strings.Contains(diff, "line two") {
		t.Fatalf("diff = %s, want it to mention the added line", diff)
	}
}

func TestKnowledgeDiffRejectsAnUnretainedVersion(t *testing.T) {
	projectEnv(t)
	runCmd(t, "knowledge", "new", "--title", "Solo")

	if _, err := runCmdErr(t, "knowledge", "diff", "solo", "--from", "9", "--to", "9"); cliErrCode(err) != "revision_not_retained" {
		t.Errorf("err = %v, want revision_not_retained", err)
	}
}

func TestCardHistoryAndDiff(t *testing.T) {
	projectEnv(t)
	created := runCmd(t, "card", "new", "--title", "Ship", "--body", "draft", "--json")
	var card core.Card
	if err := json.Unmarshal([]byte(created), &card); err != nil {
		t.Fatalf("decode created card: %v", err)
	}
	runCmd(t, "card", "edit", card.Ref, "--body", "final", "--if-version", "1")

	history := runCmd(t, "card", "history", card.Ref, "--json")
	if !strings.Contains(history, `"actor"`) {
		t.Fatalf("history = %s, want an actor field", history)
	}

	diff := runCmd(t, "card", "diff", card.Ref, "--json")
	if !strings.Contains(diff, "-draft") || !strings.Contains(diff, "+final") {
		t.Fatalf("diff = %s, want draft removed and final added", diff)
	}
}
