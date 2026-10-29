package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mtch3n/trellis/internal/platform"
	"github.com/spf13/cobra"
)

var projectFlagKey string

type errExit struct{ code int }

func (errExit) Error() string { return "" }

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "trellis",
		Short:         "The Trellis server and its thin client",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetUsageTemplate(agentUsageTemplate)
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().BoolVar(&forceJSON, "json", false, "force JSON output")
	root.PersistentFlags().StringVar(&projectFlagKey, "project", "", "project key to act on, instead of the working directory")
	root.AddCommand(newServeCmd(), newAdminCmd(), newImportCmd(), newLoginCmd(), newCallCmd(), newInitCmd(), newUpdateCmd(), newVersionCmd())
	return root
}

func Execute() int {
	root := newRootCmd()
	err := root.Execute()
	if err == nil {
		return 0
	}
	if strings.HasPrefix(err.Error(), "unknown command") {
		err = unknownCommand(root, err)
	}
	if ee, ok := errors.AsType[errExit](err); ok {
		return ee.code
	}
	if pe, ok := errors.AsType[*platform.Error](err); ok {
		if forceJSON || !isTTY() {
			b, _ := json.Marshal(map[string]any{"error": pe})
			fmt.Fprintln(os.Stderr, string(b))
		} else {
			fmt.Fprintln(os.Stderr, "error: "+pe.Error())
		}
		return exitCode(pe.Kind)
	}
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	return 1
}

func exitCode(k platform.Kind) int {
	switch k {
	case platform.KindUsage:
		return 2
	case platform.KindNotFound:
		return 3
	case platform.KindConflict:
		return 4
	default:
		return 5
	}
}

func unknownCommand(root *cobra.Command, err error) error {
	name := "unknown"
	if _, rest, ok := strings.Cut(err.Error(), `unknown command "`); ok {
		name, _, _ = strings.Cut(rest, `"`)
	}
	msg := fmt.Sprintf("unknown command %q", name)
	root.SuggestionsMinimumDistance = 2
	if s := root.SuggestionsFor(name); len(s) > 0 {
		msg += fmt.Sprintf(" (did you mean: %s?)", s[0])
	}
	return platform.ErrUsage("unknown_command", msg, "trellis --help")
}
