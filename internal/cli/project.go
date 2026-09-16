package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Create and list projects"}
	cmd.AddCommand(newProjectNewCmd(), newProjectLsCmd())
	return cmd
}

func newProjectNewCmd() *cobra.Command {
	var noPreset bool
	cmd := &cobra.Command{
		Use:   "new <KEY>",
		Short: "Create a project without pinning any directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := c.CreateProject(cmd.Context(), args[0], !noPreset)
			if err != nil {
				return err
			}
			return Emit(cmd, p, func() string {
				return fmt.Sprintf("created project %s · pin a directory to it with: trellis init --key %s", p.Key, p.Key)
			})
		},
	}
	cmd.Flags().BoolVar(&noPreset, "no-preset", false, "skip seeding default labels")
	return cmd
}

func newProjectLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List every project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx := cmd.Context()
			projects, err := c.ListProjects(ctx)
			if err != nil {
				return err
			}
			type row struct {
				Key       string `json:"key"`
				Name      string `json:"name"`
				Boards    int    `json:"boards"`
				CreatedAt int64  `json:"created_at"`
			}
			rows := make([]row, len(projects))
			for i, p := range projects {
				boards, err := c.ListBoards(ctx, p.ID)
				if err != nil {
					return err
				}
				rows[i] = row{Key: p.Key, Name: p.Name, Boards: len(boards), CreatedAt: p.CreatedAt}
			}
			return Emit(cmd, map[string]any{"projects": rows}, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				for _, r := range rows {
					fmt.Fprintf(w, "%s\t%d boards\t%s\n", r.Key, r.Boards,
						time.UnixMilli(r.CreatedAt).UTC().Format(time.DateOnly))
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	}
}
