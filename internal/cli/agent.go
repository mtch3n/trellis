package cli

import (
	"cmp"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Agents seen by trellis"}
	cmd.AddCommand(newAgentLsCmd(), newAgentRemindCmd(), newAgentRegisterCmd())
	return cmd
}

// newAgentRegisterCmd records this session in the agent table. Contention
// triage (§8.5) can only say "message that handle" if someone wrote the handle
// down, so the SessionStart hook calls this once per session.
func newAgentRegisterCmd() *cobra.Command {
	var handle, kind string
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Record this session as an agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if handle == "" {
				handle = cmp.Or(os.Getenv("TRELLIS_AGENT_HANDLE"), "agent")
			}
			cwd, _ := os.Getwd()
			host, _ := os.Hostname()
			agent, err := c.RegisterAgent(cmd.Context(), handle, kind, cwd, host, int64(os.Getppid()))
			if err != nil {
				return err
			}
			return Emit(cmd, agent, func() string { return "registered " + agent.Handle })
		},
	}
	cmd.Flags().StringVar(&handle, "handle", "", "harness address for messaging (default $TRELLIS_AGENT_HANDLE)")
	cmd.Flags().StringVar(&kind, "kind", "agent", "agent, hook, human")
	return cmd
}

// newAgentLsCmd answers "who is active, where, holding what" (§8.6) — the
// triage view for a contended card.
func newAgentLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List agents and the cards they have claimed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			agents, err := c.ListAgents(cmd.Context())
			if err != nil {
				return err
			}
			return Emit(cmd, agents, func() string {
				var b strings.Builder
				w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "HANDLE\tKIND\tHOST\tPID\tLAST SEEN\tCWD")
				for _, a := range agents {
					fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n", a.Handle, a.Kind, a.Host, a.PID,
						since(a.LastSeen), a.CWD)
				}
				w.Flush()
				return strings.TrimRight(b.String(), "\n")
			})
		},
	}
}

func since(ms int64) string {
	d := time.Since(time.UnixMilli(ms)).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String() + " ago"
}

// newBackupCmd uses VACUUM INTO: copying ~/.trellis while a writer is active
// can capture a torn WAL (§5).
func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup <path>",
		Short: "Write a consistent copy of the database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := c.Backup(cmd.Context(), args[0]); err != nil {
				return core.ErrUsage("backup_failed", err.Error(),
					"trellis backup ~/trellis-backup.db  # the destination must not exist")
			}
			return Emit(cmd, map[string]string{"path": args[0]}, func() string {
				return "backed up to " + args[0]
			})
		},
	}
	cmd.AddCommand(newBackupPruneCmd())
	return cmd
}

func newBackupPruneCmd() *cobra.Command {
	var keep int
	cmd := &cobra.Command{
		Use: "prune <directory>", Short: "Keep only the newest named Trellis backups",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			deleted, err := c.BackupsPrune(args[0], keep)
			if err != nil {
				return err
			}
			return Emit(cmd, map[string]int{"deleted": deleted},
				func() string { return fmt.Sprintf("deleted %d old backups", deleted) })
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 5, "number of newest named backups to retain")
	return cmd
}

// newAgentRemindCmd is what the Stop hook runs: it prints a reminder for cards
// claimed with nothing written down, and nothing at all otherwise. Silence is the
// common case, and a hook that speaks every time gets ignored.
func newAgentRemindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remind",
		Short: "Report claimed cards with no comment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, db, err := openCore()
			if err != nil {
				return err
			}
			defer db.Close()
			cards, err := c.ClaimedWithoutComment(cmd.Context())
			if err != nil {
				return err
			}
			if len(cards) == 0 && !forceJSON {
				return nil
			}
			return Emit(cmd, map[string]any{"claimed_without_comment": cards}, func() string {
				var b strings.Builder
				b.WriteString("You still claim work with nothing written down:\n")
				for _, c := range cards {
					fmt.Fprintf(&b, "  %s  %s\n", c.Ref, c.Title)
				}
				b.WriteString("  trellis card comment <ref> --body \"...\"   # what you learned, then release")
				return b.String()
			})
		},
	}
}
