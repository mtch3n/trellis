package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// remindUnnominated runs `agent remind --json` and returns the refs it lists
// as unnominated.
func remindUnnominated(t *testing.T) []string {
	t.Helper()
	var out struct {
		Unnominated []struct {
			Ref   string `json:"ref"`
			Title string `json:"title"`
		} `json:"unnominated_entries"`
	}
	raw := runCmd(t, "agent", "remind", "--json")
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("agent remind: %q, %v", raw, err)
	}
	if out.Unnominated == nil {
		t.Fatalf("agent remind has no unnominated_entries list: %s", raw)
	}
	refs := []string{}
	for _, e := range out.Unnominated {
		if e.Title == "" {
			t.Errorf("%s is listed without its title", e.Ref)
		}
		refs = append(refs, e.Ref)
	}
	slices.Sort(refs)
	return refs
}

func TestGate_GATE_C1_remind_lists_entries_this_actor_created_or_edited(t *testing.T) {
	projectEnv(t)
	t.Setenv("TRELLIS_AGENT", "agent:other")
	runCmd(t, "vault", "new", "--title", "Entry B", "--body", "first draft\n")

	t.Setenv("TRELLIS_AGENT", "agent:me")
	runCmd(t, "vault", "new", "--title", "Entry A", "--body", "mine\n")
	runCmd(t, "vault", "edit", "entry-b", "--body", "my rewrite\n", "--if-version", "1")

	got := remindUnnominated(t)
	want := []string{"/TEST/vault/entry-a", "/TEST/vault/entry-b"}
	if !slices.Equal(got, want) {
		t.Fatalf("unnominated_entries = %v, want %v", got, want)
	}
}

func TestGate_GATE_C2_remind_skips_others_global_and_nominated_entries(t *testing.T) {
	projectEnv(t)
	t.Setenv("TRELLIS_AGENT", "agent:other")
	runCmd(t, "vault", "new", "--title", "Theirs", "--body", "not mine\n")

	t.Setenv("TRELLIS_AGENT", "agent:me")
	runCmd(t, "vault", "new", "--title", "Nominated", "--body", "mine, argued for\n")
	runCmd(t, "vault", "nominate", "nominated", "--reason", "every repo re-derives this")
	runCmd(t, "vault", "new", "--title", "Promoted", "--body", "mine, now global\n")
	promoteByHand(t, "TEST", "promoted")

	if got := remindUnnominated(t); len(got) != 0 {
		t.Fatalf("unnominated_entries = %v, want none", got)
	}
}

func TestGate_GATE_C3_remind_outside_a_project_lists_nothing(t *testing.T) {
	markerEnv(t, "nowhere")
	t.Setenv("TRELLIS_AGENT", "agent:me")

	out, err := execCmd("agent", "remind", "--json")
	if err != nil {
		t.Fatalf("agent remind outside a project: %v\n%s", err, out)
	}
	var got struct {
		Unnominated []json.RawMessage `json:"unnominated_entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("agent remind: %q, %v", out, err)
	}
	if got.Unnominated == nil || len(got.Unnominated) != 0 {
		t.Fatalf("unnominated_entries = %s, want an empty list", out)
	}
}

// A person running remind without --json sees waiting entries too, even with
// no claimed card.
func TestRemindTextListsUnnominatedEntriesWithNoClaimedCard(t *testing.T) {
	out := formatRemind(nil, []core.UnnominatedEntry{{Ref: "/TEST/vault/entry-a", Title: "Entry A"}})
	if !strings.Contains(out, "/TEST/vault/entry-a") || !strings.Contains(out, "vault nominate") {
		t.Fatalf("remind text = %q, want the entry and how to nominate it", out)
	}
	if strings.Contains(out, "claim work") {
		t.Errorf("remind text mentions claimed work with none claimed: %q", out)
	}
}
