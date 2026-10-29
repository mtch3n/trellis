package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/platform"
)

type entryOut struct {
	Entry *platform.Entry `json:"entry"`
}

type shareOut struct {
	Result *platform.ShareResult `json:"result"`
	Pin    *platform.Pin         `json:"pin"`
}

func TestVault_VAULT_C65_ToolListHoldsTheSeventeenToolsWithSchemas(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	tools := listTools(t, cs)
	want := slices.Sorted(slices.Values([]string{
		"project_list", "board_list", "board_write", "board_brief", "card_list", "card_show", "card_write", "card_claim",
		"vault_list", "vault_search", "vault_show", "vault_write", "vault_share", "vault_check", "recall", "artifact", "template",
	}))
	if got := names(tools); !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for _, tl := range tools {
		if tl.InputSchema == nil || tl.OutputSchema == nil {
			t.Errorf("%s: input schema %v, output schema %v", tl.Name, tl.InputSchema != nil, tl.OutputSchema != nil)
		}
	}
}

func TestVault_VAULT_C66_PromoteLogsArgumentNamesAndNoWordOfTheReason(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	ok[entryOut](t, cs, "vault_write", map[string]any{"action": "new", "slug": "deploys", "title": "Deploys", "body": "Deploy from main only."})
	ok[shareOut](t, cs, "vault_share", map[string]any{"action": "publish", "slug": "deploys"})
	got := ok[shareOut](t, cs, "vault_share", map[string]any{"action": "promote", "slug": "deploys", "reason": "zebrafish lantern quokka"})
	if got.Result == nil || got.Result.Scope != "global" {
		t.Fatalf("promote = %+v", got)
	}

	rows := e.invocations()
	i := slices.IndexFunc(rows, func(r invocationRow) bool { return r.Tool == "vault_share" && r.Action == "promote" })
	if i < 0 {
		t.Fatalf("no promote row in %+v", rows)
	}
	if r := rows[i]; r.ArgNames != "action,reason,slug" || r.Code != "0" {
		t.Fatalf("promote row = %+v", r)
	}
	for _, r := range rows {
		for _, word := range []string{"zebrafish", "lantern", "quokka"} {
			if strings.Contains(r.Row, word) {
				t.Errorf("row %q holds %q", r.Row, word)
			}
		}
	}
}

func TestVault_VAULT_C67_UnknownActionNamesTheToolsActionsAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	tables := []string{"entry", "entry_revision", "entry_link", "nomination", "pin", "artifact", "artifact_link", "vault_template", "event"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = e.count(table)
	}

	for _, c := range []struct {
		tool    string
		field   string
		actions []string
	}{
		{"vault_write", "title", []string{"new", "edit", "move", "delete", "restore", "link", "unlink"}},
		{"vault_share", "slug", []string{"publish", "demote", "nominate", "promote", "verify", "pin", "unpin"}},
		{"vault_check", "slug", []string{"lint", "health", "history", "diff", "nominations"}},
		{"artifact", "name", []string{"add", "show", "list", "share", "link", "delete", "restore"}},
		{"template", "name", []string{"list", "show", "new", "edit"}},
	} {
		tool, actions := c.tool, c.actions
		got := fails(t, cs, tool, map[string]any{"action": "teleport", c.field: "x"})
		if got.Code != "bad_action" || !strings.Contains(got.Message, "teleport") {
			t.Errorf("%s: %+v, want bad_action naming teleport", tool, got)
		}
		for _, a := range actions {
			if !strings.Contains(got.Message, a) {
				t.Errorf("%s: message %q does not name the action %s", tool, got.Message, a)
			}
		}
	}
	for _, table := range tables {
		if n := e.count(table); n != before[table] {
			t.Errorf("%s: %d rows, was %d", table, n, before[table])
		}
	}
}

func TestVaultTools_AgentDrivesEveryAction(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", nil)
	card := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "ship"}).Card

	x := ok[entryOut](t, cs, "vault_write", map[string]any{"action": "new", "slug": "notes", "title": "Notes", "body": "kubernetes ingress tips", "tags": []string{"k8s"}}).Entry
	x = ok[entryOut](t, cs, "vault_write", map[string]any{"action": "edit", "slug": "notes", "body": "kubernetes ingress timeouts", "if_version": x.Version}).Entry
	x = ok[entryOut](t, cs, "vault_write", map[string]any{"action": "move", "slug": "notes", "to": "ingress", "if_version": x.Version}).Entry
	ok[entryOut](t, cs, "vault_write", map[string]any{"action": "link", "slug": "ingress", "card": card.Ref})
	ok[entryOut](t, cs, "vault_write", map[string]any{"action": "unlink", "slug": "ingress", "card": card.Ref})
	ok[entryOut](t, cs, "vault_write", map[string]any{"action": "delete", "slug": "ingress"})
	ok[entryOut](t, cs, "vault_write", map[string]any{"action": "restore", "slug": "ingress"})
	if x.Slug != "ingress" || x.Version != 3 {
		t.Fatalf("entry = %+v", x)
	}

	if got := ok[vaultListOut](t, cs, "vault_list", nil); len(got.Entries) != 1 {
		t.Fatalf("list = %+v", got)
	}
	if got := ok[struct {
		Hits []platform.EntryHit `json:"hits"`
	}](t, cs, "vault_search", map[string]any{"query": "ingress"}); len(got.Hits) != 1 {
		t.Fatalf("search = %+v", got)
	}
	if got := ok[platform.Entry](t, cs, "vault_show", map[string]any{"slug": "ingress"}); got.Body != "kubernetes ingress timeouts" {
		t.Fatalf("show = %+v", got)
	}

	share := ok[shareOut](t, cs, "vault_share", map[string]any{"action": "publish", "slug": "ingress"})
	if share.Result == nil || share.Result.Scope != platform.ScopeProject {
		t.Fatalf("publish = %+v", share)
	}
	ok[shareOut](t, cs, "vault_share", map[string]any{"action": "nominate", "slug": "ingress", "reason": "useful"})
	if pin := ok[shareOut](t, cs, "vault_share", map[string]any{"action": "pin", "slug": "ingress", "recap": "read before ingress work"}).Pin; pin == nil || pin.Recap == "" {
		t.Fatal("pin returned no pin")
	}
	ok[shareOut](t, cs, "vault_share", map[string]any{"action": "unpin", "slug": "ingress"})

	type checkOut struct {
		Diagnostics []platform.Diagnostic  `json:"diagnostics"`
		Health      []platform.HealthLine  `json:"health"`
		Revisions   []platform.Revision    `json:"revisions"`
		Diff        *platform.RevisionDiff `json:"diff"`
		Nominations []platform.Nomination  `json:"nominations"`
	}
	ok[checkOut](t, cs, "vault_check", map[string]any{"action": "lint"})
	if got := ok[checkOut](t, cs, "vault_check", map[string]any{"action": "health"}); len(got.Health) == 0 {
		t.Fatal("health is empty")
	}
	if got := ok[checkOut](t, cs, "vault_check", map[string]any{"action": "history", "slug": "ingress"}); len(got.Revisions) != 4 {
		t.Fatalf("history = %+v", got.Revisions)
	}
	if got := ok[checkOut](t, cs, "vault_check", map[string]any{"action": "diff", "slug": "ingress", "from": 1, "to": 2}); got.Diff == nil || !strings.Contains(got.Diff.Diff, "+kubernetes ingress timeouts") {
		t.Fatalf("diff = %+v", got.Diff)
	}
	if got := ok[checkOut](t, cs, "vault_check", map[string]any{"action": "nominations"}); len(got.Nominations) != 1 {
		t.Fatalf("nominations = %+v", got.Nominations)
	}
	if got := ok[struct {
		Hits []platform.RecallHit `json:"hits"`
	}](t, cs, "recall", map[string]any{"text": "why do ingress timeouts happen?"}); len(got.Hits) != 1 {
		t.Fatalf("recall = %+v", got)
	}

	type artifactOut struct {
		Artifact  *platform.Artifact  `json:"artifact"`
		Artifacts []platform.Artifact `json:"artifacts"`
	}
	a := ok[artifactOut](t, cs, "artifact", map[string]any{"action": "add", "name": "log.txt", "content": []byte("hello")}).Artifact
	ok[artifactOut](t, cs, "artifact", map[string]any{"action": "link", "ref": a.Name, "card": card.Ref})
	if got := ok[artifactOut](t, cs, "artifact", map[string]any{"action": "show", "ref": a.Name}).Artifact; string(got.Content) != "hello" {
		t.Fatalf("show = %+v", got)
	}
	ok[artifactOut](t, cs, "artifact", map[string]any{"action": "share", "ref": a.Name})
	if got := ok[artifactOut](t, cs, "artifact", map[string]any{"action": "list"}); len(got.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v", got)
	}
	ok[artifactOut](t, cs, "artifact", map[string]any{"action": "delete", "ref": a.Name})
	ok[artifactOut](t, cs, "artifact", map[string]any{"action": "restore", "ref": a.Name})

	type templateOut struct {
		Template  *platform.Template  `json:"template"`
		Templates []platform.Template `json:"templates"`
	}
	list := ok[templateOut](t, cs, "template", map[string]any{"action": "list"}).Templates
	if len(list) == 0 {
		t.Fatal("no templates")
	}
	shown := ok[templateOut](t, cs, "template", map[string]any{"action": "show", "name": list[0].Name}).Template
	raw := "---\nrequired: [summary]\n---\n# {{title}}\n"
	made := ok[templateOut](t, cs, "template", map[string]any{"action": "new", "name": "memo", "raw": raw}).Template
	ok[templateOut](t, cs, "template", map[string]any{"action": "edit", "name": "memo", "raw": raw + "\nmore\n", "if_version": made.Version})
	if shown == nil || made == nil {
		t.Fatal("template show or new returned nothing")
	}
}

type vaultListOut struct {
	Entries []platform.Entry `json:"entries"`
}
