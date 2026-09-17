package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newVaultTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "List, show and edit the shared templates",
		Long: "Templates live in one place, shared by every project: there is no\n" +
			"per-project template.",
	}
	cmd.AddCommand(newTemplateLsCmd(), newTemplateShowCmd(), newTemplateNewCmd(),
		newTemplateEditCmd(), newTemplateRmCmd(), newTemplateReinstallCmd(), newTemplateCheckCmd())
	return cmd
}

// parseSetFlags turns repeated --set name=value flags into a map. A flag
// without "=" is a usage error naming the exact text that was wrong.
func parseSetFlags(flags []string) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range flags {
		name, value, ok := strings.Cut(f, "=")
		if !ok {
			return nil, core.ErrUsage("bad_set", `--set wants name=value, not "`+f+`"`,
				`trellis vault new --set owner=alice`)
		}
		out[name] = value
	}
	return out, nil
}

// withGlobalCore opens core without resolving a project or board:
// templates are global, so no working-directory resolution belongs here.
func withGlobalCore(fn func(c *core.Core) error) error {
	c, db, err := openCore()
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(c)
}

func sortedChoiceFields(choices map[string][]string) []string {
	names := make([]string, 0, len(choices))
	for name := range choices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderTemplateShow(t core.Template) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", t.Name)
	fmt.Fprintf(&b, "enforce: %s\n", t.Enforce)
	if len(t.Required) > 0 {
		fmt.Fprintf(&b, "required: %s\n", strings.Join(t.Required, ", "))
	}
	for _, name := range sortedChoiceFields(t.Choices) {
		fmt.Fprintf(&b, "choices.%s: %s\n", name, strings.Join(t.Choices[name], ", "))
	}
	b.WriteString("---\n")
	b.WriteString(t.Body)
	return strings.TrimRight(b.String(), "\n")
}

func newTemplateLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List the shared templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withGlobalCore(func(c *core.Core) error {
				items, err := c.ListTemplates(cmd.Context())
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"templates": items}, func() string {
					var b strings.Builder
					w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
					for _, t := range items {
						builtin := ""
						if t.BuiltIn {
							builtin = "builtin"
						}
						fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Enforce, builtin)
					}
					w.Flush()
					return strings.TrimRight(b.String(), "\n")
				})
			})
		},
	}
}

func newTemplateShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a template's rules and skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.ShowTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return renderTemplateShow(t) })
			})
		},
	}
}

func newTemplateNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name>",
		Short: "Create a minimal template: enforce: warn, no rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.NewTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "wrote " + t.Path })
			})
		},
	}
}

func newTemplateEditCmd() *cobra.Command {
	var body TextValue
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Replace a template's whole file: frontmatter and body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !body.Changed() {
				return core.ErrUsage("missing_body", "--body replaces the whole template file",
					"trellis vault template edit "+args[0]+" --body -   # then paste and Ctrl-D")
			}
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.EditTemplate(cmd.Context(), args[0], body.String())
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "wrote " + t.Path })
			})
		},
	}
	cmd.Flags().Var(&body, "body", "the complete template file: frontmatter and skeleton")
	return cmd
}

func newTemplateRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a template file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				if err := c.DeleteTemplate(cmd.Context(), args[0]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"deleted": args[0]},
					func() string { return "deleted " + args[0] })
			})
		},
	}
}

func newTemplateReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reinstall <name>",
		Short: "Overwrite a template with its shipped version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withGlobalCore(func(c *core.Core) error {
				t, err := c.ReinstallTemplate(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, t, func() string { return "reinstalled " + t.Path })
			})
		},
	}
}

func newTemplateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <name> <entry>",
		Short: "Report an entry's diagnostics against a template, without blocking",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withBoard(func(app *appCtx) error {
				violations, err := app.Core.CheckTemplate(cmd.Context(), app.Project.ID, args[0], args[1])
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]any{"violations": violations}, func() string {
					if len(violations) == 0 {
						return "no violations"
					}
					return strings.Join(violations, "\n")
				})
			})
		},
	}
}
