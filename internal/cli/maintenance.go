package cli

import (
	"fmt"
	"strconv"
	"strings"
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
	var events, invocations, revisions, orphanHistory bool
	cmd := &cobra.Command{
		Use: "prune", Short: "Delete old event, invocation or revision history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !events && !invocations && !revisions && !orphanHistory {
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
				age, err := retentionDuration(retention)
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
			if orphanHistory {
				n, err := c.PruneOrphanHistory(cmd.Context())
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
	cmd.Flags().BoolVar(&orphanHistory, "orphan-history", false, "remove revision directories whose entry file is gone")
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

func retentionDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return 0, fmt.Errorf("retention is required")
	}
	if strings.HasSuffix(raw, "d") || strings.HasSuffix(raw, "w") {
		unit := time.Hour * 24
		if strings.HasSuffix(raw, "w") {
			unit *= 7
		}
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(raw, "d"), "w"), 64)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid retention %q", raw)
		}
		return time.Duration(n * float64(unit)), nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid retention %q", raw)
	}
	return d, nil
}
