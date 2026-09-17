package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// Every error is at most three lines: what is wrong, then a runnable fix.
func TestErrorsAreAtMostThreeLines(t *testing.T) {
	cases := []error{
		core.ErrUsage("missing_title", "a card needs a title", `trellis card new --title "..."`),
		core.ErrNotFound("card_not_found", "no card XPSCTL-99 in this project", "trellis card ls"),
		core.ErrConflict("conflict", "XPSCTL-12 changed since you read it (you: v9, now: v11)",
			"trellis card show XPSCTL-12 --json"),
		core.ErrNotFound("column_not_found",
			"no column \"shipped\" (have: backlog, in-progress, review, done)", "trellis column ls"),
	}
	for _, err := range cases {
		msg := err.Error()
		if n := strings.Count(msg, "\n") + 1; n > 3 {
			t.Errorf("error is %d lines, cap is 3:\n%s", n, msg)
		}
		if strings.HasSuffix(strings.SplitN(msg, "\n", 2)[0], ".") {
			t.Errorf("first line should not end in a period: %q", msg)
		}
	}
}

func TestExitCodesAreDistinct(t *testing.T) {
	want := map[string]int{
		"usage": 2, "not_found": 3, "conflict": 4, "policy": 5,
	}
	got := map[string]int{}
	for name, err := range map[string]error{
		"usage":     core.ErrUsage("a", "b", "c"),
		"not_found": core.ErrNotFound("a", "b", "c"),
		"conflict":  core.ErrConflict("a", "b", "c"),
		"policy":    core.ErrPolicy("a", "b", "c"),
	} {
		te, ok := errors.AsType[*core.Error](err)
		if !ok {
			t.Fatalf("%s did not produce a *core.Error", name)
		}
		got[name] = te.Exit
	}
	for name, code := range want {
		if got[name] != code {
			t.Errorf("%s exit = %d, want %d", name, got[name], code)
		}
	}
}

// The JSON error carries the error's detail, so a caller that hits
// contention learns who claims the card without a second lookup.
func TestErrorBodyCarriesDetail(t *testing.T) {
	plain := errorBody(&core.Error{Code: "card_not_found", Msg: "no card XPSCTL-99", Fix: "trellis card ls"})
	if _, ok := plain["detail"]; ok {
		t.Errorf("an error without detail should not carry one: %v", plain)
	}

	claimant := &core.Agent{ID: "agent:claimant", Handle: "claimant"}
	body := errorBody(&core.Error{
		Code:   "contention",
		Msg:    "card claimed by claimant",
		Detail: core.ContentionInfo{ClaimedBy: claimant, RecommendedAction: "take_another_card"},
	})
	b, err := json.Marshal(map[string]any{"error": body})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Error struct {
			Code   string `json:"code"`
			Detail struct {
				ClaimedBy struct {
					ID string `json:"id"`
				} `json:"claimed_by"`
				RecommendedAction string `json:"recommended_action"`
			} `json:"detail"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("error JSON does not parse: %v\n%s", err, b)
	}
	if got.Error.Code != "contention" || got.Error.Detail.ClaimedBy.ID != "agent:claimant" ||
		got.Error.Detail.RecommendedAction != "take_another_card" {
		t.Errorf("detail lost on the wire: %s", b)
	}
}
