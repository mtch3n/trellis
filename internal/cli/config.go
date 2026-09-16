package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

// projectContext holds the current project info needed for config operations.
type projectContext struct {
	Core    *core.Core
	Project core.Project
	db      *sqlx.DB
	cfg     config.Config
}

// currentProject resolves the current project without requiring a board.
func currentProject() (*projectContext, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}

	r, err := resolveProject(context.Background(), c)
	if err != nil {
		db.Close()
		return nil, err
	}
	p := r.Project

	cfg, err := config.Load()
	if err != nil {
		// Log but don't fail: config file issues are warnings, not hard stops.
		// Fall back to defaults.
		cfg = config.Defaults()
	}

	return &projectContext{
		Core:    c,
		Project: p,
		db:      db,
		cfg:     cfg,
	}, nil
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage settings",
	}
	cmd.AddCommand(
		newConfigGetCmd(),
		newConfigSetCmd(),
		newConfigUnsetCmd(),
		newConfigLsCmd(),
	)
	return cmd
}

func newConfigUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "unset <key>", Short: "Remove a project override", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pctx, err := currentProject()
			if err != nil {
				return err
			}
			defer pctx.db.Close()
			if _, ok := config.GetValue(pctx.cfg, args[0]); !ok {
				return core.ErrUsage("unknown_key", fmt.Sprintf("unknown config key: %q", args[0]), "trellis config ls")
			}
			if err := config.UnsetProjectConfig(cmd.Context(), pctx.db, pctx.Project.ID, args[0]); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"unset": args[0]}, func() string { return "unset " + args[0] })
		},
	}
}

func newConfigGetCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			// Load global config.
			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}

			// A project override always wins where one resolves; outside a
			// repository there is nothing to override with, so the global
			// default is the answer. The source field says which happened, so
			// no flag is needed to ask.
			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()
				value, source, err := config.EffectiveValue(cmd.Context(), globalCfg, pctx.db, pctx.Project.ID, key)
				if err != nil {
					return core.ErrUsage("unknown_key", err.Error(), "trellis config ls")
				}

				return Emit(cmd, map[string]string{
					"key":    key,
					"value":  value,
					"source": source,
				}, func() string {
					return fmt.Sprintf("%s = %s  (%s)", key, value, source)
				})
			}

			// Global scope: just get the default value.
			value, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}

			return Emit(cmd, map[string]string{
				"key":    key,
				"value":  value,
				"source": "default",
			}, func() string {
				return fmt.Sprintf("%s = %s", key, value)
			})
		},
	}

	return cmd
}

func newConfigSetCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			// config set only ever writes a project override: the global file
			// is hand-edited YAML (§5.4), so there is no scope to choose.

			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}

			_, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}

			pctx, err := currentProject()
			if err != nil {
				return err
			}
			defer pctx.db.Close()

			if err := config.SetProjectConfig(cmd.Context(), pctx.db, pctx.Project.ID, key, value); err != nil {
				return err
			}

			return Emit(cmd, map[string]string{
				"key":   key,
				"value": value,
				"scope": "project",
			}, func() string {
				return fmt.Sprintf("%s = %s (project override)", key, value)
			})
		},
	}

	return cmd
}

// configRow represents a single config key-value-source triple.
type configRow struct {
	Key    string
	Value  string
	Source string
}

func newConfigLsCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all config values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			globalCfg, err := config.Load()
			if err != nil {
				globalCfg = config.Defaults()
			}

			// All known keys.
			allKeys := []string{
				"ui.port", "ui.bind", "ui.enabled",
				"db.busy_timeout_ms",
				"git.timeout",
				"lease.ttl",
				"board.default_columns",
				"labels.preset", "labels.require_on_card",
				"tags.require_on_card",
				"card.ls_limit", "card.duplicate_check", "card.duplicate_threshold",
				"search.limit",
				"search.method",
				"search.vector.enabled", "search.vector.provider", "search.vector.embed_command", "search.vector.endpoint",
				"search.vector.model", "search.vector.dimension", "search.vector.limit",
			}

			var rows []configRow

			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()

				for _, key := range allKeys {
					value, source, err := config.EffectiveValue(cmd.Context(), globalCfg, pctx.db, pctx.Project.ID, key)
					if err != nil {
						continue // Skip unknown keys (shouldn't happen).
					}
					rows = append(rows, configRow{Key: key, Value: value, Source: source})
				}

				return Emit(cmd, rows, func() string {
					return formatConfigTable(rows)
				})
			}

			// Global scope: just show defaults.
			for _, key := range allKeys {
				value, _ := config.GetValue(globalCfg, key)
				rows = append(rows, configRow{Key: key, Value: value, Source: "default"})
			}

			return Emit(cmd, rows, func() string {
				return formatConfigTable(rows)
			})
		},
	}

	return cmd
}

func formatConfigTable(rows []configRow) string {
	if len(rows) == 0 {
		return "(no config)"
	}

	// Sort by key for stable output.
	slices.SortFunc(rows, func(a, b configRow) int { return cmp.Compare(a.Key, b.Key) })

	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE\tSOURCE")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", row.Key, row.Value, row.Source)
	}
	w.Flush()

	return buf.String()
}

// resolveConfigProject returns the project whose overrides apply, or nil when
// no pin applies here: the global defaults are still a real answer. A bad
// --project, a malformed pin, or a pin naming a missing project is an error.
func resolveConfigProject() (*projectContext, error) {
	pctx, err := currentProject()
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "unresolved" {
		return nil, nil
	}
	return pctx, err
}
