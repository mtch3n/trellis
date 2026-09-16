package cli

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/mtch3n/trellis/internal/core"
)

func TestRunEventsFollowPrintsAnEventWrittenAfterItStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var seen []int64
	calls := 0
	fetch := func(after int64) ([]core.FeedEvent, *int64, error) {
		calls++
		if calls == 1 {
			// Nothing yet: this is the poll that runs before anything new
			// has been written.
			return nil, nil, nil
		}
		seq := int64(1)
		// Stop the loop once the second poll has found the new event, so
		// the test does not depend on a real clock.
		cancel()
		return []core.FeedEvent{{Seq: seq, Kind: "card", Action: "created"}}, &seq, nil
	}

	err := runEventsFollow(ctx, time.Millisecond, 0, fetch, func(ev core.FeedEvent) error {
		seen = append(seen, ev.Seq)
		return nil
	})
	if err != nil {
		t.Fatalf("runEventsFollow: %v", err)
	}
	if len(seen) != 1 || seen[0] != 1 {
		t.Fatalf("seen = %v, want [1]", seen)
	}
}

func TestRunEventsFollowStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	err := runEventsFollow(ctx, time.Millisecond, 0, func(int64) ([]core.FeedEvent, *int64, error) {
		calls++
		return nil, nil, nil
	}, func(core.FeedEvent) error { return nil })
	if err != nil {
		t.Fatalf("runEventsFollow: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want exactly one poll before the cancelled context stops the loop", calls)
	}
}

func TestEventsListsCreatedCards(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "First")

	out := runCmd(t, "events")
	if !strings.Contains(out, `"action":"created"`) || !strings.Contains(out, `"kind":"card"`) {
		t.Fatalf("events output missing the card creation:\n%s", out)
	}
}

func TestEventsAfterExcludesEarlierEvents(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "First")
	first := strings.Split(strings.TrimSpace(runCmd(t, "events")), "\n")
	var firstEvent struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(first[len(first)-1]), &firstEvent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	runCmd(t, "card", "new", "--title", "Second")
	out := runCmd(t, "events", "--after", itoaTest(firstEvent.Seq))
	if strings.Contains(out, `"First"`) {
		t.Errorf("--after did not exclude the earlier event:\n%s", out)
	}
	if !strings.Contains(out, `"Second"`) {
		t.Errorf("--after excluded the later event too:\n%s", out)
	}
}

func itoaTest(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestEventsKindFilter(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "Card")
	runCmd(t, "label", "new", "urgent", "--description", "needs attention")

	out := runCmd(t, "events", "--kind", "label")
	if strings.Contains(out, `"kind":"card"`) {
		t.Errorf("--kind label still printed a card event:\n%s", out)
	}
	if !strings.Contains(out, `"kind":"label"`) {
		t.Errorf("--kind label printed no label event:\n%s", out)
	}
}

func TestEventsConsumerResumesAfterAck(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "A")

	first := runCmd(t, "events", "--consumer", "worker")
	lines := strings.Split(strings.TrimSpace(first), "\n")
	var lastSeq struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastSeq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Reading again without acking must return the same events: the cursor
	// has not moved.
	again := runCmd(t, "events", "--consumer", "worker")
	if strings.TrimSpace(again) != strings.TrimSpace(first) {
		t.Fatalf("a second read before ack must repeat the same events:\nfirst=%q\nagain=%q", first, again)
	}

	runCmd(t, "events", "ack", "worker", itoaTest(lastSeq.Seq))

	runCmd(t, "card", "new", "--title", "B")
	afterAck := runCmd(t, "events", "--consumer", "worker")
	if strings.Contains(afterAck, `"A"`) {
		t.Errorf("after ack, the consumer must not see the already-handled event again:\n%s", afterAck)
	}
	if !strings.Contains(afterAck, `"B"`) {
		t.Errorf("after ack, the consumer must see the new event:\n%s", afterAck)
	}
}

func TestEventsConsumersListsAndRemoves(t *testing.T) {
	projectEnv(t)
	runCmd(t, "card", "new", "--title", "A")
	runCmd(t, "events", "--consumer", "worker")

	out := runCmd(t, "events", "consumers", "--json")
	if !strings.Contains(out, `"name":"worker"`) {
		t.Fatalf("consumers listing missing worker:\n%s", out)
	}

	runCmd(t, "events", "consumers", "rm", "worker")
	after := runCmd(t, "events", "consumers", "--json")
	if strings.Contains(after, "worker") {
		t.Errorf("consumer still listed after rm:\n%s", after)
	}
}

func TestEventsAckRejectsANonIntegerSeq(t *testing.T) {
	projectEnv(t)
	if _, err := runCmdErr(t, "events", "ack", "worker", "not-a-number"); cliErrCode(err) != "invalid_seq" {
		t.Errorf("err = %v, want invalid_seq", err)
	}
}
