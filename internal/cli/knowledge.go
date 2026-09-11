package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newKnowledgeCmd() *cobra.Command {
	// No "kb" alias: cobra spends three help lines listing aliases, and the
	// 25-line cap buys more with a command than with a nickname.
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Work with knowledge entries",
	}
	cmd.AddCommand(
		newKnowledgeNewCmd(), newKnowledgeShowCmd(), newKnowledgeLsCmd(), newKnowledgeEditCmd(),
		newKnowledgeRmCmd(), newKnowledgePinCmd(), newKnowledgePinsCmd(), newKnowledgeLintCmd(),
		newKnowledgeNominateCmd(), newKnowledgeNominationsCmd(), newKnowledgeEscalateCmd(),
		newKnowledgeDemoteCmd(), newKnowledgeVerifyCmd(), newKnowledgeHealthCmd())
	return cmd
}

func newKnowledgeNewCmd() *cobra.Command {
	var title, body, summary TextValue
	var template, board string
	var tags, labels []string

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a knowledge entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !title.Changed() {
				return core.ErrUsage("missing_title", "a knowledge entry needs a title",
					`trellis knowledge new --title "Concurrency model" --template decision`)
			}
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.CreateKnowledge(cmd.Context(), app.Project.ID, core.NewKnowledge{
					Title: title.String(), Body: body.String(), Template: template,
					Summary: summary.String(), Board: board, Tags: tags, Labels: labels,
				})
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return doc.Ref + "\n" + doc.Path })
			})
		},
	}
	cmd.Flags().Var(&title, "title", "entry title")
	cmd.Flags().Var(&body, "body", "markdown body (default: the template)")
	cmd.Flags().Var(&summary, "summary", "one line, used as the pinned recap when none is written")
	cmd.Flags().StringVar(&template, "template", "note", strings.Join(core.Templates(), "|"))
	cmd.Flags().StringVar(&board, "board", "", "associate with a board (association, never ownership)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "free-form tags")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "labels from the project vocabulary")
	return cmd
}

func newKnowledgeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Show one entry with its backlinks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.ReadKnowledge(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				back, err := app.Core.Backlinks(cmd.Context(), doc.ID)
				if err != nil {
					return err
				}
				view := struct {
					core.Knowledge
					Backlinks  []core.Backlink `json:"backlinks,omitempty"`
					Unreviewed bool            `json:"unreviewed,omitempty"`
				}{Knowledge: doc, Backlinks: back, Unreviewed: doc.Unreviewed(time.Now().UnixMilli())}
				return Emit(cmd, view, func() string {
					var b strings.Builder
					b.WriteString(doc.Ref)
					if view.Unreviewed {
						fmt.Fprintf(&b, "  (unreviewed since %s)", msDate(*doc.ReviewedAt))
					}
					b.WriteString("\n" + doc.Title + "\n\n" + doc.BodyMD)
					if len(back) > 0 {
						b.WriteString("\nBacklinks:\n")
						for _, l := range back {
							fmt.Fprintf(&b, "  %s  %s\n", l.Ref, l.Title)
						}
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newKnowledgeLsCmd() *cobra.Command {
	var thisBoard, cold bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				boardID := ""
				if thisBoard {
					boardID = app.Board.ID
				}
				docs, err := app.Core.ListKnowledge(cmd.Context(), app.Project.ID, boardID)
				if cold {
					docs, err = app.Core.ColdKnowledge(cmd.Context(), app.Project.ID)
				}
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"knowledge": docs}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, d := range docs {
						fmt.Fprintf(w, "%s\t%s\t%s\n", d.Slug, d.DocType, d.Title)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	cmd.Flags().BoolVar(&thisBoard, "board-only", false, "this board's entries plus the unscoped ones")
	cmd.Flags().BoolVar(&cold, "cold", false, "entries nothing has read in 30 days")
	return cmd
}

// newKnowledgeHealthCmd is the housekeeping report. Detection is free and runs
// when asked; every line names the command that acts on it, and nothing here
// changes anything (§10.10).
func newKnowledgeHealthCmd() *cobra.Command {
	var dupes bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Count what is worth tidying",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				if dupes {
					clusters, err := app.Core.Dupes(cmd.Context(), app.Project.ID)
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"clusters": clusters}, func() string {
						if len(clusters) == 0 {
							return "no duplicate clusters"
						}
						var b strings.Builder
						for _, c := range clusters {
							fmt.Fprintf(&b, "%s\n", strings.Join(c.Slugs, ", "))
						}
						return strings.TrimRight(b.String(), "\n")
					})
				}
				lines, err := app.Core.Health(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"health": lines}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, l := range lines {
						fmt.Fprintf(w, "%s\t%d\t%s\n", l.What, l.Count, l.Fix)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
	cmd.Flags().BoolVar(&dupes, "dupes", false, "list duplicate clusters instead")
	return cmd
}

func newKnowledgeEditCmd() *cobra.Command {
	var body TextValue
	var ifVersion int64
	cmd := &cobra.Command{
		Use:   "edit <slug>",
		Short: "Replace an entry's body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !body.Changed() {
				return core.ErrUsage("missing_body", "--body replaces the whole body",
					"trellis knowledge edit "+args[0]+" --body @notes.md")
			}
			return withBoard(func(app *appCtx) error {
				var v *int64
				if ifVersion > 0 {
					v = &ifVersion
				}
				doc, err := app.Core.EditKnowledge(cmd.Context(), app.Project.ID, args[0], body.String(), v)
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return "wrote " + doc.Path })
			})
		},
	}
	cmd.Flags().Var(&body, "body", "new markdown body")
	cmd.Flags().Int64Var(&ifVersion, "if-version", 0, "fail if the entry changed since you read it")
	return cmd
}

func newKnowledgeRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <slug>",
		Short: "Delete an entry and its file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if err := app.Core.DeleteKnowledge(cmd.Context(), app.Project.ID, args[0]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": args[0]},
					func() string { return "deleted " + args[0] })
			})
		},
	}
}

func newKnowledgePinCmd() *cobra.Command {
	var recap TextValue
	var board string
	var remove bool
	cmd := &cobra.Command{
		Use:   "pin <slug> [--recap ...] [--remove]",
		Short: "Pin an entry's recap into every session start",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if remove {
					if err := app.Core.UnpinKnowledge(cmd.Context(), app.Project.ID, args[0], board); err != nil {
						return err
					}
					return Emit(cmd, map[string]string{"unpinned": args[0]},
						func() string { return "unpinned " + args[0] })
				}
				pin, err := app.Core.PinKnowledge(cmd.Context(), app.Project.ID, args[0], recap.String(), board)
				if err != nil {
					return err
				}
				return Emit(cmd, pin, func() string { return "pinned " + pin.Slug + ": " + pin.Recap })
			})
		},
	}
	cmd.Flags().Var(&recap, "recap", "the summary to inject; you write it, trellis never generates one")
	cmd.Flags().StringVar(&board, "board", "", "pin to one board (default: project-wide)")
	cmd.Flags().BoolVar(&remove, "remove", false, "unpin instead")
	return cmd
}

func newKnowledgePinsCmd() *cobra.Command {
	var stale bool
	cmd := &cobra.Command{
		Use:   "pins",
		Short: "List what session start injects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				pins, err := app.Core.Pins(cmd.Context(), app.Project.ID, app.Board.ID)
				if err != nil {
					return err
				}
				if stale {
					kept := pins[:0]
					for _, p := range pins {
						if p.Stale {
							kept = append(kept, p)
						}
					}
					pins = kept
				}
				return Emit(cmd, map[string]any{"pins": pins}, func() string { return pinTable(pins) })
			})
		},
	}
	cmd.Flags().BoolVar(&stale, "stale", false, "only recaps whose entry has changed since")
	return cmd
}

func pinTable(pins []core.Pin) string {
	if len(pins) == 0 {
		return "(nothing pinned)"
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, p := range pins {
		mark := ""
		if p.Stale {
			mark = "(stale) "
		}
		fmt.Fprintf(w, "%s\t%s%s\n", p.Slug, mark, p.Recap)
	}
	w.Flush()
	if len(pins) > core.MaxInjectedPins {
		fmt.Fprintf(&b, "%d pinned, %d injected: the rest are listed as titles only\n",
			len(pins), core.MaxInjectedPins)
	}
	return strings.TrimRight(b.String(), "\n")
}

func newKnowledgeLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Report stubs, broken anchors and orphans",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				findings, err := app.Core.Lint(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				// The documented exception to the three-line error rule: a
				// validation report is a list of findings (§12).
				return Emit(cmd, map[string]any{"findings": findings}, func() string {
					if len(findings) == 0 {
						return "no findings"
					}
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, f := range findings {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Kind, f.Doc, f.Ref, f.Fix)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newKnowledgeNominateCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "nominate <slug>",
		Short: "Propose an entry for the global vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if err := app.Core.NominateKnowledge(cmd.Context(), app.Project.ID, args[0], reason.String()); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"nominated": args[0]},
					func() string { return "nominated " + args[0] })
			})
		},
	}
	cmd.Flags().Var(&reason, "reason", "what you needed it for and could not get")
	return cmd
}

func newKnowledgeNominationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nominations",
		Short: "The escalation queue, with its evidence",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				noms, err := app.Core.Nominations(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"nominations": noms}, func() string {
					if len(noms) == 0 {
						return "(no nominations)"
					}
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					fmt.Fprintln(w, "SLUG\tCITED\tPINNED\tREADS/30d\tACTORS\tNOMS\tREASON")
					for _, n := range noms {
						fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%q — %s\n",
							n.Slug, n.Cited, n.Pinned, n.Reads, n.Actors, n.Noms, n.Reason, n.Actor)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newKnowledgeEscalateCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "escalate <slug>",
		Short: "Move an entry to the global vault (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireHuman(args[0]); err != nil {
				return err
			}
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.EscalateKnowledge(cmd.Context(), app.Project.ID, args[0], reason.String())
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return "escalated to " + doc.Ref })
			})
		},
	}
	cmd.Flags().Var(&reason, "reason", "why this belongs beyond its project")
	return cmd
}

func newKnowledgeDemoteCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "demote <slug>",
		Short: "Return a global entry to its project (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireHuman(args[0]); err != nil {
				return err
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			doc, err := c.DemoteKnowledge(cmd.Context(), args[0], reason.String())
			if err != nil {
				return err
			}
			return Emit(cmd, doc, func() string { return "demoted to " + doc.Ref })
		},
	}
	cmd.Flags().Var(&reason, "reason", "why it does not belong globally")
	return cmd
}

func newKnowledgeVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <slug>",
		Short: "Reset the review clock on a global entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireHuman(args[0]); err != nil {
				return err
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := c.VerifyKnowledge(cmd.Context(), args[0]); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"verified": args[0]},
				func() string { return "verified " + args[0] })
		},
	}
}

// requireHuman gates the three commands that write to the shared system of
// record. It asks for evidence of a human rather than evidence of an agent:
// stdin must be a terminal, TRELLIS_AGENT must be unset, and the operator has
// to retype the slug. There is deliberately no --force, --yes or --i-am-human,
// because an override flag is precisely what an agent reaches for when stuck
// (§10.8). A human without a TTY uses trellis ui.
//
// This is a guardrail, not a security boundary, and the actor is recorded
// either way — an escalation that somehow came from an agent is visible in the
// event log and can be demoted.
func requireHuman(slug string) error {
	if os.Getenv("TRELLIS_AGENT") != "" {
		return core.ErrPolicy("agent_refused",
			"agents nominate; humans escalate",
			"trellis knowledge nominate "+slug+` --reason "..."`)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return core.ErrPolicy("no_tty",
			"this needs an interactive terminal, or trellis ui",
			"trellis ui   # then escalate from the browser")
	}
	fmt.Fprintf(os.Stderr, "Retype the slug to confirm (%s): ", slug)
	var typed string
	if _, err := fmt.Fscanln(os.Stdin, &typed); err != nil || typed != slug {
		return core.ErrPolicy("not_confirmed", "the slug did not match; nothing changed", "")
	}
	return nil
}

func msDate(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02") }
