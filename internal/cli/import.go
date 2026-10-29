package cli

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/core"
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
				return adminErr(platform.ErrUsage("no_database", "TRELLIS_DATABASE_URL is not set", "export TRELLIS_DATABASE_URL=postgres://trellis@localhost:5432/trellis"))
			}
			db, err := pgstore.Open(cmd.Context(), url)
			if err != nil {
				return adminErr(platform.ErrUsage("no_database", "cannot open the database that TRELLIS_DATABASE_URL names: "+err.Error(), "check TRELLIS_DATABASE_URL"))
			}
			defer db.Close()
			rep, err := importer.Run(cmd.Context(), db, opt)
			if err != nil {
				return adminErr(err)
			}
			if err := Emit(cmd, rep, rep.Text); err != nil {
				return err
			}
			if len(rep.Failed) > 0 {
				failed := make([]string, 0, len(rep.Failed))
				for _, f := range rep.Failed {
					failed = append(failed, f.Project)
				}
				return adminErr(platform.ErrConflict("import_failed", "these projects were not imported: "+strings.Join(failed, ", "),
					"fix the problem named for each project, then run trellis import again"))
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

// newCardImportCmd turns an implementation plan into a board in one call
// (story A2.8). The source is "-" (stdin, the default) or "@file".
func newCardImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [-|@file]",
		Short: "Create many cards from a JSON array",
		Long: `Create many cards from a JSON array, in one transaction.

  [{"id":"a","title":"write the migration","labels":["chore"]},
   {"title":"use it","blocked_by":["a"]}]

"id" is a handle local to this import, so a card can depend on one that has no
ref yet. "blocked_by" also accepts an existing ref like XPSCTL-12.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := "-"
			if len(args) == 1 {
				src = args[0]
			}
			raw, err := readSource(src)
			if err != nil {
				return err
			}
			var items []core.ImportCard
			if err := json.Unmarshal(raw, &items); err != nil {
				return core.ErrUsage("bad_json", "the input is not a JSON array of cards: "+err.Error(),
					`echo '[{"title":"first step"}]' | trellis card import`)
			}
			return withBoard(func(app *appCtx) error {
				cards, err := app.Core.ImportCards(cmd.Context(), app.Project.ID, app.Board.ID, items)
				if err != nil {
					return err
				}
				return Emit(cmd, cards, func() string {
					var b strings.Builder
					fmt.Fprintf(&b, "imported %d cards into %s\n", len(cards), app.Board.Name)
					for _, c := range cards {
						fmt.Fprintf(&b, "  %s  %s\n", c.Ref, c.Title)
					}
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

// readSource accepts "-" for stdin or "@path" for a file, matching TextValue.
func readSource(src string) ([]byte, error) {
	switch {
	case src == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, core.ErrUsage("stdin_unreadable", "could not read stdin: "+err.Error(),
				"trellis card import @plan.json")
		}
		return b, nil
	case strings.HasPrefix(src, "@"):
		b, err := os.ReadFile(src[1:])
		if err != nil {
			return nil, core.ErrUsage("file_unreadable", err.Error(),
				"trellis card import @plan.json")
		}
		return b, nil
	default:
		return nil, core.ErrUsage("bad_source", "the source is \"-\" for stdin or \"@file\"",
			"trellis card import @plan.json")
	}
}
