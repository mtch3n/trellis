package cli

import (
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newLabelCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "label", Short: "Work with labels"}
	cmd.AddCommand(
		newLabelNewCmd(),
		newLabelLsCmd(),
		newLabelRmCmd(),
		newLabelMergeCmd(),
	)
	return cmd
}

func newLabelNewCmd() *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if description == "" {
				return core.ErrUsage("missing_description",
					"a label needs a description",
					`trellis label new <name> --description "..."`)
			}
			return withBoard(func(app *appCtx) error {
				label, err := app.Core.CreateLabel(cmd.Context(), app.Project.ID, args[0], description)
				if err != nil {
					return err
				}
				return Emit(cmd, label, func() string {
					return args[0] + " created"
				})
			})
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "label description (required)")
	return cmd
}

func newLabelLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List labels in this project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withBoard(func(app *appCtx) error {
				labels, err := app.Core.ListLabels(cmd.Context(), app.Project.ID)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"labels": labels}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, l := range labels {
						fmt.Fprintf(w, "%s\t%s\n", l.Name, l.Description)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newLabelRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if err := app.Core.DeleteLabel(cmd.Context(), app.Project.ID, args[0]); err != nil {
					if e, ok := errors.AsType[*core.Error](err); ok && e.Code == "label_in_use" {
						// Enhance the error message with the available information
						labels, listErr := app.Core.ListLabels(cmd.Context(), app.Project.ID)
						if listErr == nil {
							names := make([]string, len(labels))
							for i, l := range labels {
								names[i] = l.Name
							}
							otherLabels := make([]string, 0, len(names))
							for _, n := range names {
								if n != args[0] {
									otherLabels = append(otherLabels, n)
								}
							}
							if len(otherLabels) > 0 {
								e.Fix = fmt.Sprintf("trellis label merge %s %s", args[0], otherLabels[0])
							}
						}
					}
					return err
				}
				return Emit(cmd, map[string]any{"deleted": args[0]}, func() string {
					return args[0] + " deleted"
				})
			})
		},
	}
}

func newLabelMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge <from> <into>",
		Short: "Merge one label into another",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				if err := app.Core.MergeLabel(cmd.Context(), app.Project.ID, args[0], args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"merged": args[0], "into": args[1]}, func() string {
					return fmt.Sprintf("%s merged into %s", args[0], args[1])
				})
			})
		},
	}
}
