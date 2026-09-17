package cli

import (
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
)

func newCardRelateCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "relate <card> <relation> <other-card>",
		Short: "Record how a card relates to another: blocked-by, blocks, resolved-by, resolves, duplicate-of, duplicated-by, relates-to",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The other card takes a reference too, so it can name the
			// project: a relation, like a blocker, lives in its own card's
			// project.
			return withTargets([]refArg{
				{Collection: vpath.CollectionCards, Value: args[0]},
				{Collection: vpath.CollectionCards, Value: args[2]},
			}, func(app *appCtx, refs []string) error {
				ctx := cmd.Context()
				ref, rel, other := core.ParseCardRef(refs[0]), args[1], core.ParseCardRef(refs[1])
				change := app.Core.RelateCards
				if remove {
					change = app.Core.UnrelateCards
				}
				if err := change(ctx, app.Project.ID, ref, rel, other); err != nil {
					return err
				}
				card, err := app.Core.GetCard(ctx, app.Project.ID, ref)
				if err != nil {
					return err
				}
				relations, err := app.Core.CardRelations(ctx, card.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"ref": card.Ref, "relations": relations}, func() string {
					return card.Ref + renderRelations(relations)
				})
			})
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the relation instead of recording it")
	return cmd
}

func renderRelations(relations []core.CardRelation) string {
	if len(relations) == 0 {
		return "\n  no relations"
	}
	var b strings.Builder
	for _, r := range relations {
		b.WriteString("\n  " + strings.ReplaceAll(r.Rel, "_", "-") + " " + r.Ref + "  [" + r.Column + "]  " + r.Title)
	}
	return b.String()
}
