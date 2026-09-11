package cli

import (
	"encoding/json/v2"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var forceJSON bool

// Emit writes v as JSON when stdout is not a terminal or --json was given,
// and the table form otherwise.
func Emit(cmd *cobra.Command, v any, table func() string) error {
	if forceJSON || !isTTY() {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		cmd.OutOrStdout().Write(append(b, '\n'))
		return nil
	}
	cmd.OutOrStdout().Write([]byte(table() + "\n"))
	return nil
}

func isTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// agentUsageTemplate is cobra's default with the trailing
// `Use "x [command] --help" ...` footer removed. Help is capped at 25 lines
// (§12) and that footer is two lines of boilerplate an agent already knows;
// spending them on actual commands is the better trade. Set on the root
// command, it is inherited by every subcommand.
const agentUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}
Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`
