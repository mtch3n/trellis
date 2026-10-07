package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
)

// logInvocation records what was run and how it exited. The adoption metric in
// P2 is the exit-code-2 rate per command: an agent that cannot work the CLI
// shows up here as usage errors, not as a complaint.
//
// Flag VALUES are deliberately not stored — only the command path and the flag
// names. The values are card bodies and pasted command output, which the metric
// does not need and which trellis does not scan (§14.1).
func logInvocation(args []string, exit int, started time.Time) {
	if os.Getenv("TRELLIS_NO_LOG") != "" {
		return
	}
	// Logging never breaks a command, and never creates or migrates the
	// database either: `trellis --help` from a newer build must not be what
	// upgrades it.
	root, err := home.Root()
	if err != nil {
		return
	}
	db, err := store.OpenCurrent(filepath.Join(root, "trellis.db"))
	if err != nil {
		return
	}
	defer db.Close()
	_ = core.New(db, core.RealClock{}, cliActor(), root).LogInvocation(context.Background(), redactArgv(args), exit,
		time.Since(started).Milliseconds())
}

// redactArgv keeps the resolved command path and the names of the flags that
// command defines. Positional arguments are dropped wholesale: they are card
// refs, file paths and bodies, and the command path is what the metric groups
// by. A token is recorded as a flag only when the command knows it, so a
// prompt or body that happens to start with "-" is still a positional and is
// never stored. Everything after "--" is positional. An unresolvable command is
// recorded as the raw first token, which is the whole point of measuring typos.
func redactArgv(args []string) string {
	cmd, _, err := newRootCmd().Find(args)
	if err != nil {
		if len(args) > 0 && !strings.ContainsAny(args[0], " \t\n") {
			return args[0]
		}
		return ""
	}
	cmd.InitDefaultHelpFlag() // cobra adds --help only when a command runs
	parts := strings.Fields(cmd.CommandPath())[1:]
	for _, a := range args {
		if a == "--" {
			break
		}
		name, _, _ := strings.Cut(a, "=")
		var known bool
		switch {
		case strings.HasPrefix(name, "--"):
			n := strings.TrimPrefix(name, "--")
			known = cmd.Flags().Lookup(n) != nil || cmd.InheritedFlags().Lookup(n) != nil
		case strings.HasPrefix(name, "-") && len(name) == 2:
			n := name[1:]
			known = cmd.Flags().ShorthandLookup(n) != nil || cmd.InheritedFlags().ShorthandLookup(n) != nil
		}
		if known {
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, " ")
}
