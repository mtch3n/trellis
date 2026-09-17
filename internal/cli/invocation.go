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

// redactArgv keeps the resolved command path and the flag names. Positional
// arguments are dropped wholesale: they are card refs, file paths and bodies,
// and the command path is what the metric groups by. An unresolvable command
// is recorded as the raw first token, which is the whole point of measuring
// typos.
func redactArgv(args []string) string {
	parts := []string{}
	if cmd, _, err := newRootCmd().Find(args); err == nil {
		parts = append(parts, strings.Fields(cmd.CommandPath())[1:]...)
	} else if len(args) > 0 {
		parts = append(parts, args[0])
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			name, _, _ := strings.Cut(a, "=")
			parts = append(parts, name)
		}
	}
	return strings.Join(parts, " ")
}
