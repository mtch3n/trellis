package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newBoardCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "board", Short: "Work with boards"}

	// board ls
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List this project's boards",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := currentBoard()
			if err != nil {
				return err
			}
			defer app.db.Close()

			boards, err := app.Core.ListBoards(cmd.Context(), app.Project.ID)
			if err != nil {
				return err
			}

			counts, err := app.Core.BoardCardCounts(cmd.Context(), app.Project.ID)
			if err != nil {
				return err
			}

			type boardInfo struct {
				Name      string `json:"name"`
				Slug      string `json:"slug"`
				IsDefault bool   `json:"is_default"`
				CardCount int    `json:"card_count"`
			}

			infos := make([]boardInfo, len(boards))
			for i, b := range boards {
				infos[i] = boardInfo{
					Name:      b.Name,
					Slug:      b.Slug,
					IsDefault: b.IsDefault,
					CardCount: counts[b.ID],
				}
			}

			return Emit(cmd, map[string]any{"boards": infos}, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				for _, info := range infos {
					defaultMark := ""
					if info.IsDefault {
						defaultMark = "*"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", info.Name, info.Slug, defaultMark, info.CardCount)
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	})

	// board new
	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Create a board",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			noColumns, _ := cmd.Flags().GetBool("no-columns")

			if name == "" {
				return core.ErrUsage("missing_name",
					"board name required",
					"trellis board new --name <board>")
			}

			app, err := currentBoard()
			if err != nil {
				return err
			}
			defer app.db.Close()

			board, err := app.Core.CreateBoard(cmd.Context(), app.Project.ID, name, !noColumns)
			if err != nil {
				return err
			}

			return Emit(cmd, board, func() string {
				return fmt.Sprintf("created board %q", board.Name)
			})
		},
	}
	newCmd.Flags().String("name", "", "board name")
	newCmd.Flags().Bool("no-columns", false, "do not seed default columns")
	cmd.AddCommand(newCmd)

	// board default
	defaultCmd := &cobra.Command{
		Use:   "default <board>",
		Short: "Set the default board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withTarget(refArg{Collection: address.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
				board, err := app.Core.SetDefaultBoard(cmd.Context(), app.Project.ID, name)
				if err != nil {
					return err
				}

				return Emit(cmd, map[string]any{"board": board.Name}, func() string {
					return fmt.Sprintf("set default board to %q", board.Name)
				})
			})
		},
	}
	cmd.AddCommand(defaultCmd)

	// board show
	cmd.AddCommand(newBoardShowCmd())
	cmd.AddCommand(newBoardRenameCmd(), newBoardRmCmd())

	return cmd
}

func newBoardRenameCmd() *cobra.Command {
	return &cobra.Command{Use: "rename <from> <to>", Short: "Rename a board", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withTarget(refArg{Collection: address.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			b, err := app.Core.RenameBoard(cmd.Context(), app.Project.ID, name, args[1])
			if err != nil {
				return err
			}
			return Emit(cmd, b, func() string { return fmt.Sprintf("renamed board %q", b.Name) })
		})
	}}
}

func newBoardRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "rm <board>", Short: "Delete a board", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withTarget(refArg{Collection: address.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			if err := app.Core.DeleteBoard(cmd.Context(), app.Project.ID, name, force); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"deleted": args[0]}, func() string { return "deleted " + args[0] })
		})
	}}
	cmd.Flags().BoolVar(&force, "force", false, "delete cards on this board")
	return cmd
}
