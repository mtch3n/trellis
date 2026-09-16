package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	r, err := resolveProject(context.Background(), c)
	if err != nil {
		db.Close()
		return nil, err
	}
	p := r.Project
	// The repository file sits beside the pin that chose the project. A
	// project named by --project or TRELLIS_PROJECT has no pin, so it reads
	// no repository file.
	var repoDir string
	if r.Pin != nil {
		repoDir = filepath.Dir(r.Pin.Path)
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
	var repoFlag bool
	cmd := &cobra.Command{
		Use: "unset <key>", Short: "Remove a project override, or a repository config value with --repo", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if repoFlag {
				if !config.RepoSafe(key) {
					return core.ErrUsage("not_repo_safe", fmt.Sprintf("%q may not be set by a repository", key), "trellis config ls")
				}
				dir, err := repoConfigDir()
				if err != nil {
					return err
				}
				path, err := config.UnsetRepoValue(dir, key)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"unset": key, "file": path}, func() string { return "unset " + key + " in " + path })
			}

			pctx, err := currentProject()
			if err != nil {
				return err
			}
			defer pctx.db.Close()
			if _, ok := config.GetValue(pctx.cfg, key); !ok {
				return core.ErrUsage("unknown_key", fmt.Sprintf("unknown config key: %q", key), "trellis config ls")
			}
			if err := config.UnsetProjectConfig(cmd.Context(), pctx.db, pctx.Project.ID, key); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"unset": key}, func() string { return "unset " + key })
		},
	}
	cmd.Flags().BoolVar(&repoFlag, "repo", false, "unset in the repository's .trellis.yaml instead of a project override")
	return cmd
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
	var repoFlag bool

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long: "Set a config value.\n\n" +
			"A value starting with - (e.g. a negative number) needs a -- separator " +
			"so it is not read as a flag: trellis config set -- history.keep -1",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			if repoFlag {
				if !config.RepoSafe(key) {
					return core.ErrUsage("not_repo_safe", fmt.Sprintf("%q may not be set by a repository", key), "trellis config ls")
				}
				dir, err := repoConfigDir()
				if err != nil {
					return err
				}
				path, err := config.SetRepoValue(dir, key, value)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]string{
					"key": key, "value": value, "scope": "repo", "file": path,
				}, func() string {
					return fmt.Sprintf("%s = %s (%s)", key, value, path)
				})
			}

			// config set (without --repo) only ever writes a project
			// override: the global file is hand-edited YAML (§5.4), so there
			// is no scope to choose.
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
			if err := config.ValidateValue(key, value); err != nil {
				return core.ErrUsage("invalid_value", err.Error(), "trellis config set history.keep 100")
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

	cmd.Flags().BoolVar(&repoFlag, "repo", false, "write to the repository's .trellis.yaml instead of a project override")
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
// no pin applies here: the global defaults are still a real answer. A bad
// --project, a malformed pin, or a pin naming a missing project is an error.
// repoConfigDir is the directory whose .trellis.yaml --repo edits: the one
// holding the pin that resolves the working directory. It is never simply the
// working directory, because a file written anywhere else would never be read.
// A project named by --project or TRELLIS_PROJECT has no pin to write beside.
func repoConfigDir() (string, error) {
	if projectKey() != "" {
		return "", core.ErrUsage("no_pin",
			"--repo writes beside a .trellis pin, and --project or TRELLIS_PROJECT names a project without one",
			"run the command inside the pinned directory, without --project")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	pin, found, err := resolve.FindPin(cwd)
	if err != nil {
		return "", pinFailure(err)
	}
	if !found {
		return "", core.ErrUsage("unresolved",
			"no .trellis pin in this directory or any parent", "trellis init --key <KEY>")
	}
	return filepath.Dir(pin.Path), nil
}

func resolveConfigProject() (*projectContext, error) {
	pctx, err := currentProject()
	if ce, ok := errors.AsType[*core.Error](err); ok && ce.Code == "unresolved" {
		return nil, nil
	}
	return pctx, err
}
