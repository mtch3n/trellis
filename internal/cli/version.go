package cli

import (
	"fmt"
	"github.com/mtch3n/trellis/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{Use: "version", Short: "Show the Trellis version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if check {
			return checkForUpdate(cmd.Context(), false, false, false)
		}
		if err := Emit(cmd, map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date}, func() string {
			return fmt.Sprintf("trellis %s (%s, %s)", version.Version, version.Commit, version.Date)
		}); err != nil {
			return err
		}
		// Version checks are informational and must never prompt or install.
		if err := checkForUpdate(cmd.Context(), false, false, false); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "update check unavailable: %v\n", err)
		}
		return nil
	}}
	cmd.Flags().BoolVar(&check, "check", false, "check GitHub for a newer release")
	return cmd
}
