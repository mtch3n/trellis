package cli

import (
	"encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// TestJSONOutputUsesTheGlossary pins what each command prints under --json
// (spec §5, "JSON"). The vocabulary test keeps the retired names out of the
// code that prints them.
func TestJSONOutputUsesTheGlossary(t *testing.T) {
	projectEnv(t)
	t.Setenv("TRELLIS_AGENT", "agent:wire")
	var card struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(runCmd(t, "card", "new", "--title", "Wire card", "--json")), &card); err != nil {
		t.Fatal(err)
	}
	runCmd(t, "card", "claim", card.Ref)
	runCmd(t, "vault", "new", "--title", "Wire entry", "--body", "wire format notes\n")
	runCmd(t, "vault", "nominate", "wire-entry", "--reason", "every client parses it")
	runCmd(t, "artifact", "add", writeFile(t, "wire.png", "\x89PNG\r\n\x1a\nx"))
	promotedPastVerify(t, "Stale wire entry")

	for _, tc := range []struct {
		args []string
		want []string
		// retired is spelled out only where the vocabulary test cannot see
		// it: these two words break none of its rules.
		retired []string
	}{
		{[]string{"card", "show", card.Ref}, []string{`"claimed_by":"agent:wire"`, `"claim_until":`}, nil},
		{[]string{"agent", "remind"}, []string{`"claimed_without_comment":`}, nil},
		{[]string{"vault", "lint"}, []string{`"diagnostics":`, `"entry":"/TEST/vault/wire-entry"`}, nil},
		{[]string{"vault", "health", "--duplicates"}, []string{`"duplicate_clusters":`}, []string{`"clusters"`}},
		{[]string{"template", "check", "decision", "wire-entry"}, []string{`"diagnostics":`}, []string{`"violations"`}},
		{[]string{"vault", "nominations"}, []string{`"nominations":1`}, nil},
		{[]string{"vault", "show", "/GLOBAL/vault/stale-wire-entry"}, []string{`"unverified":true`}, nil},
		{[]string{"vector", "status"}, []string{`"configured_entries":0`, `"indexed_entries":0`, `"unindexed_entries":0`}, nil},
		{[]string{"search", "wire"}, []string{`"unverified":true`}, nil},
		{[]string{"graph", "wire-entry"}, []string{`"type":"entry"`}, nil},
		{[]string{"artifact", "link", "wire.png", "--entry", "wire-entry"}, []string{`"entry":"wire-entry"`}, nil},
		{[]string{"artifact", "unlink", "wire.png", "--entry", "wire-entry"}, []string{`"entry":"wire-entry"`}, nil},
	} {
		out := runCmd(t, append(tc.args, "--json")...)
		for _, want := range tc.want {
			if !strings.Contains(out, want) {
				t.Errorf("%v: no %s in\n%s", tc.args, want, out)
			}
		}
		for _, retired := range tc.retired {
			if strings.Contains(out, retired) {
				t.Errorf("%v: retired %s in\n%s", tc.args, retired, out)
			}
		}
	}

	// A hit's kind is the event log's entity: card or entry, and nothing else.
	for _, args := range [][]string{{"search", "wire"}, {"recall", "wire format"}} {
		out := runCmd(t, append(args, "--json")...)
		var hits struct {
			Results []struct {
				Kind string `json:"kind"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(out), &hits); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		kinds := map[string]int{}
		for _, h := range hits.Results {
			kinds[h.Kind]++
		}
		if kinds["card"] != 1 || kinds["entry"] != 2 || len(kinds) != 2 {
			t.Errorf("%v: kinds = %v, want one card and two entries\n%s", args, kinds, out)
		}
	}
}

// promotedPastVerify creates a global entry whose verify_by has passed. The
// CLI's promote needs a human at a terminal, so the entry is made in core.
func promotedPastVerify(t *testing.T, title string) {
	t.Helper()
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := core.New(db, core.RealClock{}, "human:test", root)
	p, err := c.ProjectByKey(t.Context(), "TEST")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := c.CreateEntry(t.Context(), p.ID, core.NewEntry{Title: title, Body: "wire format, promoted\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromoteEntry(t.Context(), p.ID, entry.Slug, "shared"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE entry SET verify_by = 1 WHERE id = ?`, entry.ID); err != nil {
		t.Fatal(err)
	}
}
