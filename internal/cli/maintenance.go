package cli

import (
	"fmt"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newMaintenanceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "maintenance", Short: "Prune history and compact storage"}
	cmd.AddCommand(newMaintenancePruneCmd(), newMaintenanceCompactCmd())
	return cmd
}

func newMaintenancePruneCmd() *cobra.Command {
	var retention string
	var events, invocations, revisions, leftoverRevisions bool
	cmd := &cobra.Command{
		Use: "prune", Short: "Delete old event, invocation or revision history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !events && !invocations && !revisions && !leftoverRevisions {
				return core.ErrUsage("nothing_to_prune",
					"select --events, --invocations, --revisions and/or --orphan-history",
					"trellis maintenance prune --before 90d --events")
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()

			var total int64
			var before int64
			if events || invocations {
				age, err := core.ParseRetention(retention)
				if err != nil {
					return core.ErrUsage("invalid_retention", err.Error(), "trellis maintenance prune --before 90d --events")
				}
				before = time.Now().Add(-age).UnixMilli()
				n, err := c.PruneHistory(cmd.Context(), before, events, invocations)
				if err != nil {
					return err
				}
				total += n
			}
			if revisions {
				n, err := c.PruneRevisions(cmd.Context())
				if err != nil {
					return err
				}
				total += n
			}
			if leftoverRevisions {
				n, err := c.PruneLeftoverRevisions(cmd.Context())
				if err != nil {
					return err
				}
				total += n
			}
			return Emit(cmd, map[string]any{"deleted": total, "before": before},
				func() string { return fmt.Sprintf("deleted %d historical rows", total) })
		},
	}
	cmd.Flags().StringVar(&retention, "before", "", "retention age, for example 90d or 12h (required with --events or --invocations)")
	cmd.Flags().BoolVar(&events, "events", false, "prune event history")
	cmd.Flags().BoolVar(&invocations, "invocations", false, "prune invocation history")
	cmd.Flags().BoolVar(&revisions, "revisions", false, "trim every entry's and card's revisions to history.keep")
	cmd.Flags().BoolVar(&leftoverRevisions, "orphan-history", false, "remove revision directories whose entry file is gone")
	return cmd
}

func newMaintenanceCompactCmd() *cobra.Command {
	return &cobra.Command{
		Use: "compact", Short: "Checkpoint WAL files and VACUUM the main database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := c.Compact(cmd.Context()); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"status": "compacted"}, func() string { return "main database compacted" })
		},
	}
}
