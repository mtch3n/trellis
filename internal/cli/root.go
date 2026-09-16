// Package cli is a thin shell over internal/core. No policy lives here.
package cli

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/resolve"
	"github.com/mtch3n/trellis/internal/retrieval"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/spf13/cobra"
)

type appCtx struct {
	Core    *core.Core
	Project core.Project
	Board   core.Board
	db      *sqlx.DB
}

// boardFlag holds --board; empty means "apply the selection rules".
var boardFlag string

// actorSuffix holds --as, distinguishing parallel subagents that share a
// session id. It is not a persistent flag: it is registered on the commands
// where ownership is at stake, so it does not crowd every unrelated command's
// help with a flag that has no effect there.
var actorSuffix string

// projectFlagKey holds --project; empty means "resolve from the working
// directory". An agent working across several repositories in one session
// would otherwise have to cd before every call.
var projectFlagKey string

func openCore() (*core.Core, *sqlx.DB, error) {
	path, err := home.DBPath()
	if err != nil {
		return nil, nil, err
	}
	db, err := store.Open(path)
	if err != nil {
		return nil, nil, err
	}
	actor := os.Getenv("TRELLIS_AGENT")
	if actor == "" {
		actor = fmt.Sprintf("cli:%d", os.Getpid())
	}
	// Every subagent in one Claude Code session shares the session id (§8.5),
	// so without a suffix two of them are the same principal and can claim the
	// same card twice. --as wins over TRELLIS_ACTOR because it is per command.
	if sub := cmp.Or(actorSuffix, os.Getenv("TRELLIS_ACTOR")); sub != "" {
		actor += "/" + sub
	}
	c := core.New(db, core.RealClock{}, actor)
	if err := c.SyncKnowledgeSearch(context.Background()); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("rebuild knowledge search: %w", err)
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	if ttl, err := time.ParseDuration(cfg.Lease.TTL); err == nil {
		c.SetLeaseTTL(ttl.Milliseconds())
	}
	c.SetDefaultColumns(cfg.Board.DefaultColumns)
	c.SetCardRequirements(cfg.Labels.RequireOnCard, cfg.Tags.RequireOnCard)
	search := retrieval.NewService(c, db, path, cfg)
	c.SetKnowledgeChanged(search.ReconcileProject)
	return c, db, nil
}

// currentBoard resolves the working directory to a project and then to a
// board. There is no cwd fallback: outside a git repository with no pin this
// exits 2 rather than silently creating a project.
func currentBoard() (*appCtx, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}

	key := projectKey()
	var p core.Project
	if key != "" {
		// Naming a project skips cwd resolution entirely: --project must work
		// from outside any repository.
		if p, err = c.ProjectByKey(context.Background(), key); err != nil {
			db.Close()
			return nil, err
		}
	} else {
		dir, err := os.Getwd()
		if err != nil {
			db.Close()
			return nil, err
		}
		// Identify returns a plain error: resolve cannot import core without an
		// import cycle. Exit codes are a CLI concern, so the wrapping happens here.
		id, err := resolve.Identify(dir)
		if err != nil {
			db.Close()
			return nil, core.ErrUsage("unresolved", err.Error(), "trellis init --pin")
		}
		if p, err = c.EnsureProject(context.Background(), id); err != nil {
			db.Close()
			return nil, err
		}
	}

	// --board wins; otherwise TRELLIS_BOARD; otherwise the selection rules in
	// core.SelectBoard (sole board, then the default, else exit 2).
	requested := cmp.Or(boardFlag, os.Getenv("TRELLIS_BOARD"))
	b, err := c.SelectBoard(context.Background(), p.ID, requested)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &appCtx{Core: c, Project: p, Board: b, db: db}, nil
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "trellis",
		Short:         "Local kanban and knowledge base for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetUsageTemplate(agentUsageTemplate)
	// `completion` is shell plumbing cobra generates, not a Trellis capability;
	// it still works when invoked. Every actual command stays listed: an agent
	// only discovers what `--help` shows it.
	root.CompletionOptions.HiddenDefaultCmd = true
	root.PersistentFlags().BoolVar(&forceJSON, "json", false, "force JSON output")
	root.PersistentFlags().StringVar(&boardFlag, "board", "", "board to act on")
	root.PersistentFlags().StringVar(&projectFlagKey, "project", "", "project key to act on, instead of the working directory")
	// All commands registered here once; each lives in its own file so later
	// parallel tasks never edit root.go.
	root.AddCommand(newInitCmd(), newCardCmd(), newBoardCmd(), newColumnCmd(), newLabelCmd(), newUICmd(), newSearchCmd(), newRecallCmd(), newConfigCmd(), newAgentCmd(), newBackupCmd(), newVersionCmd(), newUpdateCmd(),
		newKnowledgeCmd(), newArtifactCmd(), newLinkCmd(), newGraphCmd(), newVectorCmd(), newDaemonCmd(), newDoctorCmd(), newMaintenanceCmd(), newTUICmd(), newEventsCmd())
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	started := time.Now()
	code := run()
	logInvocation(os.Args[1:], code, started)
	return code
}

func run() int {
	root := newRootCmd()
	err := root.Execute()
	if err == nil {
		return 0
	}

	// Handle unknown command errors specially: get suggestions programmatically
	// and emit them through the structured error path so they parse as JSON.
	if isUnknownCommandError(err) {
		cmd, suggestions := extractCommandAndSuggestions(root, err)
		msg := fmt.Sprintf("unknown command %q", cmd)
		if len(suggestions) > 0 {
			msg += fmt.Sprintf(" (did you mean: %s?)", suggestions[0])
		}
		err = core.ErrUsage("unknown_command", msg, "trellis --help")
	}

	// doctor already printed its report; it only needs the exit code.
	if ee, ok := errors.AsType[errExit](err); ok {
		return ee.code
	}

	if te, ok := errors.AsType[*core.Error](err); ok {
		if forceJSON || !isTTY() {
			b, _ := json.Marshal(map[string]any{"error": map[string]string{
				"code": te.Code, "message": te.Msg, "fix": te.Fix}})
			fmt.Fprintln(os.Stderr, string(b))
		} else {
			fmt.Fprintln(os.Stderr, "error: "+te.Error())
		}
		return te.Exit
	}
	// Unknown error type - print as-is and exit 1
	fmt.Fprintln(os.Stderr, "error: "+err.Error())
	return 1
}

// isUnknownCommandError checks if the error is cobra's unknown command error.
func isUnknownCommandError(err error) bool {
	if err == nil {
		return false
	}
	// Cobra includes suggestions in the error message, so check prefix
	return strings.HasPrefix(err.Error(), "unknown command")
}

// extractCommandAndSuggestions extracts the command name from an unknown command
// error and returns it along with any suggestions from the root command.
func extractCommandAndSuggestions(root *cobra.Command, err error) (string, []string) {
	errMsg := err.Error()
	// Error format: unknown command "NAME" for "trellis"
	start := len("unknown command \"")
	if len(errMsg) > start {
		end := start
		for end < len(errMsg) && errMsg[end] != '"' {
			end++
		}
		if end < len(errMsg) {
			cmd := errMsg[start:end]
			// Set minimum distance to allow suggestions
			if root.SuggestionsMinimumDistance <= 0 {
				root.SuggestionsMinimumDistance = 2
			}
			suggestions := root.SuggestionsFor(cmd)
			return cmd, suggestions
		}
	}
	return "unknown", nil
}

// configInt reads a project-effective integer setting, falling back to def when
// the value is missing or unparseable: a bad setting must not break a listing.
func configInt(ctx context.Context, app *appCtx, key string, def int) int {
	cfg, present, err := config.LoadWithPresence()
	if err != nil {
		cfg = config.Defaults()
		present = map[string]bool{}
	}
	raw, _, err := config.EffectiveValue(ctx, cfg, present, config.RepoDoc{}, app.db, app.Project.ID, key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// projectKey is the project named by --project or $TRELLIS_PROJECT; empty means
// "resolve from the working directory". A bare --project names no project.
func projectKey() string {
	return cmp.Or(projectFlagKey, os.Getenv("TRELLIS_PROJECT"))
}

// addActorFlag registers --as on a command whose effect depends on who is
// acting: claiming, releasing, noting and editing all record or check an owner.
func addActorFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&actorSuffix, "as", "",
		"act as this subagent, distinct from others in the same session")
}
