package cli

import (
	"encoding/json/v2"
	"errors"
	"os"

	"github.com/mtch3n/trellis/internal/client"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/spf13/cobra"
)

func newCallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "call <tool> [json]",
		Short: "Call one tool of the Trellis server and print its JSON result",
		Long: "Call one MCP tool of the Trellis server and print its JSON result. The server comes from TRELLIS_URL and TRELLIS_TOKEN, else from trellis login. " +
			"The project comes from --project, TRELLIS_PROJECT or the .trellis marker; TRELLIS_SESSION names the session. A refused call prints its {code, message, fix} JSON and exits 1.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := client.Load(os.Getenv)
			if errors.Is(err, client.ErrNoServer) {
				return adminErr(platform.ErrUsage("no_server", "no Trellis server: TRELLIS_URL and TRELLIS_TOKEN are not set and trellis login has not run",
					"trellis login --url <server URL> --token <agent token>"))
			}
			if err != nil {
				return err
			}
			var toolArgs map[string]any
			if len(args) == 2 {
				if err := json.Unmarshal([]byte(args[1]), &toolArgs); err != nil {
					return adminErr(platform.ErrUsage("bad_json", "the arguments are not a JSON object: "+err.Error(),
						`trellis call `+args[0]+` '{"field":"value"}'`))
				}
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			project, err := client.ProjectKey(projectFlagKey, os.Getenv, dir)
			if err != nil {
				return adminErr(platform.ErrUsage("bad_marker", err.Error(), "fix the .trellis marker, or pass --project"))
			}
			res, err := client.Call(cmd.Context(), cfg, client.Request{
				Project: project, Session: os.Getenv("TRELLIS_SESSION"), Tool: args[0], Args: toolArgs,
			})
			if err != nil {
				return err
			}
			cmd.OutOrStdout().Write(append(res.JSON, '\n'))
			if res.Refused {
				return errExit{code: 1}
			}
			return nil
		},
	}
}
