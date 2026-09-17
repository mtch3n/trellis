package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

// newLinkCmd is the structured card-to-entry relationship (§10.2). Wikilinks
// cover entry-to-entry; this is how a card says which entry it cites.
//
// The spec allows link to cross projects on purpose (`trellis link <card>
// <entry>` may link a card to another project's entry), so the entry is not a
// second refArg: that would make withTargets reject the very thing this
// command exists to do, any time the entry is given as an address naming a
// different project than the card. What must not happen instead is a
// *relative* entry silently reading the card's project when a current project
// actually resolves and disagrees -- conflictIfEntryElsewhere covers that case
// alone, the same way a relative reference conflicts with a named one
// everywhere else in the CLI.
func newLinkCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "link <card> <entry[#anchor]> [--remove]",
		Short: "Link a card to an entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := conflictIfEntryElsewhere(app, args[0], args[1]); err != nil {
					return err
				}
				if remove {
					if err := app.Core.UnlinkCardFromEntry(cmd.Context(), app.Project.ID,
						core.ParseCardRef(ref), args[1]); err != nil {
						return err
					}
					return Emit(cmd, map[string]string{"from": args[0], "removed": args[1]},
						func() string { return args[0] + " -/-> " + args[1] })
				}
				if err := app.Core.LinkCardToEntry(cmd.Context(), app.Project.ID,
					core.ParseCardRef(ref), args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"from": args[0], "to": args[1]},
					func() string { return args[0] + " -> " + args[1] })
			})
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the link instead of adding it")
	return cmd
}

// conflictIfEntryElsewhere refuses a relative entry argument that would silently
// resolve in another project than the one standing in the working directory:
// a relative reference means the current project everywhere else in the CLI,
// so "design" must not quietly become the card's project's design when they
// differ. An address such as /OTHER/vault/design names its own project on
// purpose -- link's documented exception -- so only a relative argument is
// checked, and only once a current project actually resolves; with none, the
// card's project is the only candidate, exactly as withTargets falls back
// elsewhere.
func conflictIfEntryElsewhere(app *appCtx, cardArg, entry string) error {
	if strings.HasPrefix(strings.TrimSpace(entry), "/") {
		return nil
	}
	r, err := resolveProject(context.Background(), app.Core)
	if err != nil {
		return nil
	}
	if r.Project.Key != app.Project.Key {
		return projectConflict(r.Project.Key, cardArg, app.Project.Key)
	}
	return nil
}

// keyNamesProject reports whether key names a project this database has,
// including one that was merged away. A qualified card ref routes to the
// card path only then; an entry slug that merely looks like PREFIX-N
// (release-2026, adr-12) is not a card ref just because it matches the
// grammar, and falls through to the relative lookup instead, which tries an
// entry first.
func keyNamesProject(key string) bool {
	c, db, err := openCore()
	if err != nil {
		return false
	}
	defer db.Close()
	if _, err := c.ProjectByKey(context.Background(), key); err == nil {
		return true
	} else if ce, ok := errors.AsType[*core.Error](err); ok {
		return ce.Code == "project_merged"
	}
	return false
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
				target, _ := address.SplitAnchor(arg)
				p, err := address.Parse(strings.TrimSpace(target))
				if err != nil {
					return core.ErrUsage("bad_address", err.Error(), "trellis search <words>   # results carry valid addresses")
				}
				switch p.Collection {
				case address.CollectionCards, address.CollectionVault, address.CollectionArtifacts:
				default:
					return core.ErrUsage("wrong_collection",
						arg+" names a project or a board; a graph starts from a card, an entry or an artifact",
						"trellis card ls")
				}
				return withTarget(refArg{Collection: p.Collection, Value: arg, NoProject: true}, run(p.Collection))
			case address.ValidCardRef(strings.ToUpper(arg)) && keyNamesProject(core.ParseCardRef(arg).ProjectKey):
				// KEY-N is a card ref everywhere, but only once KEY actually
				// names a project (existing or merged): an entry slug that
				// merely looks like PREFIX-N, such as release-2026, is not a
				// card ref just because it matches the grammar, and falls
				// through to the relative lookup below, which tries an entry
				// first.
				return withTarget(refArg{Collection: address.CollectionCards, Value: arg}, run(address.CollectionCards))
			}
			relative := run("")
			return withBoard(func(app *appCtx) error { return relative(app, arg) })
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 2, "how many hops to walk")
	cmd.Flags().StringSliceVar(&rels, "rel", nil, "blocked_by, cites, wikilink")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "walk inbound: what breaks if this changes")
	return cmd
}

// resolveEntity finds where a walk starts. A collection says what the argument
// is; a relative argument of unknown kind tries an entry first, then a card.
func resolveEntity(cmd *cobra.Command, app *appCtx, collection, ref string) (string, error) {
	ctx := cmd.Context()
	switch collection {
	case address.CollectionVault:
		entry, err := app.Core.LoadEntry(ctx, app.Project.ID, ref)
		return entry.ID, err
	case address.CollectionCards:
		card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
		return card.ID, err
	case address.CollectionArtifacts:
		a, err := app.Core.ResolveArtifact(ctx, app.Project.ID, ref)
		return a.ID, err
	}
	if entry, err := app.Core.LoadEntry(ctx, app.Project.ID, ref); err == nil {
		return entry.ID, nil
	}
	card, err := app.Core.GetCard(ctx, app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return "", core.ErrNotFound("not_found", "no card or entry "+ref,
			"trellis card ls   # or: trellis vault ls")
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
