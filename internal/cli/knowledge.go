package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/vpath"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newKnowledgeCmd() *cobra.Command {
	// No "kb" alias: cobra spends three help lines listing aliases, and a
	// command earns those lines more than a nickname does.
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Work with knowledge entries",
	}
	cmd.AddCommand(
		newKnowledgeNewCmd(), newKnowledgeShowCmd(), newKnowledgeLsCmd(), newKnowledgeEditCmd(),
		newKnowledgeRmCmd(), newKnowledgeMvCmd(), newKnowledgePinCmd(), newKnowledgePinsCmd(), newKnowledgeLintCmd(),
		newKnowledgeNominateCmd(), newKnowledgeNominationsCmd(), newKnowledgeEscalateCmd(),
		newKnowledgeDemoteCmd(), newKnowledgeVerifyCmd(), newKnowledgeHealthCmd(),
		newKnowledgeUptakeCmd(), newKnowledgeTemplateCmd(),
		newKnowledgeHistoryCmd(), newKnowledgeDiffCmd())
	return cmd
}

func newKnowledgeNewCmd() *cobra.Command {
	var title, body, summary TextValue
	var template, board, provenance, dir string
	var tags, labels, setFlags, sources []string
	var private, newDir bool

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a knowledge entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !title.Changed() {
				return core.ErrUsage("missing_title", "a knowledge entry needs a title",
					`trellis knowledge new --title "Concurrency model"`)
			}
			fields, err := parseSetFlags(setFlags)
			if err != nil {
				return err
			}
			return withTargets([]refArg{{Collection: vpath.CollectionBoards, Value: board}}, func(app *appCtx, refs []string) error {
				doc, err := app.Core.CreateKnowledge(cmd.Context(), app.Project.ID, core.NewKnowledge{
					Title: title.String(), Body: body.String(), Template: template,
					Provenance: provenance,
					Summary:    summary.String(), Board: refs[0], Tags: tags, Labels: labels,
					Private: private, Set: fields, Sources: sources,
					Dir: dir, NewDir: newDir,
				})
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string {
					out := doc.Ref + "\n" + doc.Path
					for _, w := range doc.Warnings {
						out += "\nwarning: " + w
					}
					return out
				})
			})
		},
	}
	cmd.Flags().Var(&title, "title", "entry title")
	cmd.Flags().Var(&body, "body", "markdown body (default: simple header)")
	cmd.Flags().Var(&summary, "summary", "one line, used as the pinned recap when none is written")
	cmd.Flags().StringVar(&template, "template", "", strings.Join(core.Templates(), "|")+" (optional)")
	cmd.Flags().StringVar(&provenance, "provenance", "", "ingestion path: "+strings.Join(core.Provenances(), "|")+" (default authored)")
	cmd.Flags().StringVar(&board, "board", "", "associate with a board (association, never ownership)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "free-form tags")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "labels from the project vocabulary")
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "name=value, repeatable; supplies a field the template asks for")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"cite what a claim is based on: a URL, path:lines, card ref, wikilink or absolute address; repeatable")
	cmd.Flags().BoolVar(&private, "private", false,
		"do not transmit this body automatically: no vector index, no recap, no content in the event log, pointer-only injection")
	cmd.Flags().StringVar(&dir, "in", "", "place the entry in this directory instead of the vault root")
	cmd.Flags().BoolVar(&newDir, "new-dir", false, "create --in even if it resembles an existing directory")
	return cmd
}

func newKnowledgeShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Show one entry with its backlinks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				doc, err := app.Core.ReadKnowledge(cmd.Context(), app.Project.ID, ref)
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

// withholdContent strips what a listing must not carry. Listing is not
// reading: agents always receive the JSON form, so a body here would hand
// every entry in the vault to the model at once, and `knowledge show` is where
// a body is read. A private entry loses its summary and recap as well.
//
// docs must carry a Private flag refreshed from the file, as ListKnowledge and
// ColdKnowledge both provide. The mirror alone is one read stale after a hand
// edit, which is exactly when this matters.
func withholdContent(docs []core.Knowledge) {
	for i := range docs {
		docs[i].BodyMD = ""
		if docs[i].Private {
			docs[i].Summary, docs[i].Recap = "", nil
			docs[i].Fields = make(map[string]any)
		}
	}
}

func newKnowledgeHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <slug>",
		Short: "List an entry's retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				revs, err := app.Core.ListKnowledgeRevisions(cmd.Context(), app.Project.ID, args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"revisions": revs}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, r := range revs {
						fmt.Fprintf(w, "%d\t%s\n", r.Version, msDate(r.Timestamp))
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newKnowledgeDiffCmd() *cobra.Command {
	var from, to int64
	cmd := &cobra.Command{
		Use:   "diff <slug>",
		Short: "Show a unified diff between two retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				d, err := app.Core.DiffKnowledge(cmd.Context(), app.Project.ID, args[0], from, to)
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

// renderKnowledgeList is the text form of `knowledge ls`. It is a function
// rather than a closure so it can be tested directly: Emit selects JSON
// whenever stdout is captured.
// renderKnowledgeList is the text form of `knowledge ls`: a tree grouped by
// directory, since a knowledge slug may now be path-shaped. A root-level
// entry — the majority of any small vault — renders exactly as it always
// has; an entry under a directory gets a header line for that directory the
// first time it appears. JSON output (Emit's other branch) stays a flat
// array; a client can group it the same way from the slug.
func renderKnowledgeList(docs []core.Knowledge) string {
	sorted := slices.Clone(docs)
	slices.SortFunc(sorted, func(a, b core.Knowledge) int { return cmp.Compare(a.Slug, b.Slug) })
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	lastDir := ""
	for _, d := range sorted {
		dir, leaf := "", d.Slug
		if i := strings.LastIndex(d.Slug, "/"); i >= 0 {
			dir, leaf = d.Slug[:i], d.Slug[i+1:]
		}
		if dir != lastDir {
			if dir != "" {
				fmt.Fprintf(w, "%s/\n", dir)
			}
			lastDir = dir
		}
		indent := ""
		if dir != "" {
			indent = "  "
		}
		mark := ""
		if d.Missing {
			mark = "missing"
		} else if d.Private {
			mark = "private"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\n", indent, leaf, d.Template, d.Provenance, mark, d.Title)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func newKnowledgeLsCmd() *cobra.Command {
	var thisBoard, cold bool
	var templates, provenances, tags []string
	cmd := &cobra.Command{
		Use:   "ls [dir]",
		Short: "List entries, as a tree grouped by directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				filter := core.KnowledgeFilter{Templates: templates, Provenances: provenances, Tags: tags}
				if thisBoard {
					filter.BoardID = app.Board.ID
				}
				if len(args) == 1 {
					dir, err := core.SlugifyPath(args[0])
					if err != nil {
						return err
					}
					filter.Dir = dir
				}
				var docs []core.Knowledge
				var err error
				if cold {
					docs, err = app.Core.ColdKnowledge(cmd.Context(), app.Project.ID)
				} else {
					docs, err = app.Core.ListKnowledge(cmd.Context(), app.Project.ID, filter)
				}
				if err != nil {
					return err
				}
				withholdContent(docs)
				return Emit(cmd, map[string]any{"knowledge": docs}, func() string {
					return renderKnowledgeList(docs)
				})
			})
		},
	}
	cmd.Flags().BoolVar(&thisBoard, "board-only", false, "this board's entries plus the unscoped ones")
	cmd.Flags().BoolVar(&cold, "cold", false, "entries nothing has read in 30 days")
	cmd.Flags().StringSliceVar(&templates, "template", nil, "only these templates: "+strings.Join(core.Templates(), "|"))
	cmd.Flags().StringSliceVar(&provenances, "provenance", nil, "only these ingestion paths: "+strings.Join(core.Provenances(), "|"))
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "only entries with every one of these tags")
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
	var sources, tags, labels, setFlags []string
	var template, private string
	var ifVersion int64
	cmd := &cobra.Command{
		Use:   "edit <slug>",
		Short: "Replace an entry's body, sources, template, flags or fields",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setSources := cmd.Flags().Changed("source")
			setTags := cmd.Flags().Changed("tag")
			setLabels := cmd.Flags().Changed("label")
			setTemplate := cmd.Flags().Changed("template")
			setPrivate := cmd.Flags().Changed("private")
			fields, err := parseSetFlags(setFlags)
			if err != nil {
				return err
			}
			if !body.Changed() && !setSources && !setTags && !setLabels && !setTemplate && !setPrivate && len(fields) == 0 {
				return core.ErrUsage("missing_body",
					"name what to change: --body, --source, --template, --tag, --label, --private or --set",
					"trellis knowledge edit "+args[0]+" --body @notes.md")
			}
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				edit := core.KnowledgeEdit{Set: fields}
				if body.Changed() {
					b := body.String()
					edit.Body = &b
				}
				if setSources {
					edit.Sources = &sources
				}
				if setTags {
					// Filter out empty strings (e.g., from --tag="" to clear tags)
					filtered := make([]string, 0)
					for _, t := range tags {
						if t != "" {
							filtered = append(filtered, t)
						}
					}
					edit.Tags = &filtered
				}
				if setLabels {
					// Filter out empty strings (e.g., from --label="" to clear labels)
					filtered := make([]string, 0)
					for _, l := range labels {
						if l != "" {
							filtered = append(filtered, l)
						}
					}
					edit.Labels = &filtered
				}
				if setTemplate {
					edit.Template = &template
				}
				if setPrivate {
					p := private == "true"
					edit.Private = &p
				}
				if ifVersion > 0 {
					edit.IfVersion = &ifVersion
				}
				doc, err := app.Core.EditKnowledgeFields(cmd.Context(), app.Project.ID, ref, edit)
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string {
					out := "wrote " + doc.Path
					for _, w := range doc.Warnings {
						out += "\nwarning: " + w
					}
					return out
				})
			})
		},
	}
	cmd.Flags().Var(&body, "body", "new markdown body")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "replace the source list; repeatable")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "replace the tag list; repeatable")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "replace the label list; repeatable")
	cmd.Flags().StringVar(&template, "template", "", "change the template; \"\" for none")
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "name=value, repeatable; writes a field the template asks for (empty value removes it)")
	cmd.Flags().StringVar(&private, "private", "", "mark private (true|false)")
	cmd.Flags().Int64Var(&ifVersion, "if-version", 0, "the version you read; required (knowledge show --json)")
	return cmd
}

func newKnowledgeRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <slug>",
		Short: "Delete an entry and its file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.DeleteKnowledge(cmd.Context(), app.Project.ID, ref); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": args[0]},
					func() string { return "deleted " + args[0] })
			})
		},
	}
}

func newKnowledgeMvCmd() *cobra.Command {
	var newDir bool
	cmd := &cobra.Command{
		Use:   "mv <ref> <new-path>",
		Short: "Move or rename an entry within its project's vault",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				doc, err := app.Core.MoveKnowledge(cmd.Context(), app.Project.ID, args[0], args[1], newDir)
				if err != nil {
					return err
				}
				return Emit(cmd, doc, func() string { return "moved to " + doc.Slug })
			})
		},
	}
	cmd.Flags().BoolVar(&newDir, "new-dir", false, "create the destination directory even if it resembles an existing one")
	return cmd
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
			return withTargets([]refArg{
				{Collection: vpath.CollectionKnowledge, Value: args[0]},
				{Collection: vpath.CollectionBoards, Value: board},
			}, func(app *appCtx, refs []string) error {
				if remove {
					if err := app.Core.UnpinKnowledge(cmd.Context(), app.Project.ID, refs[0], refs[1]); err != nil {
						return err
					}
					return Emit(cmd, map[string]string{"unpinned": args[0]},
						func() string { return "unpinned " + args[0] })
				}
				pin, err := app.Core.PinKnowledge(cmd.Context(), app.Project.ID, refs[0], recap.String(), refs[1])
				if err != nil {
					return err
				}
				// A private entry is pinned as a pointer and has no recap.
				return Emit(cmd, pin, func() string { return "pinned " + pin.Slug + ": " + cmp.Or(pin.Recap, pin.Title) })
			})
		},
	}
	cmd.Flags().Var(&recap, "recap", "the summary to inject; you write it, trellis never generates one (discarded for a private entry)")
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
				pins, err := app.Core.Pins(cmd.Context(), app.Project.ID, app.Board.ID, 0)
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
		fmt.Fprintf(w, "%s\t%s%s\n", p.Slug, mark, cmp.Or(p.Recap, p.Title))
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
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.NominateKnowledge(cmd.Context(), app.Project.ID, ref, reason.String()); err != nil {
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
			return withTarget(refArg{Collection: vpath.CollectionKnowledge, Value: args[0]}, func(app *appCtx, ref string) error {
				doc, err := app.Core.EscalateKnowledge(cmd.Context(), app.Project.ID, ref, reason.String())
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

func newKnowledgeUptakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uptake",
		Short: "How often a recalled identifier was then opened, by ingestion path",
		Long: `How often a recalled identifier was then opened, by ingestion path.

Injected and never opened is noise, and it was paid for on cache write plus
every later read in that session. Injected and then opened is a hit. Only
recalls run with --record appear here.

This measures association, not causation, and says nothing about whether the
entry that was opened was any good.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				rows, err := app.Core.RecallUptake(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"uptake": rows}, func() string {
					if len(rows) == 0 {
						return "nothing recorded yet; recall with --record"
					}
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					fmt.Fprintf(w, "provenance\tinjected\topened\trate\n")
					for _, r := range rows {
						rate := "-"
						if r.Injected > 0 {
							rate = fmt.Sprintf("%d%%", r.Opened*100/r.Injected)
						}
						fmt.Fprintf(w, "%s\t%d\t%d\t%s\n",
							cmp.Or(r.Provenance, "(unrecorded)"), r.Injected, r.Opened, rate)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}
