package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mtch3n/trellis/internal/vocabulary"
)

// Help is the only way an agent discovers what Trellis can do, so every
// command must appear in its parent's listing. The spec's old 25-line cap
// bought brevity by hiding commands, which cost more than it saved; what is
// enforced now is that nothing is hidden and every entry carries a summary.
func TestHelpListsEveryCommand(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			if err := cmd.Help(); err != nil {
				t.Fatalf("Help(): %v", err)
			}
			help := buf.String()
			for _, sub := range cmd.Commands() {
				// `completion` is cobra-generated shell plumbing, not a
				// Trellis command, and is deliberately not advertised.
				if sub.Name() == "completion" {
					continue
				}
				if sub.Hidden {
					t.Errorf("%s is hidden; agents only find what help lists", sub.CommandPath())
					continue
				}
				if sub.Short == "" {
					t.Errorf("%s has no Short summary", sub.CommandPath())
				}
				if !strings.Contains(help, sub.Name()) {
					t.Errorf("%s is missing from its parent's help:\n%s", sub.CommandPath(), help)
				}
			}
		})
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
}

// Help text is how an agent learns the vocabulary, so it must use the
// glossary's words.
func TestHelpUsesTheGlossary(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		texts := map[string]string{"use": cmd.Use, "short": cmd.Short, "long": cmd.Long}
		cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
			texts["--"+f.Name] = f.Name + " " + f.Usage
		})
		for where, text := range texts {
			if rules := vocabulary.Find(text); len(rules) > 0 {
				t.Errorf("%s %s breaks %v: %q", cmd.CommandPath(), where, rules, text)
			}
		}
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
