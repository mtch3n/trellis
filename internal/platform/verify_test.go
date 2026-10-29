package platform

import (
	"strings"
	"testing"
)

func TestVault_VAULT_C50_VerifyingProjectEntryIsUsageError(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	id := w.gateSeedEntry(a, gateSeed{slug: "local", body: "Local rule.", scope: "project"})
	before := w.entryRow(id)

	_, err := w.p.Verify(t.Context(), a, "APP", "local")
	e := wantCode(t, err, "not_global")
	if e.Kind != KindUsage || !strings.Contains(e.Msg, "only a global entry") {
		t.Fatalf("error = %+v", e)
	}
	if w.entryRow(id) != before {
		t.Fatal("entry changed")
	}
}
