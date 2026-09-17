package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPruneRevisionsTrimsEntriesAndCards(t *testing.T) {
	c, p, b := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Log", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	version := entry.Version
	for i := 2; i <= 4; i++ {
		edited, err := c.EditEntry(t.Context(), p.ID, entry.Slug, fmt.Sprintf("v%d\n", i), &version)
		if err != nil {
			t.Fatalf("EditEntry v%d: %v", i, err)
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
	entries, err := os.ReadDir(revisionDir(entry.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("%d entry revisions after pruning to keep=2, want 2", len(entries))
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
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Kept", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	// What removing a file and its row outside Trellis leaves behind.
	stray := filepath.Join(filepath.Dir(entry.Path), ".removed-by-hand.md")
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
	if _, err := os.Stat(revisionDir(entry.Path)); err != nil {
		t.Errorf("the live entry's revisions were removed too: %v", err)
	}
}

// An entry whose file was deleted by hand still has its row, and its history
// holds the only copy of the content left. Pruning must not take it.
func TestPruneOrphanHistoryKeepsTheHistoryOfAnEntryWhoseFileIsGone(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Gone", Body: "the only copy\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if err := os.Remove(entry.Path); err != nil {
		t.Fatal(err)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 0 {
		t.Fatalf("removed %d, want 0: the row still registers this entry", n)
	}
	raw, err := os.ReadFile(revisionFilePath(entry.Path, 1))
	if err != nil {
		t.Fatalf("version 1 must survive: %v", err)
	}
	if !strings.Contains(string(raw), "the only copy") {
		t.Errorf("revision 1 = %q", raw)
	}
}

// A vault directory is not exclusively Trellis's: the user may open it in
// Obsidian (.obsidian/) or version it with git (.git/). Both are directories
// whose name starts with ".", exactly like a revision directory, but neither
// is one, and --orphan-history must never touch either -- review-knowledge #1.
func TestPruneOrphanHistoryLeavesGitAndObsidianAlone(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Kept", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	vault := filepath.Dir(entry.Path)

	gitDir := filepath.Join(vault, ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(gitFile, []byte("ref: refs/heads/main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	obsidianDir := filepath.Join(vault, ".obsidian")
	if err := os.MkdirAll(obsidianDir, 0o700); err != nil {
		t.Fatal(err)
	}
	obsidianFile := filepath.Join(obsidianDir, "workspace.json")
	if err := os.WriteFile(obsidianFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, orphaned, err := c.RevisionHealth(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("RevisionHealth: %v", err)
	}
	if orphaned != 0 {
		t.Errorf("orphaned = %d, want 0: .git and .obsidian are not revision directories", orphaned)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 0 {
		t.Fatalf("removed %d, want 0", n)
	}
	if _, err := os.Stat(gitFile); err != nil {
		t.Errorf(".git/HEAD must survive: %v", err)
	}
	if _, err := os.Stat(obsidianFile); err != nil {
		t.Errorf(".obsidian/workspace.json must survive: %v", err)
	}
	if _, err := os.Stat(revisionDir(entry.Path)); err != nil {
		t.Errorf("the live entry's own revisions were removed too: %v", err)
	}
}

// review-knowledge #11: a project filter must not make another project's
// promoted entry, sitting in the global vault everyone shares, look
// orphaned; and a nested directory must not be walked -- and its orphan
// reported -- twice.
func TestHealthAndPruneDoNotOverCount(t *testing.T) {
	c, p1, _ := vaultCore(t)
	p2 := seededProject2(t, c)

	// p2 promotes an entry into the shared global vault.
	entry, err := c.CreateEntry(t.Context(), p2.ID, NewEntry{Title: "Shared", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	promoted, err := c.PromoteEntry(t.Context(), p2.ID, entry.Slug, "shared runbook")
	if err != nil {
		t.Fatalf("PromoteEntry: %v", err)
	}

	// p1 has a nested entry, so its directory is both walked directly (it is
	// a vault by virtue of holding an entry) and as part of recursing its
	// parent vault.
	nested, err := c.CreateEntry(t.Context(), p1.ID, NewEntry{Title: "Rollback", Body: "v1\n", Dir: "deployment"})
	if err != nil {
		t.Fatalf("CreateEntry nested: %v", err)
	}
	stray := filepath.Join(filepath.Dir(nested.Path), ".orphan.md")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}

	// Scoped to p1: the global vault's only entry belongs to p2, and must
	// not be reported as p1's orphan just because p1's filter excluded it
	// from "live".
	_, orphaned, err := c.RevisionHealth(t.Context(), p1.ID)
	if err != nil {
		t.Fatalf("RevisionHealth: %v", err)
	}
	if orphaned != 1 {
		t.Errorf("orphaned = %d, want 1 (the one real orphan under deployment/, counted once)", orphaned)
	}

	n, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1: the nested vault must not be walked twice", n)
	}
	if _, err := os.Stat(revisionDir(promoted.Path)); err != nil {
		t.Errorf("p2's promoted entry's own revisions were removed too: %v", err)
	}
}

// OrphanHistoryCount is the small exported wrapper the settings API's
// maintenance stats route uses; it must agree with what PruneOrphanHistory
// would actually remove.
func TestOrphanHistoryCountMatchesWhatPruneRemoves(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Kept", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if n, err := c.OrphanHistoryCount(t.Context()); err != nil || n != 0 {
		t.Fatalf("OrphanHistoryCount = %d, err = %v, want 0 before any stray directory exists", n, err)
	}

	stray := filepath.Join(filepath.Dir(entry.Path), ".removed-by-hand.md")
	if err := os.MkdirAll(stray, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stray, "1.md"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := c.OrphanHistoryCount(t.Context())
	if err != nil {
		t.Fatalf("OrphanHistoryCount: %v", err)
	}
	if n != 1 {
		t.Fatalf("OrphanHistoryCount = %d, want 1", n)
	}

	removed, err := c.PruneOrphanHistory(t.Context())
	if err != nil {
		t.Fatalf("PruneOrphanHistory: %v", err)
	}
	if int(removed) != n {
		t.Fatalf("PruneOrphanHistory removed %d, OrphanHistoryCount reported %d", removed, n)
	}
	if n, err := c.OrphanHistoryCount(t.Context()); err != nil || n != 0 {
		t.Fatalf("OrphanHistoryCount after pruning = %d, err = %v, want 0", n, err)
	}
}

// ParseRetention is shared by `maintenance prune --before` and the settings
// API's prune route, so "90d" means the same age from either caller.
func TestParseRetentionAcceptsDaysWeeksAndGoDurations(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
		want    time.Duration
	}{
		{"90d", false, 90 * 24 * time.Hour},
		{"12w", false, 12 * 7 * 24 * time.Hour},
		{"36h", false, 36 * time.Hour},
		{"", true, 0},
		{"0d", true, 0},
		{"-5d", true, 0},
		{"banana", true, 0},
	}
	for _, tc := range cases {
		got, err := ParseRetention(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRetention(%q): want an error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRetention(%q): %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseRetention(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestHealthReportsRevisionsAndOrphans(t *testing.T) {
	c, p, _ := vaultCore(t)
	entry1, err := c.CreateEntry(t.Context(), p.ID, NewEntry{Title: "Watched", Body: "v1\n"})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	// Create and edit to get multiple revisions
	if _, err := c.EditEntry(t.Context(), p.ID, entry1.Slug, "v2\n", &entry1.Version); err != nil {
		t.Fatalf("EditEntry: %v", err)
	}

	// A revision directory no row accounts for.
	stray := filepath.Join(filepath.Dir(entry1.Path), ".orphan.md")
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
