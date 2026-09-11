package cli

import (
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

// newLinkCmd is the structured card-to-doc relationship (§10.2). Wikilinks
// cover doc-to-doc; this is how a card says which entry documents it.
func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "link <card> <slug[#anchor]>",
		Short:  "Link a card to a knowledge entry",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if err := app.Core.LinkCardToDoc(cmd.Context(), app.Project.ID,
					core.ParseCardRef(args[0]), args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"from": args[0], "to": args[1]},
					func() string { return args[0] + " -> " + args[1] })
			})
		},
	}
}

// newGraphCmd walks the link table. Backlinks answer one hop; "is the thing
// blocking me itself blocked" needs the chain (§10.12).
func newGraphCmd() *cobra.Command {
	var depth int
	var rels []string
	var reverse bool

	cmd := &cobra.Command{
		Use:    "graph <card|slug>",
		Hidden: true,
		Short:  "Walk links from a card or entry",
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				startID, err := resolveEntity(cmd, app, args[0])
				if err != nil {
					return err
				}
				g, err := app.Core.Traverse(cmd.Context(), startID, depth, rels, reverse)
				if err != nil {
					return err
				}
				return Emit(cmd, g, func() string { return graphTree(g) })
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 2, "how many hops to walk")
	cmd.Flags().StringSliceVar(&rels, "rel", nil, "blocked_by, documents, wikilink")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk inbound: what breaks if this changes")
	return cmd
}

// resolveEntity accepts either form of reference, so an agent does not have to
// know which kind of thing it is holding.
func resolveEntity(cmd *cobra.Command, app *appCtx, ref string) (string, error) {
	if doc, err := app.Core.LoadKnowledge(cmd.Context(), app.Project.ID, ref); err == nil {
		return doc.ID, nil
	}
	card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return "", core.ErrNotFound("not_found", "no card or knowledge entry "+ref,
			"trellis card ls   # or: trellis knowledge ls")
	}
	return card.ID, nil
}

func graphTree(g core.Graph) string {
	if len(g.Nodes) <= 1 {
		return "(no links)"
	}
	byDepth := map[int][]core.GraphNode{}
	max := 0
	for _, n := range g.Nodes {
		byDepth[n.Depth] = append(byDepth[n.Depth], n)
		if n.Depth > max {
			max = n.Depth
		}
	}
	var b strings.Builder
	for d := 0; d <= max; d++ {
		for _, n := range byDepth[d] {
			state := ""
			if n.Type == "card" && n.Done {
				state = " (done)"
			}
			fmt.Fprintf(&b, "%s%s  %s%s\n", strings.Repeat("  ", d), n.Ref, n.Title, state)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
