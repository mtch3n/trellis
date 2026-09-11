package cli

import (
	"fmt"

	"github.com/mtch3n/trellis/internal/daemon"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/spf13/cobra"
)

func newUICmd() *cobra.Command {
	var port int
	var open bool

	cmd := &cobra.Command{
		Use:    "ui",
		Short:  "Serve the web UI",
		Long:   "Start the trellis web UI server on 127.0.0.1",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if open {
				_ = open
			} // retained for CLI compatibility; opening is host-specific.
			root, err := home.Root()
			if err == nil {
				if resp, callErr := daemon.Call(cmd.Context(), daemon.Endpoint(root), daemon.Request{Method: "health"}); callErr == nil {
					fmt.Printf("trellis UI is already running at %v\n", resp.Data["url"])
					return nil
				}
			}
			return runApplicationServerContext(cmd.Context(), "127.0.0.1", port)
		},
	}

	cmd.Flags().IntVar(&port, "port", 7788, "port to listen on")
	cmd.Flags().BoolVar(&open, "open", false, "open in browser after starting")

	return cmd
}
