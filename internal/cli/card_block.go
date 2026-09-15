package cli

import (
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newCardBlockCmd() *cobra.Command {
	var by string
	var remove bool
	cmd := &cobra.Command{
		Use:   "block <card> --by <card> [--remove]",
		Short: "Record or remove a blocked-by link",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if by == "" {
				return core.ErrUsage("missing_blocker", "say which card blocks it",
					"trellis card block "+args[0]+" --by 12")
			}
			return withBoard(func(app *appCtx) error {
				link := app.Core.BlockCard
				if remove {
					link = app.Core.UnblockCard
				}
				if err := link(cmd.Context(), app.Project.ID,
					core.ParseCardRef(args[0]), core.ParseCardRef(by)); err != nil {
					return err
				}
				return emitBlockers(cmd, app, args[0])
			})
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "the blocking card")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the link instead of adding it")
	return cmd
}

// emitBlockers prints the card's remaining blockers, so block and unblock both
// answer "can this be worked on now" rather than only confirming the write.
func emitBlockers(cmd *cobra.Command, app *appCtx, ref string) error {
	card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return err
	}
	blockers, err := app.Core.Blockers(cmd.Context(), card.ID)
	if err != nil {
		return err
	}
	return Emit(cmd, map[string]any{"ref": card.Ref, "blocked_by": blockers}, func() string {
		return card.Ref + " blocked by: " + blockerLine(blockers)
	})
}
func blockerLine(blockers []core.Blocker) string {
	if len(blockers) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(blockers))
	for _, b := range blockers {
		s := b.Ref
		if b.Done {
			s += " (done)"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}
