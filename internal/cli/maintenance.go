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
	cmd := &cobra.Command{Use: "maintenance", Short: "Prune history and compact storage", Hidden: true}
	cmd.AddCommand(newMaintenancePruneCmd(), newMaintenanceCompactCmd())
	return cmd
}

func newMaintenancePruneCmd() *cobra.Command {
	var retention string
	var events, invocations bool
	cmd := &cobra.Command{
		Use: "prune", Short: "Delete old event or invocation history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			age, err := retentionDuration(retention)
			if err != nil {
				return core.ErrUsage("invalid_retention", err.Error(), "trellis maintenance prune --before 90d --events")
			}
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			n, err := c.PruneHistory(cmd.Context(), time.Now().Add(-age).UnixMilli(), events, invocations)
			if err != nil {
				return err
			}
			return Emit(cmd, map[string]any{"deleted": n, "before": time.Now().Add(-age).UnixMilli()}, func() string { return fmt.Sprintf("deleted %d historical rows", n) })
		},
	}
	cmd.Flags().StringVar(&retention, "before", "", "retention age, for example 90d or 12h")
	cmd.Flags().BoolVar(&events, "events", false, "prune event history")
	cmd.Flags().BoolVar(&invocations, "invocations", false, "prune invocation history")
	_ = cmd.MarkFlagRequired("before")
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
