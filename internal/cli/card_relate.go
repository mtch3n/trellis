package cli

import (
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newCardRelateCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "relate <card> <rel> <other> [--remove]",
		Short: "Record or remove a card relation",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			rel := args[1]
			// Normalize: dashes to underscores
			rel = strings.ReplaceAll(rel, "-", "_")

			return withBoard(func(app *appCtx) error {
				var fn func(cmd *cobra.Command, app *appCtx, ref, rel, other string) error
				if remove {
					fn = unrelateEmitter
				} else {
					fn = relateEmitter
				}
				return fn(cmd, app, args[0], rel, args[2])
			})
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the relation instead of adding it")
	return cmd
}

func relateEmitter(cmd *cobra.Command, app *appCtx, ref, rel, other string) error {
	if err := app.Core.RelateCards(cmd.Context(), app.Project.ID,
		core.ParseCardRef(ref), rel, core.ParseCardRef(other)); err != nil {
		return err
	}
	return emitRelations(cmd, app, ref)
}

func unrelateEmitter(cmd *cobra.Command, app *appCtx, ref, rel, other string) error {
	if err := app.Core.UnrelateCards(cmd.Context(), app.Project.ID,
		core.ParseCardRef(ref), rel, core.ParseCardRef(other)); err != nil {
		return err
	}
	return emitRelations(cmd, app, ref)
}

// emitRelations prints the card's relations, so relate and unrelate both answer
// "what's the status of this card's relationships".
func emitRelations(cmd *cobra.Command, app *appCtx, ref string) error {
	card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return err
	}
	relations, err := app.Core.CardRelations(cmd.Context(), card.ID)
	if err != nil {
		return err
	}
	return Emit(cmd, map[string]any{"ref": card.Ref, "relations": relations}, func() string {
		if len(relations) == 0 {
			return card.Ref + " has no relations"
		}
		var b strings.Builder
		b.WriteString(card.Ref + " relations:\n")
		for _, r := range relations {
			b.WriteString("  " + r.Rel + ": " + r.Ref + " (" + r.Column + ")")
			if r.Done {
				b.WriteString(" [done]")
			}
			b.WriteString("\n")
		}
		return strings.TrimRight(b.String(), "\n")
	})
}
