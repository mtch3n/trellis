package cli

import (
	"encoding/json"
	"slices"
	"testing"
)

// lsUnverified runs `vault ls --unverified --json` and returns the listed refs.
func lsUnverified(t *testing.T) []string {
	t.Helper()
	var out struct {
		Entries []struct {
			Ref string `json:"ref"`
		} `json:"entries"`
	}
	raw := runCmd(t, "vault", "ls", "--unverified", "--json")
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("vault ls --unverified: %q, %v", raw, err)
	}
	if out.Entries == nil {
		t.Fatalf("vault ls --unverified has no entries list: %s", raw)
	}
	refs := []string{}
	for _, e := range out.Entries {
		refs = append(refs, e.Ref)
	}
	return refs
}

func TestGate_GATE_C13_ls_unverified_lists_only_overdue_global_entries(t *testing.T) {
	projectEnv(t)
	promotedPastVerify(t, "Overdue rule")
	runCmd(t, "vault", "new", "--title", "Fresh rule", "--body", "promoted today\n")
	promoteByHand(t, "TEST", "fresh-rule")
	runCmd(t, "vault", "new", "--title", "Project note", "--body", "stays here\n")

	got := lsUnverified(t)
	if !slices.Equal(got, []string{"/GLOBAL/vault/overdue-rule"}) {
		t.Fatalf("vault ls --unverified = %v, want only /GLOBAL/vault/overdue-rule", got)
	}
}

func TestGate_GATE_C14_ls_unverified_with_nothing_overdue_is_empty(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Fresh rule", "--body", "promoted today\n")
	promoteByHand(t, "TEST", "fresh-rule")

	if got := lsUnverified(t); len(got) != 0 {
		t.Fatalf("vault ls --unverified = %v, want an empty list", got)
	}
}
