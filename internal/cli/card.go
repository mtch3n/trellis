package cli

import (
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
)

// cardID resolves any accepted reference — 12, XPSCTL-12 or the uuid — to the
// id the lease and comment calls take. Without this, only the uuid worked, which
// is the one form an agent never has to hand.
func cardID(cmd *cobra.Command, app *appCtx, ref string) (string, error) {
	card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
	if err != nil {
		return "", err
	}
	return card.ID, nil
}

// ptrOf converts a value to a pointer.
func ptrOf[T any](v T) *T { return &v }

// withBoard resolves the board context and ensures the database is closed.
func withBoard(fn func(*appCtx) error) error {
	app, err := currentBoard()
	if err != nil {
		return err
	}
	defer app.db.Close()
	return fn(app)
}

func newCardCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "card", Short: "Work with cards"}
	cmd.AddCommand(
		newCardNewCmd(), newCardShowCmd(), newCardLsCmd(), newCardMoveCmd(), newCardEditCmd(), newCardRmCmd(),
		newCardClaimCmd(), newCardReleaseCmd(), newCardRenewCmd(), newCardNextCmd(), newCardCommentCmd(),
		newCardArchiveCmd(), newCardBlockCmd(), newCardRelateCmd(), newCardImportCmd(), newCardHistoryCmd(), newCardDiffCmd())
	return cmd
}

func newCardNewCmd() *cobra.Command {
	var title, body TextValue
	var column, priority string
	var labels, tags []string

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a card",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !title.Changed() {
				return core.ErrUsage("missing_title", "a card needs a title",
					`trellis card new --title "..."`)
			}
			var prio *core.Priority
			if priority != "" {
				p, err := core.ParsePriority(priority)
				if err != nil {
					return err
				}
				prio = &p
			}
			return withBoard(func(app *appCtx) error {
				card, err := app.Core.CreateCard(cmd.Context(), app.Project.ID, app.Board.ID, core.NewCard{
					Title: title.String(), Body: body.String(), Column: column, Priority: prio,
					Labels: labels, Tags: tags,
				})
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string {
					return card.Ref + "  " + card.Title
				})
			})
		},
	}
	cmd.Flags().Var(&title, "title", "card title (text, - for stdin, or @file)")
	cmd.Flags().Var(&body, "body", "card body (text, - for stdin, or @file)")
	cmd.Flags().StringVar(&column, "column", "", "target column (default: the first)")
	cmd.Flags().StringVar(&priority, "priority", "", "urgent, high, normal or low")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label to add (can be used multiple times)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "tag to add (can be used multiple times)")
	return cmd
}

func newCardShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <card>",
		Short: "Show one card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				blockers, err := app.Core.Blockers(cmd.Context(), card.ID)
				if err != nil {
					return err
				}
				relations, err := app.Core.CardRelations(cmd.Context(), card.ID)
				if err != nil {
					return err
				}
				view := struct {
					core.Card
					BlockedBy []core.Blocker      `json:"blocked_by,omitempty"`
					Relations []core.CardRelation `json:"relations,omitempty"`
				}{Card: card, BlockedBy: blockers, Relations: relations}
				return Emit(cmd, view, func() string {
					head := card.Ref + "  [" + card.ColumnName + "/" + card.PriorityName + "]  " + card.Title
					if len(blockers) > 0 {
						head += "\nblocked by: " + blockerLine(blockers)
					}
					// Blockers have their own line above.
					others := slices.DeleteFunc(relations, func(r core.CardRelation) bool { return r.Rel == "blocked_by" })
					if len(others) > 0 {
						head += "\nrelations:" + renderRelations(others)
					}
					return head + "\n\n" + card.BodyMD
				})
			})
		},
	}
}

func newCardHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <card>",
		Short: "List a card's retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				revs, err := app.Core.ListCardRevisions(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"revisions": revs}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, r := range revs {
						fmt.Fprintf(w, "%d\t%s\t%s\n", r.Version, msDate(r.Timestamp), r.Actor)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newCardDiffCmd() *cobra.Command {
	var from, to int64
	cmd := &cobra.Command{
		Use:   "diff <card>",
		Short: "Show a unified diff between two retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				d, err := app.Core.DiffCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref), from, to)
				if err != nil {
					return err
				}
				return Emit(cmd, d, func() string { return d.Diff })
			})
		},
	}
	cmd.Flags().Int64Var(&from, "from", 0, "earlier version (default: the one before --to)")
	cmd.Flags().Int64Var(&to, "to", 0, "later version (default: the latest retained)")
	return cmd
}

func newCardLsCmd() *cobra.Command {
	var column, priority, label string
	var archived, all, allProjects bool
	var limit int

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List cards",
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := core.CardFilter{Column: column, IncludeArchived: archived, Label: label}
			if priority != "" {
				p, err := core.ParsePriority(priority)
				if err != nil {
					return err
				}
				f.Priority = &p
			}
			if all {
				f.Limit = -1
			} else if limit > 0 {
				f.Limit = limit
			}

			// --all-projects answers "what is in flight everywhere", which is
			// the question a session spanning several repositories asks. It
			// needs no project resolution, so it works outside a repository.
			if allProjects {
				c, db, err := openCore()
				if err != nil {
					return err
				}
				defer db.Close()
				page, err := c.ListCardsPage(cmd.Context(), core.CardScope{}, f)
				if err != nil {
					return err
				}
				return emitCardPage(cmd, page)
			}

			return withBoard(func(app *appCtx) error {
				if f.Limit == 0 {
					f.Limit = configInt(cmd.Context(), app, "card.ls_limit", core.DefaultCardLimit)
				}
				page, err := app.Core.ListCardsPage(cmd.Context(), core.CardScope{BoardID: app.Board.ID}, f)
				if err != nil {
					return err
				}
				return emitCardPage(cmd, page)
			})
		},
	}
	cmd.Flags().StringVar(&column, "column", "", "only cards in this column")
	cmd.Flags().StringVar(&priority, "priority", "", "only cards at this priority")
	cmd.Flags().StringVar(&label, "label", "", "only cards with this label")
	cmd.Flags().BoolVar(&archived, "archived", false, "include archived cards")
	cmd.Flags().BoolVar(&allProjects, "all-projects", false, "every project, not just this one")
	cmd.Flags().IntVar(&limit, "limit", 0, "row cap (default 50)")
	cmd.Flags().BoolVar(&all, "all", false, "no row cap")
	return cmd
}

// emitCardPage prints a listing. The row cap is carried in the JSON rather than
// applied silently: a truncated list an agent believes is complete is worse
// than a long one.
func emitCardPage(cmd *cobra.Command, page core.CardPage) error {
	return Emit(cmd, page, func() string {
		var b strings.Builder
		w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, c := range page.Cards {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Ref, c.ColumnName, c.PriorityName, c.Title)
		}
		w.Flush()
		out := strings.TrimRight(b.String(), "\n")
		if page.Truncated {
			out += fmt.Sprintf("\n... %d more of %d (--limit N or --all)",
				page.Total-len(page.Cards), page.Total)
		}
		return out
	})
}

func newCardMoveCmd() *cobra.Command {
	var targetColumn string
	cmd := &cobra.Command{
		Use:   "move <card> [column]",
		Short: "Move a card to another column",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				column := targetColumn
				if len(args) == 2 {
					column = args[1]
				}
				card, err := app.Core.GetCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref))
				if err != nil {
					return err
				}
				if column == "" {
					return core.ErrUsage("missing_column", "a destination column is required", "trellis card move "+args[0]+" --column <name>")
				}
				if card.BoardID != app.Board.ID && targetColumn == "" && len(args) != 2 {
					return core.ErrUsage("missing_column", "a destination column is required when crossing boards", "trellis card move "+args[0]+" --board <name> --column <name>")
				}
				card, err = app.Core.MoveCard(cmd.Context(), app.Project.ID, app.Board.ID,
					core.ParseCardRef(ref), column)
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string {
					return card.Ref + " -> " + card.ColumnName
				})
			})
		},
	}
	cmd.Flags().StringVar(&targetColumn, "column", "", "destination column (required across boards)")
	addActorFlag(cmd)
	return cmd
}

func newCardEditCmd() *cobra.Command {
	var title, body TextValue
	var priority string
	var ifVersion int64
	var addLabels, removeLabels, addTags, removeTags []string

	cmd := &cobra.Command{
		Use:   "edit <card>",
		Short: "Edit a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				var e core.CardEdit
				if title.Changed() {
					e.Title = ptrOf(title.String())
				}
				if body.Changed() {
					e.Body = ptrOf(body.String())
				}
				if priority != "" {
					p, err := core.ParsePriority(priority)
					if err != nil {
						return err
					}
					e.Priority = &p
				}
				if cmd.Flags().Changed("if-version") {
					e.IfVersion = &ifVersion
				}
				e.AddLabels = addLabels
				e.RemoveLabels = removeLabels
				e.AddTags = addTags
				e.RemoveTags = removeTags

				card, err := app.Core.EditCard(cmd.Context(), app.Project.ID,
					core.ParseCardRef(ref), e)
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string { return card.Ref + " updated" })
			})
		},
	}
	cmd.Flags().Var(&title, "title", "new title (text, - for stdin, or @file)")
	cmd.Flags().Var(&body, "body", "new body (text, - for stdin, or @file)")
	cmd.Flags().StringVar(&priority, "priority", "", "urgent, high, normal or low")
	cmd.Flags().Int64Var(&ifVersion, "if-version", 0, "version you read; required for --title and --body")
	cmd.Flags().StringSliceVar(&addLabels, "add-label", nil, "label to add (can be used multiple times)")
	cmd.Flags().StringSliceVar(&removeLabels, "rm-label", nil, "label to remove (can be used multiple times)")
	cmd.Flags().StringSliceVar(&addTags, "add-tag", nil, "tag to add (can be used multiple times)")
	cmd.Flags().StringSliceVar(&removeTags, "rm-tag", nil, "tag to remove (can be used multiple times)")
	addActorFlag(cmd)
	return cmd
}

func newCardRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <card>",
		Short: "Delete a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.DeleteCard(cmd.Context(), app.Project.ID, core.ParseCardRef(ref)); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"deleted": args[0]}, func() string {
					return args[0] + " deleted"
				})
			})
		},
	}
}

func newCardClaimCmd() *cobra.Command {
	var ttl int64
	var steal bool
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "claim <card>",
		Short: "Claim ownership of a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if steal && !reason.Changed() {
				// Stealing is legitimate when a holder has gone quiet, but the
				// displaced agent has to be able to find out what happened.
				return core.ErrUsage("missing_reason", "stealing a card records why",
					`trellis card claim `+args[0]+` --steal --reason "held 4h, no notes"`)
			}
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				id, err := cardID(cmd, app, ref)
				if err != nil {
					return err
				}
				card, err := app.Core.ClaimCard(cmd.Context(), id, ttl*60*1000, steal, reason.String())
				if err != nil {
					return err
				}
				return Emit(cmd, card, func() string {
					return card.Ref + " claimed (until " + fmt.Sprintf("%d", card.LeaseUntil) + ")"
				})
			})
		},
	}
	cmd.Flags().Int64Var(&ttl, "ttl", 0, "lease duration in minutes (default: config)")
	cmd.Flags().BoolVar(&steal, "steal", false, "take it from a quiet holder")
	cmd.Flags().Var(&reason, "reason", "why you stole it; the displaced agent sees this")
	addActorFlag(cmd)
	return cmd
}

func newCardReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release <card>",
		Short: "Release ownership of a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				id, err := cardID(cmd, app, ref)
				if err != nil {
					return err
				}
				if err := app.Core.ReleaseCard(cmd.Context(), id); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"released": args[0]}, func() string {
					return args[0] + " released"
				})
			})
		},
	}
	addActorFlag(cmd)
	return cmd
}

func newCardRenewCmd() *cobra.Command {
	var ttl int64
	cmd := &cobra.Command{
		// Editing a card already extends its lease (§8.4); renew is the
		// explicit escape hatch for holding one without changing it.
		Use:   "renew <card>",
		Short: "Extend the lease on a card you own",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				id, err := cardID(cmd, app, ref)
				if err != nil {
					return err
				}
				if err := app.Core.RenewLease(cmd.Context(), id, ttl*60*1000); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"renewed": args[0]}, func() string {
					return args[0] + " renewed"
				})
			})
		},
	}
	cmd.Flags().Int64Var(&ttl, "ttl", 0, "lease duration in minutes (default: config)")
	addActorFlag(cmd)
	return cmd
}

func newCardNextCmd() *cobra.Command {
	var claim bool
	var ttl int64
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Fetch the next unclaimed card",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				var claimedCard *core.Card
				var err error
				if claim {
					claimedCard, err = app.Core.ClaimNextCard(cmd.Context(), app.Board.ID, ttl*60*1000)
				} else {
					claimedCard, err = app.Core.GetNextCard(cmd.Context(), app.Board.ID)
				}
				if err != nil {
					return err
				}
				if claimedCard == nil {
					return Emit(cmd, map[string]any{"card": nil}, func() string {
						return "no work available"
					})
				}
				return Emit(cmd, claimedCard, func() string {
					return claimedCard.Ref + " " + claimedCard.Title
				})
			})
		},
	}
	cmd.Flags().BoolVar(&claim, "claim", false, "claim the card for this agent")
	cmd.Flags().Int64Var(&ttl, "ttl", 0, "lease duration in minutes (with --claim; default: config)")
	addActorFlag(cmd)
	return cmd
}

func newCardCommentCmd() *cobra.Command {
	var body TextValue
	cmd := &cobra.Command{
		Use:   "comment <card>",
		Short: "Append a comment to a card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !body.Changed() {
				return core.ErrUsage("missing_body", "a comment needs text",
					`trellis card comment <card> --body "..."`)
			}
			return withTarget(refArg{Collection: vpath.CollectionCards, Value: args[0]}, func(app *appCtx, ref string) error {
				id, err := cardID(cmd, app, ref)
				if err != nil {
					return err
				}
				comment, err := app.Core.CreateComment(cmd.Context(), id, body.String())
				if err != nil {
					return err
				}
				return Emit(cmd, comment, func() string {
					return args[0] + " commented"
				})
			})
		},
	}
	cmd.Flags().Var(&body, "body", "comment text (text, - for stdin, or @file)")
	addActorFlag(cmd)
	return cmd
}
