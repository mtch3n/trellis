package mcpserver_test

import (
	"slices"
	"testing"

	"github.com/mtch3n/trellis/internal/platform"
)

type clientListOut struct {
	Entries []platform.Entry `json:"entries"`
}

func TestClientVaultListUnverifiedListsOnlyGlobalEntriesPastVerifyBy(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	for _, slug := range []string{"stale", "fresh"} {
		clientEntry(t, cs, slug, nil)
		ok[shareOut](t, cs, "vault_share", map[string]any{"action": "publish", "slug": slug})
		ok[shareOut](t, cs, "vault_share", map[string]any{"action": "promote", "slug": slug, "reason": "holds everywhere"})
	}
	clientEntry(t, cs, "local", nil)
	if _, err := e.db.Exec(`UPDATE entry SET verify_by = now() - interval '1 day' WHERE slug = 'stale'`); err != nil {
		t.Fatal(err)
	}

	var slugs []string
	for _, entry := range ok[clientListOut](t, cs, "vault_list", map[string]any{"unverified": true}).Entries {
		slugs = append(slugs, entry.Slug)
	}
	if !slices.Equal(slugs, []string{"stale"}) {
		t.Fatalf("unverified = %v, want only stale", slugs)
	}
	if all := ok[clientListOut](t, cs, "vault_list", map[string]any{}).Entries; len(all) != 3 {
		t.Fatalf("vault_list without unverified lists %d entries, want 3", len(all))
	}
}
