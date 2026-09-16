package cli

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/spf13/cobra"
)

// projectContext holds the current project info needed for config operations.
type projectContext struct {
	Core    *core.Core
	Project core.Project
	db      *sqlx.DB
	cfg     config.Config
	present map[string]bool
	repo    config.RepoDoc
}

// currentProject resolves the current project without requiring a board. It
// also resolves the directory that answered — the .trellis pin's directory,
// or the repository root when there is no pin yet — and loads that
// directory's .trellis.yaml, if any. A project named by --project or
// TRELLIS_PROJECT skips directory resolution entirely, so it reads no
// repository file, matching the design.
func currentProject() (*projectContext, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}

	var p core.Project
	var repoDir string
	if key := projectKey(); key != "" {
		// --project XPSCTL settings a project you are not standing in.
		if p, err = c.ProjectByKey(context.Background(), key); err != nil {
			db.Close()
			return nil, err
		}
	} else {
		dir, err := os.Getwd()
		if err != nil {
			db.Close()
			return nil, err
		}
		id, err := resolve.Identify(dir)
		if err != nil {
			db.Close()
			return nil, core.ErrUsage("unresolved", err.Error(), "trellis init --pin")
		}
		repoDir = id.RootPath
		if p, err = c.EnsureProject(context.Background(), id); err != nil {
			db.Close()
			return nil, err
		}
	}

	cfg, present, err := config.LoadWithPresence()
	if err != nil {
		// Log but don't fail: config file issues are warnings, not hard stops.
		// Fall back to defaults.
		cfg = config.Defaults()
		present = map[string]bool{}
	}
	repo, _, _, err := config.LoadRepo(repoDir)
	if err != nil {
		db.Close()
		return nil, core.ErrUsage("bad_repo_config", err.Error(), "fix the file .trellis.yaml/.trellis.yml names")
	}

	return &projectContext{
		Core:    c,
		Project: p,
		db:      db,
		cfg:     cfg,
		present: present,
		repo:    repo,
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

			// A project override always wins where one resolves; outside a
			// repository there is nothing to override with, so the global
			// default (or the repo file, if standing in one) is the answer.
			// The source field says which happened, so no flag is needed to
			// ask.
			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()
				value, source, err := config.EffectiveValue(cmd.Context(), pctx.cfg, pctx.present, pctx.repo, pctx.db, pctx.Project.ID, key)
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
			globalCfg, present, err := config.LoadWithPresence()
			if err != nil {
				globalCfg, present = config.Defaults(), map[string]bool{}
			}
			value, found := config.GetValue(globalCfg, key)
			if !found {
				return core.ErrUsage("unknown_key",
					fmt.Sprintf("unknown config key: %q", key),
					"trellis config ls")
			}
			source := "default"
			if present[key] {
				source = "config"
			}

			return Emit(cmd, map[string]string{
				"key":    key,
				"value":  value,
				"source": source,
			}, func() string {
				return fmt.Sprintf("%s = %s  (%s)", key, value, source)
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
			var rows []configRow

			pctx, perr := resolveConfigProject()
			if perr != nil {
				return perr
			}
			if pctx != nil {
				defer pctx.db.Close()

				for _, key := range config.AllKeys() {
					value, source, err := config.EffectiveValue(cmd.Context(), pctx.cfg, pctx.present, pctx.repo, pctx.db, pctx.Project.ID, key)
					if err != nil {
						continue // Skip unknown keys (shouldn't happen).
					}
					rows = append(rows, configRow{Key: key, Value: value, Source: source})
				}

				return Emit(cmd, rows, func() string {
					return formatConfigTable(rows)
				})
			}

			// Global scope: just show defaults, or the global file's values.
			globalCfg, present, err := config.LoadWithPresence()
			if err != nil {
				globalCfg, present = config.Defaults(), map[string]bool{}
			}
			for _, key := range config.AllKeys() {
				value, _ := config.GetValue(globalCfg, key)
				source := "default"
				if present[key] {
					source = "config"
				}
				rows = append(rows, configRow{Key: key, Value: value, Source: source})
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
// there is none. A bad --project is an error; merely standing outside a
// repository is not, because the global defaults are still a real answer.
func resolveConfigProject() (*projectContext, error) {
	pctx, err := currentProject()
	if err != nil {
		if projectKey() != "" {
			return nil, err
		}
		return nil, nil
	}
	return pctx, nil
}
