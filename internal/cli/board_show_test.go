package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

func TestFormatBriefShowsUnownedCards(t *testing.T) {
	// TRELLIS-1: brief must show open cards left by earlier sessions (unowned),
	// most recently updated first, each with its latest note.
	brief := &boardBrief{
		yours: []cardInfo{},
		others: []cardInfo{
			{Ref: "P-2", Title: "Second", Comment: "Latest note"},
			{Ref: "P-1", Title: "First", Comment: "Some note"},
		},
		counts: map[string]int{"backlog": 2},
		pins:   []core.Pin{},
	}

	text := formatBrief(brief)
	if text == "" {
		t.Fatal("brief should not be empty")
	}

	// Should have unowned cards section
	if !strings.Contains(text, "### others") {
		t.Error("brief should show 'others' section with unowned cards")
	}

	// Should have unowned cards
	if !strings.Contains(text, "P-2") {
		t.Error("brief should show unowned card P-2")
	}
	if !strings.Contains(text, "P-1") {
		t.Error("brief should show unowned card P-1")
	}

	// Should show the count
	if !strings.Contains(text, "backlog") {
		t.Error("brief should show counts")
	}
}

func TestFormatBriefCollapsesEmptyBoard(t *testing.T) {
	// TRELLIS-2: on a board with nothing to show, the brief collapses to one line.
	brief := &boardBrief{
		yours:  []cardInfo{},
		others: []cardInfo{},
		counts: map[string]int{},
		pins:   []core.Pin{},
	}

	text := formatBrief(brief)

	// Should be very short (just one line)
	lines := strings.Count(text, "\n")
	if lines > 2 {
		t.Errorf("empty board brief should be short (~1 line), got %d lines:\n%s", lines, text)
	}

	// Should not show the full "do this" cheatsheet
	if strings.Contains(text, "card new --title") {
		t.Error("brief should not show full cheatsheet when board is empty")
	}
}

// Cards untouched for a week are counted, not listed, and alone do not bring
// the command cheatsheet: most of a board's open cards sit like that for weeks.
func TestFormatBriefCountsStaleCardsWithoutListingThem(t *testing.T) {
	brief := &boardBrief{
		yours:  []cardInfo{},
		others: []cardInfo{},
		stale:  52,
		counts: map[string]int{"backlog": 52},
		pins:   []core.Pin{},
	}
	text := formatBrief(brief)
	if !strings.Contains(text, "+52 untouched for 7+ days: trellis card ls") {
		t.Errorf("brief should count stale cards:\n%s", text)
	}
	if strings.Contains(text, "card new --title") || strings.Contains(text, "board is empty") {
		t.Errorf("stale cards alone should bring neither the cheatsheet nor 'board is empty':\n%s", text)
	}
}

func TestBoardBriefListsRecentCardsAndCountsOldOnes(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "Fresh work")
	runCmd(t, "card", "new", "--title", "Forgotten work")

	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-briefRecent - time.Hour).UnixMilli()
	if _, err := db.Exec(`UPDATE card SET updated_at = ? WHERE title = 'Forgotten work'`, old); err != nil {
		t.Fatal(err)
	}
	db.Close()

	brief := runCmd(t, "board", "show", "--brief")
	if !strings.Contains(brief, "Fresh work") || strings.Contains(brief, "Forgotten work") {
		t.Errorf("brief should list the recent card and not the old one:\n%s", brief)
	}
	if !strings.Contains(brief, "+1 untouched for 7+ days") {
		t.Errorf("brief should count the old card:\n%s", brief)
	}
}
