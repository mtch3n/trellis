package cli

import (
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "artifact", Short: "Store and link filesystem artifacts", Hidden: true}
	cmd.AddCommand(newArtifactAddCmd(), newArtifactLsCmd(), newArtifactLinkCmd(), newArtifactRmCmd())
	return cmd
}

func newArtifactRmCmd() *cobra.Command {
	return &cobra.Command{Use: "rm <artifact>", Short: "Delete an artifact and its links", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withBoard(func(app *appCtx) error {
			if err := app.Core.DeleteArtifact(cmd.Context(), app.Project.ID, args[0]); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"deleted": args[0]}, func() string { return "deleted " + args[0] })
		})
	}}
}

func newArtifactAddCmd() *cobra.Command {
	var card string
	cmd := &cobra.Command{
		Use: "add <file>", Short: "Copy an image or other permitted artifact into Trellis",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				artifact, err := app.Core.CreateArtifact(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				if card != "" {
					id, err := cardID(cmd, app, card)
					if err != nil {
						return err
					}
					if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, artifact.ID); err != nil {
						return err
					}
				}
				return Emit(cmd, artifact, func() string { return artifact.ID + "  " + artifact.Path })
			})
		},
	}
	cmd.Flags().StringVar(&card, "card", "", "attach to a card reference")
	return cmd
}

func newArtifactLinkCmd() *cobra.Command {
	var card string
	cmd := &cobra.Command{
		Use: "link <artifact>", Short: "Link an existing artifact to a card",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if card == "" {
				return core.ErrUsage("missing_card", "specify --card", "trellis artifact link <id> --card <card>")
			}
			return withBoard(func(app *appCtx) error {
				id, err := cardID(cmd, app, card)
				if err != nil {
					return err
				}
				if err := app.Core.LinkArtifactToCard(cmd.Context(), app.Project.ID, id, args[0]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"artifact": args[0], "card": card}, func() string { return card + " -> " + args[0] })
			})
		},
	}
	cmd.Flags().StringVar(&card, "card", "", "card reference")
	return cmd
}

func newArtifactLsCmd() *cobra.Command {
	var card string
	cmd := &cobra.Command{
		Use: "ls", Short: "List stored artifacts", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				cardIDValue := ""
				if card != "" {
					var err error
					cardIDValue, err = cardID(cmd, app, card)
					if err != nil {
						return err
					}
				}
				items, err := app.Core.ListArtifacts(cmd.Context(), app.Project.ID, cardIDValue)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"artifacts": items}, func() string {
					var b strings.Builder
					for _, item := range items {
						fmt.Fprintf(&b, "%s  %s  %s\n", item.ID, item.Kind, item.Path)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	cmd.Flags().StringVar(&card, "card", "", "only artifacts linked to this card")
	return cmd
}
