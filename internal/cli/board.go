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
	var trashed bool
	ls := &cobra.Command{
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
				TrashedAt *int64 `json:"trashed_at,omitzero"`
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
			if trashed {
				gone, err := app.Core.TrashedBoards(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				for _, b := range gone {
					infos = append(infos, boardInfo{Name: b.Name, Slug: b.Slug, TrashedAt: b.TrashedAt})
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
					if info.TrashedAt != nil {
						defaultMark = "(trashed)"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", info.Name, info.Slug, defaultMark, info.CardCount)
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	}
	ls.Flags().BoolVar(&trashed, "trashed", false, "include boards in the trash")
	cmd.AddCommand(ls)

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
	cmd.AddCommand(newBoardRenameCmd(), newBoardRmCmd(), newBoardRestoreCmd())

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
	cmd := &cobra.Command{Use: "rm <board>", Short: "Move a board and its cards to the trash", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withTarget(refArg{Collection: address.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			if err := app.Core.DeleteBoard(cmd.Context(), app.Project.ID, name, force); err != nil {
				return err
			}
			return Emit(cmd, map[string]string{"trashed": args[0]}, func() string {
				return args[0] + " moved to the trash (trellis board restore " + args[0] + " brings it back)"
			})
		})
	}}
	cmd.Flags().BoolVar(&force, "force", false, "trash the cards on this board too")
	return cmd
}

func newBoardRestoreCmd() *cobra.Command {
	return &cobra.Command{Use: "restore <board>", Short: "Put a trashed board back with its cards", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withTarget(refArg{Collection: address.CollectionBoards, Value: args[0]}, func(app *appCtx, name string) error {
			b, err := app.Core.RestoreBoard(cmd.Context(), app.Project.ID, name)
			if err != nil {
				return err
			}
			return Emit(cmd, b, func() string { return "restored " + b.Name })
		})
	}}
}
