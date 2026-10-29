package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/platform"
	"github.com/spf13/cobra"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Set up orgs, members, projects and tokens on the multi-user server",
		Long:  "Set up orgs, members, projects and tokens on the multi-user server. Run it where TRELLIS_DATABASE_URL reaches the server's Postgres database.",
	}
	cmd.AddCommand(newAdminOrgCmd(), newAdminMemberCmd(), newAdminProjectCmd(), newAdminTokenCmd())
	return cmd
}

func adminRun(cmd *cobra.Command, fn func(ctx context.Context, p *platform.Platform) error) error {
	url := os.Getenv("TRELLIS_DATABASE_URL")
	if url == "" {
		return platform.ErrUsage("no_database", "TRELLIS_DATABASE_URL is not set", "export TRELLIS_DATABASE_URL=postgres://trellis@localhost:5432/trellis")
	}
	db, err := pgstore.Open(cmd.Context(), url)
	if err != nil {
		return platform.ErrUsage("no_database", "cannot open the database that TRELLIS_DATABASE_URL names: "+err.Error(), "check TRELLIS_DATABASE_URL")
	}
	defer db.Close()
	return fn(cmd.Context(), platform.New(db))
}

func addOrgFlag(cmd *cobra.Command, org *string) {
	cmd.Flags().StringVar(org, "org", "", "org name")
	cmd.MarkFlagRequired("org")
}

func newAdminOrgCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Create orgs"}
	cmd.AddCommand(&cobra.Command{
		Use:   "new <name>",
		Short: "Create an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				o, err := p.CreateOrg(ctx, args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, o, func() string { return "created org " + o.Name })
			})
		},
	})
	return cmd
}

func newAdminMemberCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "member", Short: "Add members to an org and set their org role"}
	var org, name string
	add := &cobra.Command{
		Use:   "add <handle>",
		Short: "Add a member to an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				m, err := p.AddMember(ctx, org, args[0], name)
				if err != nil {
					return err
				}
				return Emit(cmd, m, func() string { return fmt.Sprintf("added member %s to org %s", m.Handle, org) })
			})
		},
	}
	addOrgFlag(add, &org)
	add.Flags().StringVar(&name, "name", "", "display name")

	var roleOrg string
	role := &cobra.Command{
		Use:   "role <handle> <role>",
		Short: "Set a member's org role: " + strings.Join(platform.OrgRoles, " or "),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				if err := p.SetMemberRole(ctx, roleOrg, args[0], args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"org": roleOrg, "handle": args[0], "role": args[1]},
					func() string { return fmt.Sprintf("member %s is now %s in org %s", args[0], args[1], roleOrg) })
			})
		},
	}
	addOrgFlag(role, &roleOrg)
	cmd.AddCommand(add, role)
	return cmd
}

func newAdminProjectCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Create projects and manage who takes part in them"}

	var newOrg, name string
	create := &cobra.Command{
		Use:   "new <key>",
		Short: "Create a project in an org",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				pr, err := p.CreateProject(ctx, newOrg, args[0], name)
				if err != nil {
					return err
				}
				return Emit(cmd, pr, func() string { return fmt.Sprintf("created project %s in org %s", pr.Key, newOrg) })
			})
		},
	}
	addOrgFlag(create, &newOrg)
	create.Flags().StringVar(&name, "name", "", "display name")

	var addOrg, addRole string
	add := &cobra.Command{
		Use:   "add-member <key> <handle>",
		Short: "Add a member to a project; this creates the member's personal board in it",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				if err := p.AddProjectMember(ctx, addOrg, args[0], args[1], addRole); err != nil {
					return err
				}
				return Emit(cmd, platform.ProjectMember{Project: args[0], Handle: args[1], Role: addRole},
					func() string { return fmt.Sprintf("added %s to project %s as %s", args[1], args[0], addRole) })
			})
		},
	}
	addOrgFlag(add, &addOrg)
	add.Flags().StringVar(&addRole, "role", "member", "project role: "+strings.Join(platform.ProjectRoles, " or "))

	var roleOrg string
	role := &cobra.Command{
		Use:   "role <key> <handle> <role>",
		Short: "Set a member's project role: " + strings.Join(platform.ProjectRoles, " or "),
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				if err := p.SetProjectRole(ctx, roleOrg, args[0], args[1], args[2]); err != nil {
					return err
				}
				return Emit(cmd, platform.ProjectMember{Project: args[0], Handle: args[1], Role: args[2]},
					func() string { return fmt.Sprintf("%s is now %s in project %s", args[1], args[2], args[0]) })
			})
		},
	}
	addOrgFlag(role, &roleOrg)

	var rmOrg string
	remove := &cobra.Command{
		Use:   "remove-member <key> <handle>",
		Short: "Remove a member from a project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				if err := p.RemoveProjectMember(ctx, rmOrg, args[0], args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"project": args[0], "handle": args[1]},
					func() string { return fmt.Sprintf("removed %s from project %s", args[1], args[0]) })
			})
		},
	}
	addOrgFlag(remove, &rmOrg)

	var lsOrg string
	members := &cobra.Command{
		Use:   "members <key>",
		Short: "List the members of a project and their roles",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				ms, err := p.ProjectMembers(ctx, lsOrg, args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, ms, func() string {
					var b strings.Builder
					for _, m := range ms {
						fmt.Fprintf(&b, "%s\t%s\n", m.Handle, m.Role)
					}
					return strings.TrimSuffix(b.String(), "\n")
				})
			})
		},
	}
	addOrgFlag(members, &lsOrg)

	cmd.AddCommand(create, add, role, remove, members)
	return cmd
}

func newAdminTokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Issue, list and revoke a member's tokens"}

	var issueOrg, kind string
	issue := &cobra.Command{
		Use:   "issue <handle> <name>",
		Short: "Issue a token; it is shown once",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				secret, err := p.IssueToken(ctx, issueOrg, args[0], args[1], kind)
				if err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"handle": args[0], "name": args[1], "kind": kind, "token": secret},
					func() string { return secret + "\nthis token is shown once; store it now" })
			})
		},
	}
	addOrgFlag(issue, &issueOrg)
	issue.Flags().StringVar(&kind, "kind", platform.TokenAgent, "token kind: "+strings.Join(platform.TokenKinds, " or "))

	var lsOrg string
	ls := &cobra.Command{
		Use:   "ls <handle>",
		Short: "List a member's tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				ts, err := p.ListTokens(ctx, lsOrg, args[0])
				if err != nil {
					return err
				}
				return Emit(cmd, ts, func() string {
					var b strings.Builder
					for _, t := range ts {
						state := "live"
						if t.RevokedAt != nil {
							state = "revoked"
						}
						fmt.Fprintf(&b, "%s\t%s\t%s\n", t.Name, t.Kind, state)
					}
					return strings.TrimSuffix(b.String(), "\n")
				})
			})
		},
	}
	addOrgFlag(ls, &lsOrg)

	var revokeOrg string
	revoke := &cobra.Command{
		Use:   "revoke <handle> <name>",
		Short: "Revoke a member's token",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return adminRun(cmd, func(ctx context.Context, p *platform.Platform) error {
				if err := p.RevokeToken(ctx, revokeOrg, args[0], args[1]); err != nil {
					return err
				}
				return Emit(cmd, map[string]string{"handle": args[0], "name": args[1]},
					func() string { return fmt.Sprintf("revoked token %s of %s", args[1], args[0]) })
			})
		},
	}
	addOrgFlag(revoke, &revokeOrg)

	cmd.AddCommand(issue, ls, revoke)
	return cmd
}
