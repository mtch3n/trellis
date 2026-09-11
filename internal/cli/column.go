package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newColumnCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "column", Short: "Work with columns"}
	cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List this board's columns",
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := currentBoard()
			if err != nil {
				return err
			}
			defer app.db.Close()

			cols, err := app.Core.ListColumns(cmd.Context(), app.Board.ID)
			if err != nil {
				return err
			}
			return Emit(cmd, map[string]any{"columns": cols}, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				for _, c := range cols {
					done := ""
					if c.IsDone {
						done = "done"
					}
					fmt.Fprintf(w, "%d\t%s\t%s\n", c.Position, c.Name, done)
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	})
	cmd.AddCommand(newColumnAddCmd(), newColumnRenameCmd(), newColumnMoveCmd(), newColumnRmCmd())
	return cmd
}

func newColumnAddCmd() *cobra.Command {
	var after string
	var done bool
	cmd := &cobra.Command{Use: "add <name>", Short: "Add a column", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, err := currentBoard()
		if err != nil {
			return err
		}
		defer app.db.Close()
		col, err := app.Core.AddColumn(cmd.Context(), app.Board.ID, args[0], after, done)
		if err != nil {
			return err
		}
		return Emit(cmd, col, func() string { return "added " + col.Name })
	}}
	cmd.Flags().StringVar(&after, "after", "", "insert after this column")
	cmd.Flags().BoolVar(&done, "done", false, "mark as terminal")
	return cmd
}

func newColumnRenameCmd() *cobra.Command {
	return &cobra.Command{Use: "rename <from> <to>", Short: "Rename a column", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		app, err := currentBoard()
		if err != nil {
			return err
		}
		defer app.db.Close()
		if err := app.Core.RenameColumn(cmd.Context(), app.Board.ID, args[0], args[1]); err != nil {
			return err
		}
		return Emit(cmd, map[string]string{"renamed": args[0], "to": args[1]}, func() string { return "renamed " + args[0] + " to " + args[1] })
	}}
}

func newColumnMoveCmd() *cobra.Command {
	var after string
	cmd := &cobra.Command{Use: "move <name>", Short: "Move a column", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, err := currentBoard()
		if err != nil {
			return err
		}
		defer app.db.Close()
		if err := app.Core.MoveColumn(cmd.Context(), app.Board.ID, args[0], after); err != nil {
			return err
		}
		return Emit(cmd, map[string]string{"moved": args[0]}, func() string { return "moved " + args[0] })
	}}
	cmd.Flags().StringVar(&after, "after", "", "place after this column (empty means first)")
	return cmd
}

func newColumnRmCmd() *cobra.Command {
	var move string
	cmd := &cobra.Command{Use: "rm <name>", Short: "Remove a column", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		app, err := currentBoard()
		if err != nil {
			return err
		}
		defer app.db.Close()
		if err := app.Core.DeleteColumn(cmd.Context(), app.Board.ID, args[0], move); err != nil {
			return err
		}
		return Emit(cmd, map[string]string{"deleted": args[0]}, func() string { return "deleted " + args[0] })
	}}
	cmd.Flags().StringVar(&move, "move-cards-to", "", "move cards to this column")
	return cmd
}
