package mcpserver_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type clientRemindOut struct {
	Cards   []platform.Card  `json:"claimed_without_comment"`
	Entries []platform.Entry `json:"unnominated_entries"`
}

func clientRemind(t *testing.T, cs *mcp.ClientSession) (refs, slugs []string) {
	t.Helper()
	got := ok[clientRemindOut](t, cs, "remind", map[string]any{})
	for _, c := range got.Cards {
		refs = append(refs, c.Ref)
	}
	for _, e := range got.Entries {
		slugs = append(slugs, e.Slug)
	}
	return refs, slugs
}

func clientEntry(t *testing.T, cs *mcp.ClientSession, slug string, extra map[string]any) {
	t.Helper()
	args := map[string]any{"action": "new", "slug": slug, "title": "Title " + slug, "body": "The body of " + slug + "."}
	maps.Copy(args, extra)
	ok[entryOut](t, cs, "vault_write", args)
}

func TestClient_CLIENT_C13_RemindListsTheUncommentedClaimAndTheOwnEntry(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", headers{"X-Trellis-Session": "s1"})
	a := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "card a"}).Card
	b := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "card b"}).Card
	ok[claimOut](t, cs, "card_claim", map[string]any{"action": "claim", "ref": a.Ref})
	ok[claimOut](t, cs, "card_claim", map[string]any{"action": "claim", "ref": b.Ref})
	ok[cardOut](t, cs, "card_write", map[string]any{"action": "comment", "ref": b.Ref, "body": "handoff"})
	clientEntry(t, cs, "entry-e", nil)

	refs, slugs := clientRemind(t, cs)
	if !slices.Equal(refs, []string{a.Ref}) {
		t.Fatalf("cards = %v, want only %s", refs, a.Ref)
	}
	if !slices.Equal(slugs, []string{"entry-e"}) {
		t.Fatalf("entries = %v, want only entry-e", slugs)
	}
}

func TestClient_CLIENT_C14_RemindLeavesOutOthersPrivateGlobalAndNominatedEntries(t *testing.T) {
	e := newEnv(t)
	ming := e.agent("ming", "claude-laptop", nil)
	lin := e.agent("lin", "claude-desktop", nil)

	clientEntry(t, lin, "by-lin", nil)
	ok[shareOut](t, lin, "vault_share", map[string]any{"action": "publish", "slug": "by-lin"})
	clientEntry(t, ming, "hidden", map[string]any{"private": true})
	clientEntry(t, ming, "everywhere", nil)
	ok[shareOut](t, ming, "vault_share", map[string]any{"action": "publish", "slug": "everywhere"})
	ok[shareOut](t, ming, "vault_share", map[string]any{"action": "promote", "slug": "everywhere", "reason": "holds in every repo"})
	clientEntry(t, ming, "put-forward", nil)
	ok[shareOut](t, ming, "vault_share", map[string]any{"action": "publish", "slug": "put-forward"})
	ok[shareOut](t, ming, "vault_share", map[string]any{"action": "nominate", "slug": "put-forward", "reason": "useful"})
	clientEntry(t, ming, "plain", nil)

	_, slugs := clientRemind(t, ming)
	if !slices.Equal(slugs, []string{"plain"}) {
		t.Fatalf("entries = %v, want only plain", slugs)
	}
}
