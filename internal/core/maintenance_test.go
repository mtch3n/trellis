package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPruneRevisionsTrimsKnowledgeAndCards(t *testing.T) {
	c, p, b := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Log", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	version := doc.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditKnowledge(t.Context(), p.ID, doc.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditKnowledge v%d: %v", i, err)
		}
		version = edited.Version
	}
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Ship"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	cardVersion := card.Version
	for i := 2; i <= 4; i++ {
		body := fmt.Sprintf("v%d", i)
		edited, err := c.EditCard(t.Context(), p.ID, CardRef{UUID: card.ID}, CardEdit{Body: &body, IfVersion: &cardVersion})
		if err != nil {
			t.Fatalf("EditCard v%d: %v", i, err)
		}
		cardVersion = edited.Version
	}

	c.historyKeep = 2
	n, err := c.PruneRevisions(t.Context())
	if err != nil {
		t.Fatalf("PruneRevisions: %v", err)
	}
	if n == 0 {
		t.Fatal("PruneRevisions removed nothing")
	}
	entries, err := os.ReadDir(revisionDir(doc.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("%d knowledge revisions after pruning to keep=2, want 2", len(entries))
	}
	var cardRevs int
	if err := c.db.Get(&cardRevs, `SELECT COUNT(*) FROM card_revision WHERE card_id = ?`, card.ID); err != nil {
		t.Fatal(err)
	}
	if cardRevs != 2 {
		t.Errorf("%d card revisions after pruning to keep=2, want 2", cardRevs)
	}
}

func TestPruneOrphanHistoryRemovesADirectoryNoEntryAccountsFor(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Kept", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	// What removing a file and its row outside Trellis leaves behind.
	stray := filepath.Join(filepath.Dir(doc.Path), ".removed-by-hand.md")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "1.md"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("the stray revision directory still exists")
	}
	if _, err := os.Stat(revisionDir(doc.Path)); err != nil {
		t.Errorf("the live entry's revisions were removed too: %v", err)
	}
}

// An entry whose file was deleted by hand still has its row, and its history
// holds the only copy of the content left. Pruning must not take it.
func TestPruneOrphanHistoryKeepsTheHistoryOfAnEntryWhoseFileIsGone(t *testing.T) {
	c, p, _ := kbCore(t)
	doc, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Gone", Body: "the only copy\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	if err := os.Remove(doc.Path); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 0 {
		t.Fatalf("removed %d, want 0: the row still registers this entry", n)
	}
	raw, err := os.ReadFile(revisionFilePath(doc.Path, 1))
	if err != nil {
		t.Fatalf("version 1 must survive: %v", err)
	}
	if !strings.Contains(string(raw), "the only copy") {
		t.Errorf("revision 1 = %q", raw)
	}
}

func TestHealthReportsRevisionsAndOrphans(t *testing.T) {
	c, p, _ := kbCore(t)
	doc1, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Watched", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateKnowledge: %v", err)
	}
	// Create and edit to get multiple revisions
	if _, err := c.EditKnowledge(t.Context(), p.ID, doc1.Slug, "v2\n", &doc1.Version); err != nil {
		t.Fatalf("EditKnowledge: %v", err)
	}

	// A revision directory no row accounts for.
	stray := filepath.Join(filepath.Dir(doc1.Path), ".orphan.md")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}

	revisions, orphaned, err := c.RevisionHealth(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("RevisionHealth: %v", err)
	}
	if revisions != 2 {
		t.Errorf("revisions = %d, want 2 (one from Watched with 2 versions)", revisions)
	}
	if orphaned != 1 {
		t.Errorf("orphaned = %d, want 1", orphaned)
	}
}
