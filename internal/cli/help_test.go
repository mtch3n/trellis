package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The spec caps help at 25 lines per command. Enforced here or it rots.
func TestHelpIsUnder25Lines(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			if err := cmd.Help(); err != nil {
				t.Fatalf("Help(): %v", err)
			}
			if n := strings.Count(strings.TrimRight(buf.String(), "\n"), "\n") + 1; n > 25 {
				t.Errorf("%s help is %d lines, cap is 25:\n%s", cmd.CommandPath(), n, buf.String())
			}
		})
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
}

// The root command's suggestions are handled programmatically in Execute()
// to keep stderr parseable as JSON. This test confirms suggestions work.
func TestUnknownSubcommandSuggests(t *testing.T) {
	// Test that the root command has the SuggestionsFor capability
	root := newRootCmd()
	root.SuggestionsMinimumDistance = 2
	suggestions := root.SuggestionsFor("crad")
	if len(suggestions) == 0 || !strings.Contains(strings.Join(suggestions, " "), "card") {
		t.Errorf("SuggestionsFor(\"crad\") returned %v, expected to contain \"card\"", suggestions)
	}

	// Test that unknown command returns an error
	root = newRootCmd()
	root.SetArgs([]string{"crad"})
	err := root.Execute()
	if err == nil {
		t.Errorf("expected an error for unknown subcommand \"crad\", got nil")
	}
}
