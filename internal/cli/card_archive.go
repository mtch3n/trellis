package cli

import (
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newCardArchiveCmd() *cobra.Command {
	var restore bool
	// Archiving and restoring are one command with a flag, not two: the pair
	// reads as one idea.
	cmd := &cobra.Command{
		Use:   "archive <card> [--restore]",
		Short: "Archive a card, releasing any claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				action, fn := "archived", app.Core.ArchiveCard
				if restore {
					action, fn = "restored", app.Core.RestoreCard
				}
				card, err := fn(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
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
