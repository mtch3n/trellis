package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/store"
)

func TestClaimContentionNamesTheClaimantAndStealRecordsWhy(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "contended"})
	if err != nil {
		t.Fatal(err)
	}

	claimant := New(c.db, c.clock, "sess:claimant", c.root)
	if _, err := claimant.RegisterAgent(t.Context(), "worker-1", "agent", "/tmp", "host", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := claimant.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	// A second agent must be told who claims it, not left waiting on a
	// connection the transaction is already holding.
	other := New(c.db, c.clock, "sess:other", c.root)
	done := make(chan error, 1)
	go func() {
		_, err := other.ClaimCard(t.Context(), card.ID, 60_000, false, "")
		done <- err
	}()
	select {
	case err := <-done:
		var te *Error
		if !errors.As(err, &te) || te.Exit != 4 {
			t.Fatalf("contended claim = %v, want a conflict naming the claimant", err)
		}
		if !strings.Contains(te.Msg, "worker-1") {
			t.Errorf("message %q does not name the claimant's handle", te.Msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("contended claim hung: a nested transaction is waiting for the only connection")
	}

	stolen, err := other.ClaimCard(t.Context(), card.ID, 60_000, true, "claimed 4h with no notes")
	if err != nil {
		t.Fatalf("steal: %v", err)
	}
	if stolen.ClaimedBy == nil || *stolen.ClaimedBy != "sess:other" {
		t.Fatalf("ClaimedBy = %v, want the stealer", stolen.ClaimedBy)
	}
	notes, err := c.GetCommentsByCard(t.Context(), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0].BodyMD, "claimed 4h with no notes") {
		t.Errorf("notes = %+v, want the reason recorded where the displaced agent will see it", notes)
	}
}
func TestUnregisteredClaimantStillHoldsTheCard(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "claimed by a stranger"})
	if err != nil {
		t.Fatal(err)
	}
	// No RegisterAgent call: a hook may not have run, but the claim is real.
	claimant := New(c.db, c.clock, "sess:unregistered", c.root)
	if _, err := claimant.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	other := New(c.db, c.clock, "sess:other", c.root)
	_, err = other.ClaimCard(t.Context(), card.ID, 60_000, false, "")
	var te *Error
	if !errors.As(err, &te) || te.Exit != 4 {
		t.Fatalf("claim = %v, want a conflict: the claim grants the hold, not the agent row", err)
	}
}

// A refused claim operation says whose claim is in the way: contention when
// another actor holds the card, not_yours when the caller holds nothing to
// release or renew.
func TestClaimRefusalsNameWhoseClaimItIs(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	ctx := t.Context()
	card, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "claimed elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	code := func(err error) string {
		if e, ok := errors.AsType[*Error](err); ok {
			return e.Code
		}
		return fmt.Sprint(err)
	}

	if got := code(c.ReleaseCard(ctx, card.ID)); got != "not_yours" {
		t.Errorf("release of an unclaimed card = %s, want not_yours", got)
	}
	if got := code(c.RenewClaim(ctx, card.ID, 0)); got != "not_yours" {
		t.Errorf("renew of an unclaimed card = %s, want not_yours", got)
	}

	claimant := New(c.db, c.clock, "sess:claimant", c.root)
	if _, err := claimant.ClaimCard(ctx, card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}
	if got := code(c.ReleaseCard(ctx, card.ID)); got != "not_yours" {
		t.Errorf("release of another actor's claim = %s, want not_yours", got)
	}
	if got := code(c.RenewClaim(ctx, card.ID, 0)); got != "not_yours" {
		t.Errorf("renew of another actor's claim = %s, want not_yours", got)
	}
	_, err = c.ClaimCard(ctx, card.ID, 60_000, false, "")
	if got := code(err); got != "contention" {
		t.Errorf("claim of another actor's card = %s, want contention", got)
	}
	_, err = c.EditCard(ctx, p.ID, CardRef{UUID: card.ID}, CardEdit{Title: new("edited")})
	if got := code(err); got != "contention" {
		t.Errorf("edit of another actor's card = %s, want contention", got)
	}
	_, err = c.ArchiveCard(ctx, p.ID, CardRef{UUID: card.ID})
	if got := code(err); got != "contention" {
		t.Errorf("archive of another actor's card = %s, want contention", got)
	}
}

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

	core := New(db, FixedClock{MS: 1000000}, "test-setup", filepath.Dir(path))
	ctx := context.Background()

	// Create project and board.
	proj, err := core.CreateProject(ctx, "CLAIM", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
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

	var claimed, unclaimed int
	if err := db.Get(&claimed, `SELECT count(*) FROM card WHERE claimed_by IS NOT NULL`); err != nil {
		t.Fatalf("count claimed: %v", err)
	}
	if err := db.Get(&unclaimed, `SELECT count(*) FROM card WHERE claimed_by IS NULL`); err != nil {
		t.Fatalf("count unclaimed: %v", err)
	}

	if claimed != 50 {
		t.Errorf("claimed = %d, want 50", claimed)
	}
	if unclaimed != 0 {
		t.Errorf("unclaimed = %d, want 0", unclaimed)
	}

	// Verify no duplicates: each card's claimant should appear exactly once.
	var claimantCounts map[string]int
	rows, err := db.Query(`SELECT claimed_by, count(*) FROM card WHERE claimed_by IS NOT NULL GROUP BY claimed_by`)
	if err != nil {
		t.Fatalf("query claimant counts: %v", err)
	}
	defer rows.Close()
	claimantCounts = make(map[string]int)
	for rows.Next() {
		var claimant string
		var count int
		if err := rows.Scan(&claimant, &count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		claimantCounts[claimant] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Each process should have claimed some cards; verify no card is claimed twice.
	totalClaimed := 0
	for claimant, count := range claimantCounts {
		if count > 50 { // Sanity check: no process should claim more than all cards
			t.Errorf("claimant %s claimed %d cards (impossible)", claimant, count)
		}
		totalClaimed += count
	}
	if totalClaimed != 50 {
		t.Errorf("total claimed = %d, want 50", totalClaimed)
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

	core := New(db, FixedClock{MS: 1000000}, "test-actor", filepath.Dir(path))
	ctx := context.Background()

	proj, err := core.CreateProject(ctx, "CLAIMSPEC", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
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

	if claimed.ClaimedBy == nil || *claimed.ClaimedBy != "test-actor" {
		t.Errorf("ClaimedBy = %v, want test-actor", claimed.ClaimedBy)
	}
	if claimed.ClaimUntil == nil {
		t.Errorf("ClaimUntil is nil, want a timestamp")
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
	if got.ClaimedBy != nil || got.ClaimUntil != nil || got.Version != card.Version {
		t.Fatalf("preview changed card: claimant=%v claim=%v version=%d, want nil nil %d",
			got.ClaimedBy, got.ClaimUntil, got.Version, card.Version)
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

	core := New(db, FixedClock{MS: 1000000}, "test-actor", filepath.Dir(path))
	ctx := context.Background()

	proj, err := core.CreateProject(ctx, "RELEASE", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
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

	if releasedCard.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil", releasedCard.ClaimedBy)
	}
	if releasedCard.ClaimUntil != nil {
		t.Errorf("ClaimUntil = %v, want nil", releasedCard.ClaimUntil)
	}
}

// TestMoveToDonereleaseClaim tests that moving a card to a done column releases the claim.
func TestMoveToDonereleaseClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	core := New(db, FixedClock{MS: 1000000}, "test-actor", filepath.Dir(path))
	ctx := context.Background()

	proj, err := core.CreateProject(ctx, "DONEREL", false)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
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
	if claimed.ClaimedBy == nil {
		t.Fatalf("ClaimedBy is nil after claim")
	}

	// Move to done column.
	moved, err := core.MoveCard(ctx, proj.ID, board.ID, CardRef{UUID: card.ID}, "done")
	if err != nil {
		t.Fatalf("MoveCard: %v", err)
	}

	// Verify the claim is released.
	if moved.ClaimedBy != nil {
		t.Errorf("ClaimedBy = %v, want nil after move to done", moved.ClaimedBy)
	}
	if moved.ClaimUntil != nil {
		t.Errorf("ClaimUntil = %v, want nil after move to done", moved.ClaimUntil)
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
	core := New(db, RealClock{}, actor, filepath.Dir(dbPath))
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

func TestClaimedCardRejectsOtherWritesButAllowsNotes(t *testing.T) {
	c := testCore(t)
	p := seededProject(t, c)
	b := seededBoard(t, c, p)
	card, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "claimed"})
	if err != nil {
		t.Fatal(err)
	}
	claimant := New(c.db, FixedClock{MS: 1000}, "claimant", c.root)
	if _, err := claimant.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}
	other := New(c.db, FixedClock{MS: 1001}, "other", c.root)
	priority := PriorityUrgent
	if _, err := other.EditCard(t.Context(), p.ID, CardRef{Seq: card.Seq}, CardEdit{Priority: &priority}); err == nil {
		t.Fatal("other actor edited an actively claimed card")
	} else if e, ok := errors.AsType[*Error](err); !ok || e.Exit != 4 {
		t.Fatalf("edit error = %v, want conflict", err)
	}
	if _, err := other.CreateComment(t.Context(), card.ID, "handoff"); err != nil {
		t.Fatalf("note exception: %v", err)
	}
}
