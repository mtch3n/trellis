package cli

import (
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newCardArchiveCmd() *cobra.Command {
	var restore bool
	// Archiving and restoring are one command with a flag, not two: the card
	// help stays inside its 25-line cap (§12) and the pair reads as one idea.
	cmd := &cobra.Command{
		Use:   "archive <card> [--restore]",
		Short: "Archive a card, releasing any lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				ref := core.ParseCardRef(args[0])
				action, fn := "archived", app.Core.ArchiveCard
				if restore {
					action, fn = "restored", app.Core.UnarchiveCard
				}
				card, err := fn(cmd.Context(), app.Project.ID, ref)
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string { return action + " " + card.Ref + "  " + card.Title })
			})
		},
	}
	cmd.Flags().BoolVar(&restore, "restore", false, "return an archived card to the board")
	return cmd
}

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
