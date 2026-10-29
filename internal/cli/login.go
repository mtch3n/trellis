package cli

import (
	"strings"

	"github.com/mtch3n/trellis/internal/client"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	var url, token string
	cmd := &cobra.Command{
		Use:   "login --url <server URL> --token <agent token>",
		Short: "Save the Trellis server and token that trellis call uses",
		Long:  "Save the Trellis server URL and agent token to client.json in the user config directory, with mode 0600. TRELLIS_URL and TRELLIS_TOKEN win over the file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, token = strings.TrimSpace(url), strings.TrimSpace(token)
			if url == "" || token == "" {
				return platform.ErrUsage("missing_field", "login needs --url and --token", "trellis login --url <server URL> --token <agent token>")
			}
			path, err := client.Save(client.Config{URL: url, Token: token})
			if err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"url": url, "config": path}, func() string { return "saved " + url + " to " + path })
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "the server URL, such as https://trellis.example.com")
	cmd.Flags().StringVar(&token, "token", "", "an agent token from trellis admin token new")
	return cmd
}
