// Package cli is a thin shell over internal/core. No policy lives here.
package cli

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/retrieval"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/spf13/cobra"
)

type appCtx struct {
	Core    *core.Core
	Project core.Project
	Board   core.Board
	db      *sqlx.DB
	cfg     config.Config
}

// boardFlag holds --board; empty means "apply the selection rules".
var boardFlag string

// actorSuffix holds --as, distinguishing parallel subagents that share a
// session id. It is not a persistent flag: it is registered on the commands
// where ownership is at stake, so it does not crowd every unrelated command's
// help with a flag that has no effect there.
var actorSuffix string

// projectFlagKey holds --project; empty means "resolve from the nearest
// .trellis pin". An agent working across several repositories in one session
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
	c.ApplyConfig(cfg)
	search := retrieval.NewService(c, db, path, cfg)
	c.SetKnowledgeChanged(search.ReconcileProject)
	c.SetDropDerived(search.DropProject)
	return c, db, nil
}

// currentBoard resolves the project and then the board. Standing where no pin
// applies exits 2 rather than creating anything.
func currentBoard() (*appCtx, error) {
	c, db, err := openCore()
	if err != nil {
		return nil, err
	}
	return boardForCore(context.Background(), c, db)
}

// boardForCore resolves the project and board for an already-open Core, and
// layers the repository config over the global one. It is shared by
// currentBoard and, for the branch of targetContext that resolves the pinned
// project, targetContext itself: a qualified card ref or address naming the
// same project a pin would have chosen must not skip the repository file
// that a bare reference reads. It closes db on any error.
func boardForCore(ctx context.Context, c *core.Core, db *sqlx.DB) (*appCtx, error) {
	r, err := resolveProject(ctx, c)
	if err != nil {
		db.Close()
		return nil, err
	}
	b, err := selectBoard(ctx, c, r)
	if err != nil {
		db.Close()
		return nil, err
	}
	effective, err := applyRepoConfig(c, r)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &appCtx{Core: c, Project: r.Project, Board: b, db: db, cfg: effective}, nil
}

// applyRepoConfig loads the repository file beside r's pin, if any, layers it
// over the global config, applies the effective settings to c, and returns
// the effective config for app.cfg.
//
// openCore already applied the Core's lease TTL, default columns, label/tag
// requirements and history retention from the global file alone. Once the
// pin that chose the project (if any) is known, this re-derives the same
// settings with the repository file beside it layered in, and re-applies
// them with the same c.ApplyConfig openCore used: a repo-safe key wins over
// the global file. history.keep is not repo-safe, so ApplyConfig's call here
// re-applies the value the global file already gave -- a no-op, not a second
// source of truth. A project named by --project or TRELLIS_PROJECT has no
// pin and reads no repository file.
func applyRepoConfig(c *core.Core, r resolvedProject) (config.Config, error) {
	var repoDir string
	if r.Pin != nil {
		repoDir = filepath.Dir(r.Pin.Path)
	}
	cfg, _, cfgErr := config.LoadWithPresence()
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	repo, _, _, repoErr := config.LoadRepo(repoDir)
	if repoErr != nil {
		return config.Config{}, core.ErrUsage("bad_repo_config", repoErr.Error(), "fix the file .trellis.yaml/.trellis.yml names")
	}
	effective := config.ApplyRepoOverrides(cfg, repo)
	c.ApplyConfig(effective)
	return effective, nil
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
	root.AddCommand(newInitCmd(), newProjectCmd(), newCardCmd(), newBoardCmd(), newColumnCmd(), newLabelCmd(), newUICmd(), newSearchCmd(), newRecallCmd(), newConfigCmd(), newAgentCmd(), newBackupCmd(), newVersionCmd(), newUpdateCmd(),
		newKnowledgeCmd(), newArtifactCmd(), newLinkCmd(), newGraphCmd(), newVectorCmd(), newDaemonCmd(), newDoctorCmd(), newMaintenanceCmd(), newTUICmd(), newEventsCmd(), newExtensionCmd())
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
			body := map[string]any{"code": te.Code, "message": te.Msg, "fix": te.Fix}
			if len(te.Problems) > 0 {
				body["problems"] = te.Problems
			}
			b, _ := json.Marshal(map[string]any{"error": body})
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
	raw, _, err := config.EffectiveValue(ctx, app.cfg, map[string]bool{}, config.RepoDoc{}, app.db, app.Project.ID, key)
	if err != nil {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// projectNamed reports whether --project or $TRELLIS_PROJECT names the
// project, rather than a pin in the working directory.
func projectNamed() bool {
	return projectFlagKey != "" || os.Getenv("TRELLIS_PROJECT") != ""
}

// addActorFlag registers --as on a command whose effect depends on who is
// acting: claiming, releasing, noting and editing all record or check an owner.
func addActorFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&actorSuffix, "as", "",
		"act as this subagent, distinct from others in the same session")
}
