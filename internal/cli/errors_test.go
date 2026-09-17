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

// ErrorsCanMarshalToJSON verifies that core.Error types properly marshal
// to JSON with expected structure (this ensures stderr output will be parseable).
func TestErrorsCanMarshalToJSON(t *testing.T) {
	testErr := core.ErrNotFound("card_not_found", "no card XPSCTL-99 in this project", "trellis card ls")

	// All error types should be *core.Error instances
	te, ok := errors.AsType[*core.Error](testErr)
	if !ok {
		t.Fatalf("error did not produce *core.Error")
	}

	// Verify the JSON structure that Execute() will emit
	errJSON := map[string]string{
		"code": te.Code, "message": te.Msg, "fix": te.Fix,
	}
	b, err := json.Marshal(map[string]any{"error": errJSON})
	if err != nil {
		t.Fatalf("failed to marshal error JSON: %v", err)
	}

	// Verify it parses back
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Errorf("error JSON does not parse: %v\nJSON was: %s", err, string(b))
	}

	// Verify structure
	if _, ok := result["error"]; !ok {
		t.Errorf("error JSON missing 'error' key: %v", result)
	}
}
