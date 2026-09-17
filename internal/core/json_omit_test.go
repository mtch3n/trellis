package core

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

// encoding/json/v2's omitempty never drops false or 0; these flags are meant
// to be absent unless set, so they use omitzero.
func TestEntryFlagsAreOmittedWhenFalse(t *testing.T) {
	plain, err := json.Marshal(Entry{Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"global"`, `"private"`} {
		if strings.Contains(string(plain), key) {
			t.Errorf("%s present in %s", key, plain)
		}
	}
	set, err := json.Marshal(Entry{Slug: "a", Global: true, Private: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"global":true`, `"private":true`} {
		if !strings.Contains(string(set), want) {
			t.Errorf("%s missing from %s", want, set)
		}
	}
}
