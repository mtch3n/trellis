package core

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/mtch3n/trellis/internal/store"
)

func TestCreateCardDefaults(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "Fix D3cold regression"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	if card.Seq != 1 {
		t.Errorf("Seq = %d, want 1", card.Seq)
	}
	if card.Ref != "XPSCTL-1" {
		t.Errorf("Ref = %q, want XPSCTL-1", card.Ref)
	}
	if card.ColumnName != "backlog" {
		t.Errorf("ColumnName = %q, want backlog (the first column)", card.ColumnName)
	}
	if card.Priority != PriorityNormal {
		t.Errorf("Priority = %v, want normal", card.Priority)
	}
	if card.Version != 1 {
		t.Errorf("Version = %d, want 1", card.Version)
	}
}

func TestCreateCardRecordsEvent(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}

	var action string
	if err := c.db.Get(&action,
		`SELECT action FROM event WHERE entity_type='card' AND entity_id=?`, card.ID); err != nil {
		t.Fatalf("no event recorded for the new card: %v", err)
	}
	if action != "created" {
		t.Errorf("action = %q, want created", action)
	}
}

func TestCreateCardUnknownColumn(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	_, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "x", Column: "shipped"})
	if err == nil {
		t.Fatal("expected an error for an unknown column")
	}
}

// Sequence numbers must stay unique when several processes create cards at
// once. Separate processes are covered in P1; this covers the in-process race.
func TestConcurrentCreateAllocatesDistinctSeqs(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := New(db, FixedClock{MS: 1_757_000_000_000}, "test:1", dir)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	const n = 12
	var wg sync.WaitGroup
	seqs := make([]int64, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "concurrent"})
			seqs[idx], errs[idx] = card.Seq, err
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("CreateCard %d: %v", i, errs[i])
		}
		if seen[seqs[i]] {
			t.Fatalf("duplicate seq %d", seqs[i])
		}
		seen[seqs[i]] = true
	}
}

// NewCard with no Priority specified defaults to Normal.
func TestCreateCardNoPriorityDefaultsToNormal(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "default priority"})
	if err != nil {
		t.Fatal(err)
	}
	if card.Priority != PriorityNormal {
		t.Errorf("Priority = %v, want normal", card.Priority)
	}
}

// NewCard with explicit Urgent priority creates an urgent card.
func TestCreateCardExplicitUrgent(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)

	urgent := PriorityUrgent
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "urgent card", Priority: &urgent})
	if err != nil {
		t.Fatal(err)
	}
	if card.Priority != PriorityUrgent {
		t.Errorf("Priority = %v, want urgent", card.Priority)
	}
}
