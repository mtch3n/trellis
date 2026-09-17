package cli

import (
	"fmt"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/spf13/cobra"
)

// newUICmd is the one command a human runs to reach the board in a browser. It
// never starts a second server: when a daemon is already serving the UI --
// which is the normal case once `trellis daemon install` has run -- it prints
// where that daemon is and exits.
func newUICmd() *cobra.Command {
	var port int
	var open bool

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open or report the web UI",
		Long:  "Print the address of the running UI, or serve it here when no daemon is.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if open {
				_ = open
			} // retained for CLI compatibility; opening is host-specific.
			cfg := config.Defaults()
			if root, err := home.Root(); err == nil {
				if loaded, err := config.Load(root); err == nil {
					cfg = loaded
				}
			}
			if !cfg.UI.UIEnabled() {
				return core.ErrUsage("ui_disabled",
					"the web UI is turned off by ui.enabled in the config file",
					"set ui.enabled: true in "+configFileHint())
			}

			status, err := resolveDaemonStatus(cmd.Context())
			if err != nil {
				return err
			}
			if status.Running {
				if status.URL == "" {
					// A daemon started while ui.enabled was false holds the
					// database lock, so serving here would fail on the port
					// it never opened; restarting it is the fix.
					return core.ErrUsage("ui_not_served",
						"a trellis daemon is running but is not serving the web UI",
						"trellis daemon restart")
				}
				return Emit(cmd, map[string]any{"url": status.URL, "managed_by": status.ManagedBy, "started": false},
					func() string {
						return fmt.Sprintf("trellis UI is already running at %s\n  supervised by %s",
							status.URL, describeManager(status))
					})
			}

			bind, resolved := daemonDefaults("", port)
			return runApplicationServerContext(cmd.Context(), bind, resolved)
		},
	}

	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (default: ui.port)")
	cmd.Flags().BoolVar(&open, "open", false, "open in browser after starting")

	return cmd
}
