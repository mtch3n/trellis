package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var pin bool
	var key string
	var noPreset bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the board for this project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pin {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				if key == "" {
					key = filepath.Base(dir)
				}
				if err := os.WriteFile(filepath.Join(dir, ".trellis"),
					[]byte(key+"\n"), 0o644); err != nil {
					return err
				}
			}
			app, err := currentBoard()
			if err != nil {
				return err
			}
			defer app.db.Close()

			// Seed default labels unless --no-preset is specified
			if !noPreset {
				if _, err := app.Core.EnsureDefaultLabels(cmd.Context(), app.Project.ID); err != nil {
					return err
				}
			}

			cols, err := app.Core.ListColumns(cmd.Context(), app.Board.ID)
			if err != nil {
				return err
			}
			return Emit(cmd, map[string]any{"project": app.Project, "board": app.Board, "columns": cols},
				func() string {
					names := make([]string, len(cols))
					for i, c := range cols {
						names[i] = c.Name
					}
					return fmt.Sprintf("%s ready · columns: %v", app.Project.Key, names)
				})
		},
	}
	cmd.Flags().BoolVar(&pin, "pin", false, "write a .trellis file pinning this directory")
	cmd.Flags().StringVar(&key, "key", "", "project key, when the default collides")
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}
