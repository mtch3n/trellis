package cli

import (
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newCardArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <card>",
		Short: "Archive a card, releasing any claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				card, err := app.Core.ArchiveCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string { return "archived " + card.Ref + "  " + card.Title })
			})
		},
	}
}

func newCardRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <card>",
		Short: "Put an archived or trashed card back on its board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				card, err := app.Core.RestoreCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string { return "restored " + card.Ref + "  " + card.Title })
			})
		},
	}
}
