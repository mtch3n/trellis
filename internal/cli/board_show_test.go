package cli

import (
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

func TestFormatBriefShowsUnownedCards(t *testing.T) {
	// TRELLIS-1: brief must show open cards left by earlier sessions (unowned),
	// most recently updated first, each with its latest note.
	brief := &boardBrief{
		yours: []cardInfo{},
		others: []cardInfo{
			{Ref: "P-2", Title: "Second", Note: "Latest note"},
			{Ref: "P-1", Title: "First", Note: "Some note"},
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
