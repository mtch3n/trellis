package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Create and list projects"}
	cmd.AddCommand(newProjectNewCmd(), newProjectLsCmd(), newProjectMergeCmd())
	return cmd
}

func newProjectNewCmd() *cobra.Command {
	var noPreset bool
	cmd := &cobra.Command{
		Use:   "new <KEY>",
		Short: "Create a project without marking any directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := c.CreateProject(cmd.Context(), args[0], !noPreset)
			if err != nil {
				return err
			}
			return Emit(cmd, p, func() string {
				return fmt.Sprintf("created project %s · mark a directory for it: trellis init --key %s", p.Key, p.Key)
			})
		},
	}
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}

func newProjectLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List every project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			projects, err := c.ListProjects(ctx)
			if err != nil {
				return err
			}
			type row struct {
				Key       string `json:"key"`
				Name      string `json:"name"`
				Boards    int    `json:"boards"`
				CreatedAt int64  `json:"created_at"`
			}
			rows := make([]row, len(projects))
			for i, p := range projects {
				boards, err := c.ListBoards(ctx, p.ID)
				if err != nil {
					return err
				}
				rows[i] = row{Key: p.Key, Name: p.Name, Boards: len(boards), CreatedAt: p.CreatedAt}
			}
			return Emit(cmd, map[string]any{"projects": rows}, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				for _, r := range rows {
					fmt.Fprintf(w, "%s\t%d boards\t%s\n", r.Key, r.Boards,
						time.UnixMilli(r.CreatedAt).UTC().Format(time.DateOnly))
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	}
}

func newProjectMergeCmd() *cobra.Command {
	var into string
	var apply, rename bool
	cmd := &cobra.Command{
		Use:   "merge <SRC> --into <DST>",
		Short: "Merge one project into another; prints the plan unless --apply",
		Long: "Move every board, card, entry and artifact of SRC into DST, keep card refs\n" +
			"such as SRC-12 working, and retire SRC's key. Without --apply the merge only\n" +
			"reports what it would do; with --apply it backs up first.\n\n" +
			"An entry or artifact that both projects name, with different content, stops\n" +
			"the merge; --rename-conflicts renames SRC's side instead. Markers that name\n" +
			"SRC under the enclosing repository are rewritten: commit them.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if into == "" {
				return core.ErrUsage("missing_into", "name the project to merge into",
					"trellis project merge "+args[0]+" --into <KEY>")
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := resolve.ScanRoot(dir)
			if err != nil {
				return err
			}
			markers, skipped, err := resolve.MarkersUnder(root)
			if err != nil {
				return err
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			// An apply hard-links a file into DST before dropping the SRC row;
			// an unhandled SIGINT would kill the process between the two with no
			// chance to run the transaction's rollback. This gives an interrupt a
			// context to cancel instead, which fails the transaction and lets
			// Core.Tx and stage.rollback run.
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			plan, err := c.MergeProjects(ctx, normalizeProjectArg(args[0]), normalizeProjectArg(into),
				core.MergeOptions{Apply: apply, RenameConflicts: rename, ScanRoot: root, Markers: markers, UnreadableMarkers: skipped})
			if err != nil {
				return err
			}
			return Emit(cmd, plan, func() string { return mergeTable(plan, apply) })
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "the project that receives everything (KEY or /KEY)")
	cmd.Flags().BoolVar(&apply, "apply", false, "perform the merge; without it only the plan is printed")
	cmd.Flags().BoolVar(&rename, "rename-conflicts", false, "rename SRC's side of a name conflict instead of stopping")
	return cmd
}

// mergeTable is the plan for a person: what moves, what stops the merge, and
// what the caller has to do next.
func mergeTable(p core.MergePlan, applied bool) string {
	var b strings.Builder
	verb := "would merge"
	if applied {
		verb = "merged"
	}
	fmt.Fprintf(&b, "%s %s into %s\n", verb, p.Src, p.Dst)
	if p.Refused != "" {
		fmt.Fprintf(&b, "refused: %s\n", p.Refused)
		return strings.TrimRight(b.String(), "\n")
	}
	for _, bm := range p.Boards {
		fmt.Fprintf(&b, "board   %s -> %s (/%s/boards/%s)\n", bm.Name, bm.NewName, p.Dst, bm.NewSlug)
	}
	fmt.Fprintf(&b, "cards   %d, numbered in %s from %d; refs unchanged\n", p.Cards.Moved, p.Dst, p.Cards.FirstSeq)
	for _, group := range []struct {
		name  string
		moves core.ItemMoves
	}{{"entries", p.Entries}, {"artifacts", p.Artifacts}} {
		fmt.Fprintf(&b, "%-9s %d moved, %d collapsed, %d renamed, %d in conflict\n", group.name,
			group.moves.Moved, len(group.moves.Collapsed), len(group.moves.Renamed), len(group.moves.Conflicts))
		for _, r := range group.moves.Renamed {
			fmt.Fprintf(&b, "  renamed   %s -> %s\n", r.From, r.To)
		}
		for _, conflict := range group.moves.Conflicts {
			note := ""
			if conflict.Vault {
				note = " (vault entry: never renamed)"
			}
			fmt.Fprintf(&b, "  conflict  %s%s\n", conflict.Name, note)
		}
	}
	fmt.Fprintf(&b, "labels  %d moved, %d folded; tags %d moved, %d folded\n",
		len(p.Labels.Moved), len(p.Labels.Folded), len(p.Tags.Moved), len(p.Tags.Folded))
	for _, d := range p.ConfigDropped {
		fmt.Fprintf(&b, "config  %s=%s dropped (%s keeps %q)\n", d.Key, d.Src, p.Dst, d.Dst)
	}
	for _, addr := range p.EntriesRewritten {
		fmt.Fprintf(&b, "rewrite %s\n", addr)
	}
	for _, r := range p.Markers.Rewrite {
		fmt.Fprintf(&b, "marker  %s: %s -> %s\n", r.Path, r.From, r.To)
	}
	for _, left := range p.Markers.Left {
		fmt.Fprintf(&b, "marker  left as is: %s\n", left)
	}
	fmt.Fprintf(&b, "markers searched under %s only\n", p.Markers.ScanRoot)
	if p.Backup != "" {
		fmt.Fprintf(&b, "backup  %s\n", p.Backup)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "warning %s\n", w)
	}
	switch {
	case !p.Ready:
		b.WriteString("not ready: resolve the conflicts, or rerun with --rename-conflicts\n")
	case applied && len(p.Markers.Rewrite) > 0:
		b.WriteString("commit the rewritten .trellis files\n")
	case !applied:
		b.WriteString("rerun with --apply to perform it\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
