package cli

import (
	"encoding/json"
	"strings"
	"testing"
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
