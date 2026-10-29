package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var key string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Mark this directory with a project",
		Long: "Write .trellis here, naming the project that trellis call uses in this directory.\n" +
			"Commit the file: every clone and worktree then resolves to the same project.\n\n" +
			"Without --key the key comes from the directory name.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if projectFlagKey != "" {
				return platform.ErrUsage("init_project_flag", "init names its project with --key, not --project",
					"trellis init --key "+strings.ToUpper(strings.TrimSpace(projectFlagKey)))
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
				return platform.ErrUsage("init_location", reason, "cd <project directory>, then trellis init")
			}
			key = strings.ToUpper(strings.TrimSpace(key))
			if key == "" {
				if key = address.KeyFromName(filepath.Base(dir)); key == "" {
					return platform.ErrUsage("missing_key",
						fmt.Sprintf("cannot derive a project key from %q", filepath.Base(dir)), "trellis init --key <KEY>")
				}
			}
			target := address.Project(key)
			if _, err := address.ParseMarker(target.String()); err != nil {
				return platform.ErrUsage("bad_key", fmt.Sprintf("%s is not a project key: %v", key, err), "trellis init --key <KEY>")
			}
			path := filepath.Join(dir, resolve.MarkerFile)
			wrote, err := writeMarker(path, target)
			if err != nil {
				return err
			}
			notes := initNotes(key, wrote, parentMarker(dir))
			return Emit(cmd, map[string]any{"project": key, "marker_path": path, "marker_written": wrote, "notes": notes},
				func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "project %s · marker %s", key, path)
					for _, n := range notes {
						b.WriteString("\nnote: " + n)
					}
					return b.String()
				})
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "project key")
	return cmd
}

func writeMarker(path string, target address.Address) (bool, error) {
	existing, err := resolve.ReadMarker(path)
	switch {
	case err == nil && existing.Target == target:
		return false, nil
	case err == nil:
		return false, platform.ErrConflict("marker_exists",
			fmt.Sprintf("%s already names %s", path, existing.Target), "edit or delete "+path+", then rerun trellis init")
	case isMarkerError(err):
		return false, platform.ErrUsage("bad_marker", err.Error(), "trellis init --key <KEY>   # after removing "+path)
	case !errors.Is(err, fs.ErrNotExist):
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(target.String() + "\n"); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

func parentMarker(dir string) *resolve.Marker {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	marker, found, err := resolve.FindMarker(filepath.Dir(dir))
	if err != nil || !found {
		return nil
	}
	return &marker
}

func initNotes(key string, wrote bool, overridden *resolve.Marker) []string {
	notes := []string{}
	if wrote {
		notes = append(notes, "commit .trellis so every clone and worktree resolves to "+key)
	}
	if overridden != nil {
		notes = append(notes, fmt.Sprintf("this marker overrides %s from %s", overridden.Target, overridden.Path))
	}
	if env := strings.ToUpper(strings.TrimSpace(os.Getenv("TRELLIS_PROJECT"))); env != "" && env != key {
		notes = append(notes, fmt.Sprintf("TRELLIS_PROJECT=%s is set; commands in this environment act on %s, not on this marker", env, env))
	}
	return notes
}

func isMarkerError(err error) bool {
	_, ok := errors.AsType[*resolve.MarkerError](err)
	return ok
}
