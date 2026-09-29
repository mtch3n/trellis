package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// newCardRef creates a card and returns its ref.
func newCardRef(t *testing.T, title string) string {
	t.Helper()
	var card struct {
		Ref string `json:"ref"`
	}
	out := runCmd(t, "card", "new", "--title", title, "--json")
	if err := json.Unmarshal([]byte(out), &card); err != nil || card.Ref == "" {
		t.Fatalf("card new: %q, %v", out, err)
	}
	return card.Ref
}

type listedCards struct {
	Cards []struct {
		Ref       string `json:"ref"`
		TrashedAt *int64 `json:"trashed_at"`
	} `json:"cards"`
}

func listCards(t *testing.T, args ...string) listedCards {
	t.Helper()
	var page listedCards
	out := runCmd(t, append([]string{"card", "ls", "--json"}, args...)...)
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("card ls: %q, %v", out, err)
	}
	return page
}

func TestTrash_TRASH_C1_card_rm_trashes_and_ls_trashed_shows_it(t *testing.T) {
	projectEnv(t)
	ref := newCardRef(t, "mistake")

	runCmd(t, "card", "rm", ref)

	for _, c := range listCards(t).Cards {
		if c.Ref == ref {
			t.Fatalf("card ls still lists %s", ref)
		}
	}
	var trashed bool
	for _, c := range listCards(t, "--trashed").Cards {
		if c.Ref == ref {
			trashed = c.TrashedAt != nil && *c.TrashedAt > 0
		}
	}
	if !trashed {
		t.Fatalf("card ls --trashed does not list %s with trashed_at", ref)
	}

	runCmd(t, "card", "restore", ref)
	out := runCmd(t, "card", "show", ref, "--json")
	if !strings.Contains(out, `"title":"mistake"`) || strings.Contains(out, "trashed_at") {
		t.Errorf("restored card: %s", out)
	}
}

func TestTrash_TRASH_C9_a_trashed_pinned_entry_leaves_search_recall_and_the_brief(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Zanzibar rollout", "--body", "zanzibar steps\n")
	runCmd(t, "vault", "pin", "zanzibar-rollout")
	runCmd(t, "vault", "rm", "zanzibar-rollout")

	for _, args := range [][]string{
		{"search", "zanzibar", "--json"},
		{"recall", "zanzibar rollout", "--json"},
		{"vault", "pins", "--json"},
	} {
		if out := runCmd(t, args...); strings.Contains(out, "zanzibar-rollout") {
			t.Errorf("%v still finds the trashed entry: %s", args, out)
		}
	}
	c, db, err := openCore()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := c.ProjectByKey(t.Context(), "TEST")
	if err != nil {
		t.Fatal(err)
	}
	indexed, err := c.ListSearchEntries(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range indexed {
		if e.Slug == "zanzibar-rollout" {
			t.Errorf("the vector index still holds the trashed entry")
		}
	}

	out := runCmd(t, "search", "zanzibar", "--trashed", "--json")
	if !strings.Contains(out, "zanzibar-rollout") || !strings.Contains(out, "trashed_at") {
		t.Errorf("search --trashed: %s", out)
	}
}

// trashedMarks maps each listed name to whether it carries trashed_at.
func trashedMarks(t *testing.T, out, list, name string) map[string]bool {
	t.Helper()
	var listing map[string]json.RawMessage
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	if err := json.Unmarshal(listing[list], &items); err != nil {
		t.Fatalf("%q: %v", out, err)
	}
	marks := map[string]bool{}
	for _, item := range items {
		_, trashed := item["trashed_at"]
		marks[item[name].(string)] = trashed
	}
	return marks
}

func TestTrash_TRASH_C7_ls_hides_the_trash_and_trashed_shows_it(t *testing.T) {
	projectEnv(t)
	live, gone := newCardRef(t, "live card"), newCardRef(t, "gone card")
	runCmd(t, "card", "rm", gone)
	runCmd(t, "vault", "new", "--title", "Live entry")
	runCmd(t, "vault", "new", "--title", "Gone entry")
	runCmd(t, "vault", "rm", "gone-entry")
	runCmd(t, "artifact", "add", writeFile(t, "live.txt", "live"))
	runCmd(t, "artifact", "add", writeFile(t, "gone.txt", "gone"))
	runCmd(t, "artifact", "rm", "gone.txt")
	runCmd(t, "board", "new", "--name", "live-board")
	runCmd(t, "board", "new", "--name", "gone-board")
	runCmd(t, "board", "rm", "gone-board")
	seedProject(t, "LIVE")
	seedProject(t, "GONE")
	runCmd(t, "project", "rm", "GONE")

	for _, c := range []struct {
		args             []string
		list, name       string
		liveKey, goneKey string
	}{
		{[]string{"card", "ls"}, "cards", "ref", live, gone},
		{[]string{"vault", "ls"}, "entries", "slug", "live-entry", "gone-entry"},
		{[]string{"artifact", "ls"}, "artifacts", "name", "live.txt", "gone.txt"},
		{[]string{"board", "ls"}, "boards", "name", "live-board", "gone-board"},
		{[]string{"project", "ls"}, "projects", "key", "LIVE", "GONE"},
	} {
		plain := trashedMarks(t, runCmd(t, append(c.args, "--json")...), c.list, c.name)
		if _, ok := plain[c.goneKey]; ok || plain[c.liveKey] {
			t.Errorf("%v: %v; want only %s, unmarked", c.args, plain, c.liveKey)
		}
		all := trashedMarks(t, runCmd(t, append(c.args, "--json", "--trashed")...), c.list, c.name)
		if trashed, ok := all[c.goneKey]; !ok || !trashed || all[c.liveKey] {
			t.Errorf("%v --trashed: %v; want %s marked and %s not", c.args, all, c.goneKey, c.liveKey)
		}
	}
}

func TestTrash_TRASH_C8_show_needs_trashed_to_see_the_trash(t *testing.T) {
	projectEnv(t)
	ref := newCardRef(t, "gone card")
	runCmd(t, "card", "rm", ref)
	runCmd(t, "vault", "new", "--title", "Gone entry", "--body", "the body\n")
	runCmd(t, "vault", "rm", "gone-entry")

	for _, args := range [][]string{{"card", "show", ref}, {"vault", "show", "gone-entry"}} {
		if _, err := runCmdErr(t, append(args, "--json")...); coreErrExit(err) != 3 {
			t.Errorf("%v: %v, want exit 3", args, err)
		}
		out := runCmd(t, append(args, "--json", "--trashed")...)
		if !strings.Contains(out, "trashed_at") {
			t.Errorf("%v --trashed: %s", args, out)
		}
	}
	if out := runCmd(t, "vault", "show", "gone-entry", "--trashed", "--json"); !strings.Contains(out, "the body") {
		t.Errorf("a trashed entry shows without its body: %s", out)
	}
}

func coreErrExit(err error) int {
	if e, ok := errors.AsType[*core.Error](err); ok {
		return e.Exit
	}
	return 0
}
