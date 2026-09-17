package cli

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

// newCardImportCmd turns an implementation plan into a board in one call
// (story A2.8). The source is "-" (stdin, the default) or "@file".
func newCardImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [-|@file]",
		Short: "Create many cards from a JSON array",
		Long: `Create many cards from a JSON array, in one transaction.

  [{"id":"a","title":"write the migration","labels":["chore"]},
   {"title":"use it","blocked_by":["a"]}]

"id" is a handle local to this import, so a card can depend on one that has no
ref yet. "blocked_by" also accepts an existing ref like XPSCTL-12.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := "-"
			if len(args) == 1 {
				src = args[0]
			}
			raw, err := readSource(src)
			if err != nil {
				return err
			}
			var items []core.ImportCard
			if err := json.Unmarshal(raw, &items); err != nil {
				return core.ErrUsage("bad_json", "the input is not a JSON array of cards: "+err.Error(),
					`echo '[{"title":"first step"}]' | trellis card import`)
			}
			return withBoard(func(app *appCtx) error {
				cards, err := app.Core.ImportCards(cmd.Context(), app.Project.ID, app.Board.ID, items)
				if err != nil {
					return err
				}
				return Emit(cmd, cards, func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "imported %d cards into %s\n", len(cards), app.Board.Name)
					for _, c := range cards {
						fmt.Fprintf(&b, "  %s  %s\n", c.Ref, c.Title)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

// readSource accepts "-" for stdin or "@path" for a file, matching TextValue.
func readSource(src string) ([]byte, error) {
	switch {
	case src == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, core.ErrUsage("stdin_unreadable", "could not read stdin: "+err.Error(),
				"trellis card import @plan.json")
		}
		return b, nil
	case strings.HasPrefix(src, "@"):
		b, err := os.ReadFile(src[1:])
		if err != nil {
			return nil, core.ErrUsage("file_unreadable", err.Error(),
				"trellis card import @plan.json")
		}
		return b, nil
	default:
		return nil, core.ErrUsage("bad_source", "the source is \"-\" for stdin or \"@file\"",
			"trellis card import @plan.json")
	}
}
