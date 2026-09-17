package cli

import (
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
)

// newLinkCmd is the structured card-to-doc relationship (§10.2). Wikilinks
// cover doc-to-doc; this is how a card says which entry documents it.
func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <card> <entry[#anchor]>",
		Short: "Link a card to a knowledge entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.LinkCardToDoc(cmd.Context(), app.Project.ID,
					core.ParseCardRef(ref), args[1]); err != nil {
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
		Use:   "graph <card|entry|artifact>",
		Short: "Walk links from a card or entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			run := func(collection string) func(*appCtx, string) error {
				return func(app *appCtx, ref string) error {
					startID, err := resolveEntity(cmd, app, collection, ref)
					if err != nil {
						return err
					}
					g, err := app.Core.Traverse(cmd.Context(), startID, depth, rels, reverse)
					if err != nil {
						return err
					}
					return Emit(cmd, g, func() string { return graphTree(g) })
				}
			}
			switch {
			case strings.HasPrefix(arg, "/"):
				target, _ := vpath.SplitAnchor(arg)
				p, err := vpath.Parse(strings.TrimSpace(target))
				if err != nil {
					return core.ErrUsage("bad_path", err.Error(), "trellis search <words>   # results carry valid addresses")
				}
				switch p.Collection {
				case vpath.CollectionCards, vpath.CollectionKnowledge, vpath.CollectionArtifacts:
				default:
					return core.ErrUsage("wrong_collection",
						arg+" names a project or a board; a graph starts from a card, an entry or an artifact",
						"trellis card ls")
				}
				return withTarget(refArg{Collection: p.Collection, Value: arg, NoProject: true}, run(p.Collection))
			case vpath.ValidCardRef(strings.ToUpper(arg)):
				// KEY-N is a card ref everywhere, and names its project.
				return withTarget(refArg{Collection: vpath.CollectionCards, Value: arg}, run(vpath.CollectionCards))
			}
			relative := run("")
			return withBoard(func(app *appCtx) error { return relative(app, arg) })
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 2, "how many hops to walk")
	cmd.Flags().StringSliceVar(&rels, "rel", nil, "blocked_by, documents, wikilink")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk inbound: what breaks if this changes")
	return cmd
}

// resolveEntity finds where a walk starts. A collection says what the argument
// is; a relative argument of unknown kind tries an entry first, then a card.
func resolveEntity(cmd *cobra.Command, app *appCtx, collection, ref string) (string, error) {
	ctx := cmd.Context()
	switch collection {
	case vpath.CollectionKnowledge:
		doc, err := app.Core.LoadKnowledge(ctx, app.Project.ID, ref)
		return doc.ID, err
	case vpath.CollectionCards:
		card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
		return card.ID, err
	case vpath.CollectionArtifacts:
		a, err := app.Core.ResolveArtifact(ctx, app.Project.ID, ref)
		return a.ID, err
	}
	if doc, err := app.Core.LoadKnowledge(ctx, app.Project.ID, ref); err == nil {
		return doc.ID, nil
	}
	card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
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
