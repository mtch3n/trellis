package cli

import (
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/importer"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var opt importer.Options
	cmd := &cobra.Command{
		Use:   "import --home <dir> --org <org> --member <handle>",
		Short: "Copy a local Trellis home into the server once",
		Long:  `Copy every project of a local Trellis home (trellis.db and the files under projects/) into an org on the server, one transaction per project. Every local actor becomes --member. A project whose key the org already has is skipped, so a rerun imports only what is missing. Run it where TRELLIS_DATABASE_URL reaches the server's Postgres database.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			url := os.Getenv("TRELLIS_DATABASE_URL")
			if url == "" {
				return platform.ErrUsage("no_database", "TRELLIS_DATABASE_URL is not set", "export TRELLIS_DATABASE_URL=postgres://trellis@localhost:5432/trellis")
			}
			db, err := pgstore.Open(cmd.Context(), url)
			if err != nil {
				return platform.ErrUsage("no_database", "cannot open the database that TRELLIS_DATABASE_URL names: "+err.Error(), "check TRELLIS_DATABASE_URL")
			}
			defer db.Close()
			rep, err := importer.Run(cmd.Context(), db, opt)
			if err != nil {
				return err
			}
			if err := Emit(cmd, rep, rep.Text); err != nil {
				return err
			}
			if len(rep.Failed) > 0 {
				failed := make([]string, 0, len(rep.Failed))
				for _, f := range rep.Failed {
					failed = append(failed, f.Project)
				}
				return platform.ErrConflict("import_failed", "these projects were not imported: "+strings.Join(failed, ", "),
					"fix the problem named for each project, then run trellis import again")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opt.Home, "home", "", "the local Trellis home, for example ~/.trellis")
	cmd.Flags().StringVar(&opt.Org, "org", "", "the org on the server")
	cmd.Flags().StringVar(&opt.Member, "member", "", "the member that every local actor becomes")
	for _, f := range []string{"home", "org", "member"} {
		cmd.MarkFlagRequired(f)
	}
	return cmd
}
