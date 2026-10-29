package mcpserver_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

func TestPlat_PLAT_C13_TheClaimantNamesHandleTokenAndSession(t *testing.T) {
	e := newEnv(t)
	cs := e.agent("ming", "claude-laptop", headers{"X-Trellis-Session": "s1"})
	card := ok[cardOut](t, cs, "card_write", map[string]any{"action": "new", "title": "ship"}).Card
	got := ok[claimOut](t, cs, "card_claim", map[string]any{"action": "claim", "ref": card.Ref}).Card
	if got.ClaimedBy == nil || *got.ClaimedBy != "agent:ming/claude-laptop/s1" {
		t.Fatalf("claimant = %v, want agent:ming/claude-laptop/s1", got.ClaimedBy)
	}
}

func TestPlat_PLAT_C14_ABadSessionHeaderIsRefusedAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	token := e.member("ming", "claude-laptop")
	for name, session := range map[string]string{
		"too long": strings.Repeat("s", 65),
		"bad char": "s1/evil",
		"space":    "s 1",
	} {
		t.Run(name, func(t *testing.T) {
			cs := e.connect(token, headers{"X-Trellis-Project": "APP", "X-Trellis-Session": session}, "")
			got := fails(t, cs, "card_write", map[string]any{"action": "new", "title": "ship"})
			if got.Code != "bad_session" {
				t.Fatalf("code = %q, want bad_session", got.Code)
			}
			if n := e.count("card"); n != 0 {
				t.Fatalf("%d cards written", n)
			}
		})
	}
}

func TestPlat_PLAT_C15_TwoSessionsOfOneTokenContendForOneCard(t *testing.T) {
	e := newEnv(t)
	token := e.member("ming", "claude-laptop")
	s1 := e.connect(token, headers{"X-Trellis-Project": "APP", "X-Trellis-Session": "s1"}, "")
	s2 := e.connect(token, headers{"X-Trellis-Project": "APP", "X-Trellis-Session": "s2"}, "")
	card := ok[cardOut](t, s1, "card_write", map[string]any{"action": "new", "title": "ship"}).Card

	type result struct {
		actor   string
		res     *mcp.CallToolResult
		callErr error
		err     *toolErr
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i, cs := range []*sessionClient{{s1, "agent:ming/claude-laptop/s1"}, {s2, "agent:ming/claude-laptop/s2"}} {
		wg.Go(func() {
			results[i].actor = cs.actor
			results[i].res, results[i].callErr = cs.cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "card_claim",
				Arguments: map[string]any{"action": "claim", "ref": card.Ref}})
		})
	}
	wg.Wait()
	for i := range results {
		if results[i].callErr != nil {
			t.Fatal(results[i].callErr)
		}
		if results[i].res.IsError {
			e := decodeErr(t, results[i].res)
			results[i].err = &e
		}
	}

	var winner, loser *result
	for i := range results {
		if results[i].err == nil {
			winner = &results[i]
		} else {
			loser = &results[i]
		}
	}
	if winner == nil || loser == nil {
		t.Fatalf("want one winner and one loser, got %+v", results)
	}
	if loser.err.Code != "contention" || !strings.Contains(loser.err.Message, winner.actor) {
		t.Fatalf("loser got %+v, want contention that names %s", *loser.err, winner.actor)
	}
	shown := ok[platform.Card](t, s1, "card_show", map[string]any{"ref": card.Ref})
	if shown.ClaimedBy == nil || *shown.ClaimedBy != winner.actor {
		t.Fatalf("claimant = %v, want %s", shown.ClaimedBy, winner.actor)
	}
}
