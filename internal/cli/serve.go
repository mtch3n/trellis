package cli

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/mtch3n/trellis/internal/server"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the multi-user Trellis server",
		Long:  "Run the multi-user Trellis server. TRELLIS_DATABASE_URL names the Postgres database; TRELLIS_ADDR is the listen address (default " + server.DefaultAddr + ").",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return server.Run(ctx, os.Getenv, func(addr net.Addr) {
				fmt.Fprintf(cmd.ErrOrStderr(), "trellis serve: listening on http://%s\n", addr)
			})
		},
	}
}
