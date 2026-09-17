package cli

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestVaultHistoryAndDiff(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Notes", "--body", "line one")
	runCmd(t, "vault", "edit", "notes", "--body", "line one\nline two", "--if-version", "1")

	history := runCmd(t, "vault", "history", "notes", "--json")
	if !strings.Contains(history, `"version":2`) || !strings.Contains(history, `"version":1`) {
		t.Fatalf("history = %s, want both versions", history)
	}

	diff := runCmd(t, "vault", "diff", "notes", "--json")
	if !strings.Contains(diff, `"from":1`) || !strings.Contains(diff, `"to":2`) {
		t.Fatalf("diff = %s, want from 1 to 2", diff)
	}
	if !strings.Contains(diff, "line two") {
		t.Fatalf("diff = %s, want it to mention the added line", diff)
	}
}

func TestVaultDiffRejectsAnUnretainedVersion(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Solo")

	if _, err := runCmdErr(t, "vault", "diff", "solo", "--from", "9", "--to", "9"); cliErrCode(err) != "revision_not_retained" {
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

// review-cli #9: card history/diff, vault history/diff and vault mv
// never adopted withTarget, so a reference that names its own project failed
// with unresolved outside any marker, unlike every other reference-taking
// command.
func TestHistoryDiffAndMvNeedNoMarker(t *testing.T) {
	markerEnv(t, "loose")
	seedProject(t, "BETA")

	created := runCmd(t, "card", "new", "--title", "Ship", "--body", "draft", "--project", "BETA", "--json")
	var card core.Card
	if err := json.Unmarshal([]byte(created), &card); err != nil {
		t.Fatal(err)
	}
	runCmd(t, "card", "edit", card.Ref, "--body", "final", "--if-version", "1", "--project", "BETA")

	for _, args := range [][]string{
		{"card", "history", "/BETA/cards/" + card.Ref},
		{"card", "history", card.Ref},
		{"card", "diff", card.Ref},
	} {
		if _, err := runCmdErr(t, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}

	refOf(t, "vault", "new", "--title", "Notes", "--project", "BETA")
	runCmd(t, "vault", "edit", "notes", "--body", "changed", "--if-version", "1", "--project", "BETA")
	for _, args := range [][]string{
		{"vault", "history", "/BETA/vault/notes"},
		{"vault", "diff", "/BETA/vault/notes"},
	} {
		if _, err := runCmdErr(t, args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}

	if got := refOf(t, "vault", "mv", "/BETA/vault/notes", "notes-2"); got != "/BETA/vault/notes-2" {
		t.Errorf("vault mv with no marker = %s", got)
	}
}

// vault history on a /GLOBAL address needs no project at all, exactly as
// vault show already does.
func TestVaultHistoryOnAGlobalEntryNeedsNoMarker(t *testing.T) {
	markerEnv(t, "loose")
	seedProject(t, "ALPHA")
	refOf(t, "vault", "new", "--title", "Conventions", "--project", "ALPHA")
	promoteByHand(t, "ALPHA", "conventions")

	if _, err := runCmdErr(t, "vault", "history", "/GLOBAL/vault/conventions"); err != nil {
		t.Errorf("vault history on a global entry with no marker: %v", err)
	}
}

// review-cli #9: card relate never adopted withTargets for its second card,
// so it could not name a project of its own -- the same rule card block
// already follows for --by. With no marker, an address is what names the
// project: TestABlockerAddressNamesTheProject's pattern, for relate.
func TestCardRelateOtherCardNamesItsProject(t *testing.T) {
	markerEnv(t, "loose")
	seedProject(t, "BETA")
	refOf(t, "card", "new", "--title", "first", "--project", "BETA")
	refOf(t, "card", "new", "--title", "second", "--project", "BETA")
	out := runCmd(t, "card", "relate", "2", "relates-to", "/BETA/cards/BETA-1", "--json")
	if !strings.Contains(out, `"ref":"BETA-2"`) || !strings.Contains(out, `"BETA-1"`) {
		t.Errorf("card relate with an addressed other card = %s", out)
	}
}
