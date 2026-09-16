package cli

import (
	"encoding/json/v2"
	"os"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "extension", Short: "Read what a repository's .trellis.yaml sets for an extension"}
	cmd.AddCommand(newExtensionConfigCmd())
	return cmd
}

func newExtensionConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config <name>",
		Short: "Print the extensions.<name> subtree of .trellis.yaml as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var repoDir string
			if projectKey() == "" {
				dir, err := os.Getwd()
				if err != nil {
					return err
				}
				repoDir = dir
			}
			doc, _, _, err := config.LoadRepo(repoDir)
			if err != nil {
				return core.ErrUsage("bad_repo_config", err.Error(), "fix the file .trellis.yaml/.trellis.yml names")
			}
			var subtree any
			if m, ok := doc.Extensions.(map[string]any); ok {
				subtree = m[args[0]]
			}
			return Emit(cmd, subtree, func() string {
				b, _ := json.Marshal(subtree)
				return string(b)
			})
		},
	}
}
