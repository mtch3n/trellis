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
		// Plain version stays local: callers poll it, and a network check on
		// every call cost a GitHub request each time. --check asks GitHub.
		return Emit(cmd, map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date}, func() string {
			return fmt.Sprintf("trellis %s (%s, %s)", version.Version, version.Commit, version.Date)
		})
	}}
	cmd.Flags().BoolVar(&check, "check", false, "check GitHub for a newer release")
	return cmd
}
