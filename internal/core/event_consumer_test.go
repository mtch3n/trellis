package core

import (
	"errors"
	"testing"
)

func TestEnsureEventConsumerCreatesOnFirstUse(t *testing.T) {
	c := testCore(t)
	ec, err := c.EnsureEventConsumer(t.Context(), "reviewer")
	if err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}
	if ec.Name != "reviewer" || ec.Cursor != 0 {
		t.Fatalf("consumer = %+v, want cursor 0", ec)
	}

	again, err := c.EnsureEventConsumer(t.Context(), "reviewer")
	if err != nil {
		t.Fatalf("EnsureEventConsumer again: %v", err)
	}
	if again.CreatedAt != ec.CreatedAt {
		t.Errorf("second EnsureEventConsumer created a new row: %+v vs %+v", again, ec)
	}
}

func TestAckAdvancesTheCursor(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events to ack")
	}
	if _, err := c.EnsureEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}

	ec, err := c.AckEventConsumer(t.Context(), "worker", events[len(events)-1].Seq)
	if err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}
	if ec.Cursor != events[len(events)-1].Seq {
		t.Errorf("cursor = %d, want %d", ec.Cursor, events[len(events)-1].Seq)
	}
}

func TestAckNeverMovesBackwards(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	last := events[len(events)-1].Seq
	first := events[0].Seq

	if _, err := c.AckEventConsumer(t.Context(), "worker", last); err != nil {
		t.Fatalf("first ack: %v", err)
	}
	ec, err := c.AckEventConsumer(t.Context(), "worker", first)
	if err != nil {
		t.Fatalf("second (older) ack: %v", err)
	}
	if ec.Cursor != last {
		t.Errorf("cursor = %d after acking an older seq, want it to stay at %d", ec.Cursor, last)
	}
}

func TestAckRefusesASeqPastTheNewest(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	newest := events[len(events)-1].Seq

	_, err = c.AckEventConsumer(t.Context(), "worker", newest+1000)
	if code := consumerErrCode(err); code != "seq_too_new" {
		t.Fatalf("err = %v, want seq_too_new", err)
	}
}

// A single FixedClock timestamps every write identically, so PruneHistory's
// timestamp cutoff cannot express "prune some but not all" within one Core.
// This test opens a second Core on the same database, one tick later, the
// same technique internal/core/lease_test.go:474-478 already uses to test
// time-dependent behavior against a shared connection.
func TestEventGapAfterPartialPruning(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	baseMS := c.clock.NowMS()
	later := New(c.db, FixedClock{MS: baseMS + 1000}, c.actor)
	if _, err := later.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "later"}); err != nil {
		t.Fatalf("CreateCard (later): %v", err)
	}

	if _, err := c.PruneHistory(t.Context(), baseMS+500, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, oldest, err := c.EventGapAfter(t.Context(), events[0].Seq)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if !gap {
		t.Fatal("want a gap: pruning removed events past the consumer's cursor")
	}
	if oldest <= events[0].Seq {
		t.Errorf("oldest = %d, want it greater than the acked cursor %d", oldest, events[0].Seq)
	}
}

// If pruning removes every event, MIN(seq) has nothing to report at all --
// COALESCE would otherwise default it to 0 and the ordinary oldest > after+1
// comparison would wrongly say there is no gap, when in fact everything,
// including whatever the consumer had not yet reached, is gone.
func TestEventGapWhenEveryEventIsPruned(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	if _, err := c.PruneHistory(t.Context(), c.clock.NowMS()+1, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, _, err := c.EventGapAfter(t.Context(), events[0].Seq)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if !gap {
		t.Error("want a gap: every event, including ones past the cursor, is gone")
	}
}

func TestEventGapIsFalseForABrandNewConsumer(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.PruneHistory(t.Context(), c.clock.NowMS()+1, true, false); err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}

	gap, _, err := c.EventGapAfter(t.Context(), 0)
	if err != nil {
		t.Fatalf("EventGapAfter: %v", err)
	}
	if gap {
		t.Error("a brand-new consumer (cursor 0) must not report a gap: it never tracked the pruned events")
	}
}

func TestListEventConsumersReportsCursorLagAndGap(t *testing.T) {
	c, p, b := kbCore(t)
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "a"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateCard(t.Context(), p.ID, b.ID, NewCard{Title: "b"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	events, _, err := c.EventFeed(t.Context(), EventQuery{ProjectID: p.ID})
	if err != nil {
		t.Fatalf("EventFeed: %v", err)
	}
	if _, err := c.AckEventConsumer(t.Context(), "worker", events[0].Seq); err != nil {
		t.Fatalf("AckEventConsumer: %v", err)
	}

	list, err := c.ListEventConsumers(t.Context())
	if err != nil {
		t.Fatalf("ListEventConsumers: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v, want one consumer", list)
	}
	s := list[0]
	if s.Name != "worker" || s.Cursor != events[0].Seq {
		t.Fatalf("status = %+v, want name=worker cursor=%d", s, events[0].Seq)
	}
	if s.Lag != events[len(events)-1].Seq-events[0].Seq {
		t.Errorf("lag = %d, want %d", s.Lag, events[len(events)-1].Seq-events[0].Seq)
	}
	if s.Gap {
		t.Error("no prune happened; gap must be false")
	}
}

func TestDeleteEventConsumer(t *testing.T) {
	c := testCore(t)
	if _, err := c.EnsureEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("EnsureEventConsumer: %v", err)
	}
	if err := c.DeleteEventConsumer(t.Context(), "worker"); err != nil {
		t.Fatalf("DeleteEventConsumer: %v", err)
	}
	list, err := c.ListEventConsumers(t.Context())
	if err != nil {
		t.Fatalf("ListEventConsumers: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list = %+v, want empty after delete", list)
	}
	// Deleting an unknown consumer is not an error: a caller cleaning up
	// after an extension it never ran needs no special case.
	if err := c.DeleteEventConsumer(t.Context(), "never-existed"); err != nil {
		t.Errorf("DeleteEventConsumer of an unknown name: %v, want nil", err)
	}
}

func consumerErrCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}
