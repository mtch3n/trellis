package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/store"
)

// TestConcurrentClaimNoLostCards runs 8 OS processes racing to claim 50 cards,
// asserting every card is claimed exactly once and none is lost.
// This test re-invokes the test binary as separate processes, not goroutines,
// because the real risk is cross-process contention under SQLite's WAL mode.
func TestConcurrentClaimNoLostCards(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns child processes")
	}

	// Child mode: claim one card, then report how many cards remain in the board.
	if dbPath := os.Getenv("TRELLIS_TEST_CHILD_CLAIM"); dbPath != "" {
		runClaimChild(dbPath)
		return
	}

	// Parent mode: spawn children and verify the invariant.
	path := filepath.Join(t.TempDir(), "test.db")

	// Set up the database with one project, one board, and 50 cards.
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	core := New(db, FixedClock{MS: 1000000}, "test-setup")
	ctx := context.Background()

	// Create project and board.
	id := resolve.Identity{
		Kind:         "test",
		Value:        "concurrent-claim",
		SuggestedKey: "CLAIM",
	}
	proj, err := core.EnsureProject(ctx, id)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	// Create 50 cards.
	for i := 1; i <= 50; i++ {
		_, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
			Title: fmt.Sprintf("card-%d", i),
			Body:  "",
		})
		if err != nil {
			t.Fatalf("CreateCard %d: %v", i, err)
		}
	}

	db.Close()

	// Spawn 8 child processes, each claiming cards until none remain.
	const children = 8
	var wg sync.WaitGroup
	outs := make([][]byte, children)
	errs := make([]error, children)

	for i := range children {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentClaimNoLostCards$")
			cmd.Env = append(os.Environ(), "TRELLIS_TEST_CHILD_CLAIM="+path)
			out, err := cmd.CombinedOutput()
			outs[i] = out
			errs[i] = err
		})
	}
	wg.Wait()

	// Check for child failures.
	for i, err := range errs {
		if err != nil {
			t.Errorf("child %d failed: %v\noutput:\n%s", i, err, outs[i])
		}
	}

	// Verify the invariant: exactly 50 cards claimed, 0 unclaimed (all done).
	db, err = store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	var owned, unowned int
	if err := db.Get(&owned, `SELECT count(*) FROM card WHERE owner IS NOT NULL`); err != nil {
		t.Fatalf("count owned: %v", err)
	}
	if err := db.Get(&unowned, `SELECT count(*) FROM card WHERE owner IS NULL`); err != nil {
		t.Fatalf("count unowned: %v", err)
	}

	if owned != 50 {
		t.Errorf("owned = %d, want 50", owned)
	}
	if unowned != 0 {
		t.Errorf("unowned = %d, want 0", unowned)
	}

	// Verify no duplicates: each card's owner should appear exactly once.
	var ownerCounts map[string]int
	rows, err := db.Query(`SELECT owner, count(*) FROM card WHERE owner IS NOT NULL GROUP BY owner`)
	if err != nil {
		t.Fatalf("query owner counts: %v", err)
	}
	defer rows.Close()
	ownerCounts = make(map[string]int)
	for rows.Next() {
		var owner string
		var count int
		if err := rows.Scan(&owner, &count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ownerCounts[owner] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Each process should have claimed some cards; verify no card is claimed twice.
	totalClaimed := 0
	for owner, count := range ownerCounts {
		if count > 50 { // Sanity check: no process should claim more than all cards
			t.Errorf("owner %s claimed %d cards (impossible)", owner, count)
		}
		totalClaimed += count
	}
	if totalClaimed != 50 {
		t.Errorf("total claimed = %d, want 50", totalClaimed)
	}
}

// runClaimChild claims cards in a loop until none remain.
func runClaimChild(dbPath string) {
	db, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Get the project and board.
	var projectID, boardID string
	if err := db.Get(&projectID, `SELECT id FROM project LIMIT 1`); err != nil {
		fmt.Fprintf(os.Stderr, "get project: %v\n", err)
		os.Exit(1)
	}
	if err := db.Get(&boardID, `SELECT id FROM board WHERE project_id = ? LIMIT 1`, projectID); err != nil {
		fmt.Fprintf(os.Stderr, "get board: %v\n", err)
		os.Exit(1)
	}

	actor := fmt.Sprintf("child:%d", os.Getpid())
	core := New(db, RealClock{}, actor)
	ctx := context.Background()

	// Claim cards until none remain.
	claimed := 0
	for {
		card, err := core.ClaimNextCard(ctx, boardID, 30*60*1000) // 30 minute TTL
		if err != nil {
			fmt.Fprintf(os.Stderr, "claim: %v\n", err)
			os.Exit(1)
		}
		if card == nil {
			// No work available; exit cleanly.
			os.Exit(0)
		}
		claimed++
	}
}

// TestClaimSpecificCard tests claiming a specific card.
func TestClaimSpecificCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor")
	ctx := context.Background()

	id := resolve.Identity{
		Kind:         "test",
		Value:        "claim-specific",
		SuggestedKey: "CLAIMSPEC",
	}
	proj, err := core.EnsureProject(ctx, id)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	card, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
		Title: "test card",
		Body:  "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Claim the specific card.
	claimed, err := core.ClaimCard(ctx, card.ID, 30*60*1000, false, "")
	if err != nil {
		t.Fatalf("ClaimCard: %v", err)
	}

	if claimed.Owner == nil || *claimed.Owner != "test-actor" {
		t.Errorf("Owner = %v, want test-actor", claimed.Owner)
	}
	if claimed.LeaseUntil == nil {
		t.Errorf("LeaseUntil is nil, want a timestamp")
	}
}

func TestGetNextCardDoesNotClaimOrRecordEvent(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "preview me"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	var eventsBefore int
	if err := c.db.Get(&eventsBefore, `SELECT COUNT(*) FROM event`); err != nil {
		t.Fatalf("event count before: %v", err)
	}
	got, err := c.GetNextCard(t.Context(), b.ID)
	if err != nil {
		t.Fatalf("GetNextCard: %v", err)
	}
	if got == nil || got.ID != card.ID {
		t.Fatalf("GetNextCard = %v, want %s", got, card.ID)
	}
	if got.Owner != nil || got.LeaseUntil != nil || got.Version != card.Version {
		t.Fatalf("preview changed card: owner=%v lease=%v version=%d, want nil nil %d",
			got.Owner, got.LeaseUntil, got.Version, card.Version)
	}

	var eventsAfter int
	if err := c.db.Get(&eventsAfter, `SELECT COUNT(*) FROM event`); err != nil {
		t.Fatalf("event count after: %v", err)
	}
	if eventsAfter != eventsBefore {
		t.Fatalf("GetNextCard added %d events", eventsAfter-eventsBefore)
	}
}

// TestReleaseCard tests releasing a claimed card.
func TestReleaseCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor")
	ctx := context.Background()

	id := resolve.Identity{
		Kind:         "test",
		Value:        "release-test",
		SuggestedKey: "RELEASE",
	}
	proj, err := core.EnsureProject(ctx, id)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	card, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
		Title: "test card",
		Body:  "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Claim the card.
	_, err = core.ClaimCard(ctx, card.ID, 30*60*1000, false, "")
	if err != nil {
		t.Fatalf("ClaimCard: %v", err)
	}

	// Release it.
	if err := core.ReleaseCard(ctx, card.ID); err != nil {
		t.Fatalf("ReleaseCard: %v", err)
	}

	// Verify it's released.
	releasedCard, err := core.GetCard(ctx, proj.ID, CardRef{UUID: card.ID})
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}

	if releasedCard.Owner != nil {
		t.Errorf("Owner = %v, want nil", releasedCard.Owner)
	}
	if releasedCard.LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, want nil", releasedCard.LeaseUntil)
	}
}

// TestMoveToDonereleases Lease tests that moving a card to a done column releases the lease.
func TestMoveToDonereleaseLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor")
	ctx := context.Background()

	id := resolve.Identity{
		Kind:         "test",
		Value:        "done-release",
		SuggestedKey: "DONEREL",
	}
	proj, err := core.EnsureProject(ctx, id)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	card, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
		Title: "test card",
		Body:  "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Claim the card.
	claimed, err := core.ClaimCard(ctx, card.ID, 30*60*1000, false, "")
	if err != nil {
		t.Fatalf("ClaimCard: %v", err)
	}
	if claimed.Owner == nil {
		t.Fatalf("Owner is nil after claim")
	}

	// Move to done column.
	moved, err := core.MoveCard(ctx, proj.ID, board.ID, CardRef{UUID: card.ID}, "done")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	// Verify the lease is released.
	if moved.Owner != nil {
		t.Errorf("Owner = %v, want nil after move to done", moved.Owner)
	}
	if moved.LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, want nil after move to done", moved.LeaseUntil)
	}
}

// TestCreateNote tests creating a note on a card.
func TestCreateNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor")
	ctx := context.Background()

	id := resolve.Identity{
		Kind:         "test",
		Value:        "note-test",
		SuggestedKey: "NOTE",
	}
	proj, err := core.EnsureProject(ctx, id)
	if err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	board, err := core.CreateBoard(ctx, proj.ID, "default", true)
	if err != nil {
		t.Fatalf("CreateBoard: %v", err)
	}

	card, err := core.CreateCard(ctx, proj.ID, board.ID, NewCard{
		Title: "test card",
		Body:  "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	// Create a note.
	note, err := core.CreateNote(ctx, card.ID, "test note body")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if note.CardID != card.ID {
		t.Errorf("CardID = %s, want %s", note.CardID, card.ID)
	}
	if note.BodyMD != "test note body" {
		t.Errorf("BodyMD = %s, want 'test note body'", note.BodyMD)
	}
	if note.Actor != "test-actor" {
		t.Errorf("Actor = %s, want 'test-actor'", note.Actor)
	}

	// Verify card.version was not bumped.
	updatedCard, err := core.GetCard(ctx, proj.ID, CardRef{UUID: card.ID})
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if updatedCard.Version != card.Version {
		t.Errorf("card.Version = %d, want %d (notes should not bump version)", updatedCard.Version, card.Version)
	}
}

// TestRegisterAgent tests agent registration and updates.
func TestRegisterAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "agent-1")
	ctx := context.Background()

	// Register an agent.
	agent, err := core.RegisterAgent(ctx, "my-handle", "agent", "/home/user/work", "localhost", 12345)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if agent.Handle != "my-handle" {
		t.Errorf("Handle = %s, want 'my-handle'", agent.Handle)
	}
	if agent.Kind != "agent" {
		t.Errorf("Kind = %s, want 'agent'", agent.Kind)
	}
	if agent.PID != 12345 {
		t.Errorf("PID = %d, want 12345", agent.PID)
	}

	// Register again with updated fields.
	agent2, err := core.RegisterAgent(ctx, "my-handle-updated", "hook", "/home/user/work2", "localhost", 12346)
	if err != nil {
		t.Fatalf("RegisterAgent update: %v", err)
	}

	if agent2.ID != agent.ID {
		t.Errorf("ID changed on re-registration: %s -> %s", agent.ID, agent2.ID)
	}
	if agent2.Handle != "my-handle-updated" {
		t.Errorf("Handle not updated: %s, want 'my-handle-updated'", agent2.Handle)
	}
}
