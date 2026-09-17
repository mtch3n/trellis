package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newVaultCmd() *cobra.Command {
	// No alias: cobra spends three help lines listing aliases, and a command
	// earns those lines more than a nickname does.
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Work with entries in the project and global vaults",
		Long: "An <entry> argument is a slug in this project's vault, or an address:\n" +
			"/KEY/vault/<slug> or /GLOBAL/vault/<slug>.",
	}
	cmd.AddCommand(
		newVaultNewCmd(), newVaultShowCmd(), newVaultLsCmd(), newVaultEditCmd(),
		newVaultRmCmd(), newVaultMvCmd(), newVaultPinCmd(), newVaultPinsCmd(), newVaultLintCmd(),
		newVaultNominateCmd(), newVaultNominationsCmd(), newVaultPromoteCmd(),
		newVaultDemoteCmd(), newVaultVerifyCmd(), newVaultHealthCmd(),
		newVaultUptakeCmd(), newVaultTemplateCmd(),
		newVaultHistoryCmd(), newVaultDiffCmd())
	return cmd
}

func newVaultNewCmd() *cobra.Command {
	var title, body, summary TextValue
	var template, board, provenance, dir string
	var tags, labels, setFlags, sources []string
	var private, newDir bool

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create an entry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !title.Changed() {
				return core.ErrUsage("missing_title", "an entry needs a title",
					`trellis vault new --title "Concurrency model"`)
			}
			fields, err := parseSetFlags(setFlags)
			if err != nil {
				return err
			}
			return withTargets([]refArg{{Collection: address.CollectionBoards, Value: board}}, func(app *appCtx, refs []string) error {
				entry, err := app.Core.CreateEntry(cmd.Context(), app.Project.ID, core.NewEntry{
					Title: title.String(), Body: body.String(), Template: template,
					Provenance: provenance,
					Summary:    summary.String(), Board: refs[0], Tags: tags, Labels: labels,
					Private: private, Set: fields, Sources: sources,
					Dir: dir, NewDir: newDir,
				})
				if err != nil {
					return err
				}
				return Emit(cmd, entry, func() string {
					out := entry.Ref + "\n" + entry.Path
					for _, w := range entry.Warnings {
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
	cmd.Flags().StringVar(&provenance, "provenance", "", "how the entry was ingested: "+strings.Join(core.Provenances(), "|")+" (default authored)")
	cmd.Flags().StringVar(&board, "board", "", "associate with a board (association, never a claim)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "free-form tags")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "labels defined in this project")
	cmd.Flags().StringArrayVar(&setFlags, "set", nil, "name=value, repeatable; supplies a field the template asks for")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"evidence for what the entry says: a URL, path:lines, card ref, wikilink or absolute address; repeatable")
	cmd.Flags().BoolVar(&private, "private", false,
		"do not transmit this body automatically: no vector index, no recap, no body in the event log, pointer-only injection")
	cmd.Flags().StringVar(&dir, "in", "", "place the entry in this directory instead of the vault root")
	cmd.Flags().BoolVar(&newDir, "new-directory", false, "create --in even if it resembles an existing directory")
	return cmd
}

func newVaultShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <entry>",
		Short: "Show one entry with its backlinks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				entry, err := app.Core.ReadEntry(cmd.Context(), app.Project.ID, ref)
				if err != nil {
					return err
				}
				back, err := app.Core.Backlinks(cmd.Context(), entry.ID)
				if err != nil {
					return err
				}
				view := struct {
					core.Entry
					Backlinks  []core.Backlink `json:"backlinks,omitempty"`
					Unverified bool            `json:"unverified,omitzero"`
				}{Entry: entry, Backlinks: back, Unverified: entry.Unverified(time.Now().UnixMilli())}
				return Emit(cmd, view, func() string {
					var b strings.Builder
					b.WriteString(entry.Ref)
					if view.Unverified {
						fmt.Fprintf(&b, "  (unverified since %s)", msDate(*entry.VerifiedAt))
					}
					b.WriteString("\n" + entry.Title + "\n\n" + entry.BodyMD)
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
// every entry in the vault to the model at once, and `vault show` is where
// a body is read. A private entry loses its summary and recap as well.
//
// entries must carry a Private flag refreshed from the file, as ListEntries and
// ColdEntries both provide. The mirror alone is one read stale after a hand
// edit, which is exactly when this matters.
func withholdContent(entries []core.Entry) {
	for i := range entries {
		entries[i].BodyMD = ""
		if entries[i].Private {
			entries[i].Summary, entries[i].Recap = "", nil
			entries[i].Fields = make(map[string]any)
		}
	}
}

func newVaultHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <entry>",
		Short: "List an entry's retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				revs, err := app.Core.ListEntryRevisions(cmd.Context(), app.Project.ID, ref)
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

func newVaultDiffCmd() *cobra.Command {
	var from, to int64
	cmd := &cobra.Command{
		Use:   "diff <entry>",
		Short: "Show a unified diff between two retained revisions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				d, err := app.Core.DiffEntry(cmd.Context(), app.Project.ID, ref, from, to)
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

// renderEntryList is the text form of `vault ls`. It is a function
// rather than a closure so it can be tested directly: Emit selects JSON
// whenever stdout is captured.
// renderEntryList is the text form of `vault ls`: a tree grouped by
// directory, since an entry slug may now be path-shaped. A root-level
// entry — the majority of any small vault — renders exactly as it always
// has; an entry under a directory gets a header line for that directory the
// first time it appears. JSON output (Emit's other branch) stays a flat
// array; a client can group it the same way from the slug.
func renderEntryList(entries []core.Entry) string {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b core.Entry) int { return cmp.Compare(a.Slug, b.Slug) })
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	lastDir := ""
	for _, e := range sorted {
		dir, leaf := "", e.Slug
		if i := strings.LastIndex(e.Slug, "/"); i >= 0 {
			dir, leaf = e.Slug[:i], e.Slug[i+1:]
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
		if e.Missing {
			mark = "missing"
		} else if e.Private {
			mark = "private"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\n", indent, leaf, e.Template, e.Provenance, mark, e.Title)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

func newVaultLsCmd() *cobra.Command {
	var thisBoard, cold bool
	var templates, provenances, tags []string
	cmd := &cobra.Command{
		Use:   "ls [directory]",
		Short: "List entries, as a tree grouped by directory",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				filter := core.EntryFilter{Templates: templates, Provenances: provenances, Tags: tags}
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
				var entries []core.Entry
				var err error
				if cold {
					entries, err = app.Core.ColdEntries(cmd.Context(), app.Project.ID)
				} else {
					entries, err = app.Core.ListEntries(cmd.Context(), app.Project.ID, filter)
				}
				if err != nil {
					return err
				}
				withholdContent(entries)
				return Emit(cmd, map[string]any{"entries": entries}, func() string {
					return renderEntryList(entries)
				})
			})
		},
	}
	cmd.Flags().BoolVar(&thisBoard, "board-only", false, "this board's entries plus the unscoped ones")
	cmd.Flags().BoolVar(&cold, "cold", false, "entries nothing has read in 30 days")
	cmd.Flags().StringSliceVar(&templates, "template", nil, "only these templates: "+strings.Join(core.Templates(), "|"))
	cmd.Flags().StringSliceVar(&provenances, "provenance", nil, "only entries with these provenances: "+strings.Join(core.Provenances(), "|"))
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "only entries with every one of these tags")
	return cmd
}

// newVaultHealthCmd is the housekeeping report. Detection is free and runs
// when asked; every line names the command that acts on it, and nothing here
// changes anything (§10.10).
func newVaultHealthCmd() *cobra.Command {
	var duplicates bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Count what is worth tidying",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				if duplicates {
					clusters, err := app.Core.Duplicates(cmd.Context(), app.Project.ID)
					if err != nil {
						return err
					}
					return Emit(cmd, map[string]any{"duplicate_clusters": clusters}, func() string {
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
	cmd.Flags().BoolVar(&duplicates, "duplicates", false, "list duplicate clusters instead")
	return cmd
}

func newVaultEditCmd() *cobra.Command {
	var body TextValue
	var sources, tags, labels, setFlags []string
	var template, private, board string
	var ifVersion int64
	cmd := &cobra.Command{
		Use:   "edit <entry>",
		Short: "Replace an entry's body, sources, template, flags or fields",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setSources := cmd.Flags().Changed("source")
			setTags := cmd.Flags().Changed("tag")
			setLabels := cmd.Flags().Changed("label")
			setTemplate := cmd.Flags().Changed("template")
			setPrivate := cmd.Flags().Changed("private")
			setBoard := cmd.Flags().Changed("board")
			fields, err := parseSetFlags(setFlags)
			if err != nil {
				return err
			}
			if !body.Changed() && !setSources && !setTags && !setLabels && !setTemplate && !setPrivate && !setBoard && len(fields) == 0 {
				return core.ErrUsage("missing_body",
					"name what to change: --body, --source, --template, --tag, --label, --private, --board or --set",
					"trellis vault edit "+args[0]+" --body @notes.md")
			}
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0], NoProject: true}, func(app *appCtx, ref string) error {
				edit := core.EntryEdit{Set: fields}
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
					p, perr := strconv.ParseBool(private)
					if perr != nil {
						return core.ErrUsage("invalid_value",
							fmt.Sprintf("--private: %q is not true or false", private),
							"trellis vault edit "+args[0]+" --private true")
					}
					edit.Private = &p
				}
				if setBoard {
					edit.Board = &board
				}
				if ifVersion > 0 {
					edit.IfVersion = &ifVersion
				}
				entry, err := app.Core.EditEntryFields(cmd.Context(), app.Project.ID, ref, edit)
				if err != nil {
					return err
				}
				return Emit(cmd, entry, func() string {
					out := "wrote " + entry.Path
					for _, w := range entry.Warnings {
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
	cmd.Flags().StringVar(&board, "board", "", `associate the entry with a board; "" for none`)
	cmd.Flags().Int64Var(&ifVersion, "if-version", 0, "the version you read; required (vault show --json)")
	return cmd
}

func newVaultRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <entry>",
		Short: "Delete an entry and its file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.DeleteEntry(cmd.Context(), app.Project.ID, ref); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": args[0]},
					func() string { return "deleted " + args[0] })
			})
		},
	}
}

func newVaultMvCmd() *cobra.Command {
	var newDir bool
	cmd := &cobra.Command{
		Use:   "mv <entry> <new-path>",
		Short: "Move or rename an entry within its project's vault",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0]}, func(app *appCtx, ref string) error {
				slug, err := entrySlugArg(ref)
				if err != nil {
					return err
				}
				entry, err := app.Core.MoveEntry(cmd.Context(), app.Project.ID, slug, args[1], newDir)
				if err != nil {
					return err
				}
				return Emit(cmd, entry, func() string { return "moved to " + entry.Slug })
			})
		},
	}
	cmd.Flags().BoolVar(&newDir, "new-directory", false, "create the destination directory even if it resembles an existing one")
	return cmd
}

// entrySlugArg reduces an entry reference to the bare slug
// MoveEntry takes: unlike LoadEntry, it resolves a slug directly and
// does not parse an address itself. withTarget has already decided which
// project an address names, so only the address's own name segment is still
// needed here.
func entrySlugArg(ref string) (string, error) {
	v := strings.TrimSpace(ref)
	if !strings.HasPrefix(v, "/") {
		return v, nil
	}
	p, err := core.ParseAddress(v, address.CollectionVault)
	if err != nil {
		return "", err
	}
	return p.Name, nil
}

func newVaultPinCmd() *cobra.Command {
	var recap TextValue
	var board string
	var remove bool
	cmd := &cobra.Command{
		Use:   "pin <entry> [--recap ...] [--remove]",
		Short: "Pin an entry, injecting its recap into the session brief",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTargets([]refArg{
				{Collection: address.CollectionVault, Value: args[0]},
				{Collection: address.CollectionBoards, Value: board},
			}, func(app *appCtx, refs []string) error {
				if remove {
					if err := app.Core.UnpinEntry(cmd.Context(), app.Project.ID, refs[0], refs[1]); err != nil {
						return err
					}
					return Emit(cmd, map[string]string{"unpinned": args[0]},
						func() string { return "unpinned " + args[0] })
				}
				pin, err := app.Core.PinEntry(cmd.Context(), app.Project.ID, refs[0], recap.String(), refs[1])
				if err != nil {
					return err
				}
				// A private entry is pinned as a pointer and has no recap.
				return Emit(cmd, pin, func() string { return "pinned " + pin.Slug + ": " + cmp.Or(pin.Recap, pin.Title) })
			})
		},
	}
	cmd.Flags().Var(&recap, "recap", "text to inject, defaulting to the entry's summary; you write it, trellis never generates one (discarded for a private entry)")
	cmd.Flags().StringVar(&board, "board", "", "pin to one board (default: project-wide)")
	cmd.Flags().BoolVar(&remove, "remove", false, "unpin instead")
	return cmd
}

func newVaultPinsCmd() *cobra.Command {
	var stale bool
	cmd := &cobra.Command{
		Use:   "pins",
		Short: "List pinned entries",
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

func newVaultLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Report diagnostics in this project's vault",
		Long: `Report diagnostics in this project's vault. Each one names its fix; lint
never changes anything.

Kinds:
  ambiguous_link       a wikilink whose bare leaf names more than one entry
  bad_address          a malformed address
  broken_anchor        a wikilink to a heading its target entry does not have
  deep_directory       an entry three or more directories deep
  long_directory_name  a directory name longer than 30 characters
  missing_artifact     an artifact an entry names that does not exist
  orphan               an entry with no links in either direction
  similar_directory    two look-alike directories
  stub                 a wikilink whose target does not resolve
  template_violation   an entry that breaks its template's rules
  unknown_field        a frontmatter field that no template names
  unknown_template     an entry whose template names no template on disk
  wrong_collection     a wikilink address outside any vault`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				diagnostics, err := app.Core.Lint(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				// The documented exception to the three-line error rule: a
				// validation report is a list of diagnostics (§12).
				return Emit(cmd, map[string]any{"diagnostics": diagnostics}, func() string {
					if len(diagnostics) == 0 {
						return "no diagnostics"
					}
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, f := range diagnostics {
						fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Kind, f.Entry, f.Ref, f.Fix)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newVaultNominateCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "nominate <entry>",
		Short: "Nominate an entry for the global vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0]}, func(app *appCtx, ref string) error {
				if err := app.Core.NominateEntry(cmd.Context(), app.Project.ID, ref, reason.String()); err != nil {
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

func newVaultNominationsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nominations",
		Short: "List nominees, ranked by evidence",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				nominees, err := app.Core.Nominations(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"nominations": nominees}, func() string {
					if len(nominees) == 0 {
						return "(no nominations)"
					}
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					fmt.Fprintln(w, "SLUG\tCITED\tPINNED\tREADS/30d\tACTORS\tNOMINATIONS\tREASON")
					for _, n := range nominees {
						fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%q — %s\n",
							n.Slug, n.Cited, n.Pinned, n.Reads, n.Actors, n.Nominations, n.Reason, n.Actor)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newVaultPromoteCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "promote <entry>",
		Short: "Move an entry to the global vault (human only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireHuman(args[0]); err != nil {
				return err
			}
			return withTarget(refArg{Collection: address.CollectionVault, Value: args[0]}, func(app *appCtx, ref string) error {
				entry, err := app.Core.PromoteEntry(cmd.Context(), app.Project.ID, ref, reason.String())
				if err != nil {
					return err
				}
				return Emit(cmd, entry, func() string { return "promoted to " + entry.Ref })
			})
		},
	}
	cmd.Flags().Var(&reason, "reason", "why this belongs beyond its project")
	return cmd
}

func newVaultDemoteCmd() *cobra.Command {
	var reason TextValue
	cmd := &cobra.Command{
		Use:   "demote <entry>",
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
			entry, err := c.DemoteEntry(cmd.Context(), args[0], reason.String())
			if err != nil {
				return err
			}
			return Emit(cmd, entry, func() string { return "demoted to " + entry.Ref })
		},
	}
	cmd.Flags().Var(&reason, "reason", "why it does not belong globally")
	return cmd
}

func newVaultVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <entry>",
		Short: "Confirm a global entry still holds, resetting verify_by (human only)",
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
			if err := c.VerifyEntry(cmd.Context(), args[0]); err != nil {
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
// either way — a promotion that somehow came from an agent is visible in the
// event log and can be demoted.
func requireHuman(slug string) error {
	if os.Getenv("TRELLIS_AGENT") != "" {
		return core.ErrPolicy("agent_refused",
			"agents nominate; humans promote",
			"trellis vault nominate "+slug+` --reason "..."`)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return core.ErrPolicy("no_tty",
			"this needs an interactive terminal, or trellis ui",
			"trellis ui")
	}
	fmt.Fprintf(os.Stderr, "Retype the slug to confirm (%s): ", slug)
	var typed string
	if _, err := fmt.Fscanln(os.Stdin, &typed); err != nil || typed != slug {
		return core.ErrPolicy("not_confirmed", "the slug did not match; nothing changed", "")
	}
	return nil
}

func msDate(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02") }

func newVaultUptakeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uptake",
		Short: "How often a recalled ref was then opened, by provenance",
		Long: `How often a recalled ref was then opened, by provenance.

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
