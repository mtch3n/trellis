package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var key string
	var noPreset bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Pin this directory to a project, creating the project if needed",
		Long: "Write .trellis here, naming the project -- and, with --board, the board --\n" +
			"that commands run in this directory act on. Commit the file: every clone\n" +
			"and worktree then resolves to the same project.\n\n" +
			"Without --key the key comes from the directory name, and init only ever\n" +
			"creates: an existing project is joined only when named with --key.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectFlagKey != "" {
				return core.ErrUsage("init_project_flag", "init names its project with --key, not --project",
					"trellis init --key "+normalizeProjectArg(projectFlagKey))
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			reason, err := resolve.Unmarkable(dir)
			if err != nil {
				return err
			}
			if reason != "" {
				return core.ErrUsage("init_location", reason, "cd <project directory>, then trellis init")
			}

			req := core.InitRequest{Dir: dir, Key: key, Join: key != "", BoardName: boardFlag, Preset: !noPreset}
			existing, err := resolve.ReadMarker(filepath.Join(dir, resolve.MarkerFile))
			switch {
			case err == nil:
				req.Existing = &existing
			case errors.Is(err, fs.ErrNotExist):
				if req.Key == "" {
					if req.Key = address.KeyFromName(filepath.Base(dir)); req.Key == "" {
						return core.ErrUsage("missing_key",
							fmt.Sprintf("cannot derive a project key from %q", filepath.Base(dir)),
							"trellis init --key <KEY>")
					}
				}
			default:
				return markerFailure(err)
			}
			overridden := parentMarker(dir, req.Existing)

			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()

			res, err := c.InitProject(ctx, req)
			if err != nil {
				return err
			}
			board := res.Board
			if board == nil {
				if b, err := c.SelectBoard(ctx, res.Project.ID, ""); err == nil {
					board = &b
				}
			}
			var cols []core.Column
			if board != nil {
				if cols, err = c.ListColumns(ctx, board.ID); err != nil {
					return err
				}
			}
			notes := initNotes(res, overridden)

			return Emit(cmd, map[string]any{
				"project": res.Project, "board": board, "columns": cols,
				"pin_path": res.MarkerPath, "created": res.Created, "pin_written": res.Wrote,
				"notes": notes,
			}, func() string { return initTable(res, board, cols, notes) })
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "project key; naming an existing project joins it")
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}

// parentMarker is the marker a new marker in dir would override, or nil. A
// directory that holds .git already stops the walk, so nothing above it
// applies.
func parentMarker(dir string, existing *resolve.Marker) *resolve.Marker {
	if existing != nil {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	marker, found, err := resolve.FindMarker(filepath.Dir(dir))
	if err != nil || !found {
		return nil
	}
	return &marker
}

func initNotes(res core.InitResult, overridden *resolve.Marker) []string {
	notes := []string{}
	if res.Wrote {
		notes = append(notes, "commit .trellis so every clone and worktree resolves to "+res.Project.Key)
	}
	if overridden != nil {
		notes = append(notes, fmt.Sprintf("this marker overrides %s from %s", overridden.Target, overridden.Path))
	}
	if env := normalizeProjectArg(os.Getenv("TRELLIS_PROJECT")); env != "" && env != res.Project.Key {
		notes = append(notes, fmt.Sprintf("TRELLIS_PROJECT=%s is set; commands in this environment act on %s, not on this marker", env, env))
	}
	return notes
}

func initTable(res core.InitResult, board *core.Board, cols []core.Column, notes []string) string {
	var b strings.Builder
	verb := "joined project"
	if res.Created {
		verb = "created project"
	}
	fmt.Fprintf(&b, "%s %s · marker %s", verb, res.Project.Key, res.MarkerPath)
	if board != nil {
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}
		fmt.Fprintf(&b, "\nboard %s · columns: %v", board.Name, names)
	}
	for _, n := range notes {
		b.WriteString("\nnote: " + n)
	}
	return b.String()
}
