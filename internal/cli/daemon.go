package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	localdaemon "github.com/mtch3n/trellis/internal/daemon"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/retrieval"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/mtch3n/trellis/internal/ui"
	"github.com/spf13/cobra"
)

// newDaemonCmd is the first daemon slice: one process owns the database and
// serves the embedded UI/API. Search/model workers will attach to this
// lifecycle next; direct FTS remains available from the CLI in the meantime.
func newDaemonCmd() *cobra.Command {
	var port int
	var bind string
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run and manage the local Trellis daemon",
		Long: "Serve the embedded UI and API from one long-lived Trellis process.\n" +
			"Run bare, it serves in the foreground. Use the subcommands to install it\n" +
			"as a login service and to start, stop and restart it in the background.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApplicationServerContext(cmd.Context(), bind, port)
		},
		Args: cobra.NoArgs,
	}
	cmd.Flags().IntVar(&port, "port", 7788, "HTTP port")
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "HTTP bind address")
	// Bare `trellis daemon` stays a foreground server: that is what the
	// installed unit executes. The subcommands supervise it from outside.
	cmd.AddCommand(newDaemonInstallCmd(), newDaemonUninstallCmd(), newDaemonStartCmd(),
		newDaemonStopCmd(), newDaemonRestartCmd(), newDaemonStatusCmd())
	return cmd
}

func runApplicationServer(bind string, port int) error {
	return runApplicationServerContext(context.Background(), bind, port)
}

func runApplicationServerContext(parent context.Context, bind string, port int) error {
	if bind == "" {
		bind = "127.0.0.1"
	}
	if bind == "localhost" {
		bind = "127.0.0.1"
	}
	if ip := net.ParseIP(bind); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("daemon bind address must be loopback")
	}
	// Port 0 asks the OS for any free port; the health call reports the one it
	// got. Callers resolve the configured ui.port before they get here.
	if port < 0 || port > 65535 {
		return fmt.Errorf("daemon port %d is out of range", port)
	}
	root, err := home.Root()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(root, "trellis.db")
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	lock := flock.New(root + "/daemon.lock")
	ok, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("acquire daemon lock: %w", err)
	}
	if !ok {
		return fmt.Errorf("trellis daemon is already running")
	}
	defer lock.Unlock()
	ipc, cleanupIPC, err := localdaemon.Listen(root)
	if err != nil {
		return fmt.Errorf("listen daemon IPC: %w", err)
	}
	defer cleanupIPC()
	actor := os.Getenv("TRELLIS_AGENT")
	if actor == "" {
		actor = fmt.Sprintf("daemon:%d", os.Getpid())
	}
	c := core.New(db, core.RealClock{}, actor, root)
	if err := c.SyncKnowledgeSearch(parent); err != nil {
		return err
	}
	cfg, cfgErr := config.Load(root)
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	// openCore (internal/cli/root.go) primes every CLI invocation's Core with
	// these same three settings from the global config; the daemon's Core
	// must get them too, before the server is built, or a web claim always
	// gets the built-in 30-minute lease and web card creation skips
	// labels.require_on_card / tags.require_on_card, whatever the config
	// file or a project override says.
	if ttl, err := time.ParseDuration(cfg.Lease.TTL); err == nil {
		c.SetLeaseTTL(ttl.Milliseconds())
	}
	c.SetDefaultColumns(cfg.Board.DefaultColumns)
	c.SetCardRequirements(cfg.Labels.RequireOnCard, cfg.Tags.RequireOnCard)
	c.SetHistoryKeep(cfg.History.EffectiveKeep())
	search := retrieval.NewService(c, db, dbPath, cfg, root)
	c.SetKnowledgeChanged(search.ReconcileProject)
	c.SetDropDerived(search.DropProject)
	address := net.JoinHostPort(bind, fmt.Sprint(port))
	// ui.enabled off means the daemon is IPC-only: agents keep the shared
	// database, search index and lease clock, and nothing binds a TCP port.
	var listener net.Listener
	if cfg.UI.UIEnabled() {
		if listener, err = net.Listen("tcp", address); err != nil {
			return err
		}
		defer listener.Close()
	}
	server := ui.NewServerWithSearch(c, db, address, search)
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	errorsCh := make(chan error, 2)
	var workers sync.WaitGroup
	if listener != nil {
		workers.Go(func() { errorsCh <- server.ServeContext(ctx, listener) })
	}
	workers.Go(func() {
		errorsCh <- localdaemon.ServeContext(ctx, ipc, func(ctx context.Context, req localdaemon.Request) (localdaemon.Response, error) {
			switch req.Method {
			case "health":
				// An IPC-only daemon reports an empty url, which is how the
				// CLI tells "no daemon" from "daemon without a web UI".
				url := ""
				if listener != nil {
					actualAddress := listener.Addr().String()
					url = server.URL(actualAddress)
				}
				return localdaemon.Response{OK: true, Data: map[string]any{"url": url, "ui_enabled": listener != nil}}, nil
			case "search":
				hits, err := search.Search(ctx, req.ProjectID, req.Query, core.SearchOpts{Method: req.SearchMethod, Limit: req.Limit, AllProjects: req.AllProjects, Label: req.Label})
				if err != nil {
					return localdaemon.Response{}, err
				}
				return localdaemon.Response{OK: true, Results: hits}, nil
			case "vector_rebuild":
				n, err := search.VectorRebuild(ctx, req.ProjectID)
				return localdaemon.Response{OK: err == nil, Data: map[string]any{"rebuilt": n}}, err
			case "vector_prune":
				n, err := search.VectorPrune(ctx, req.ProjectID)
				return localdaemon.Response{OK: err == nil, Data: map[string]any{"pruned": n}}, err
			case "vector_reindex":
				result, err := search.VectorReindex(ctx, req.ProjectID)
				return localdaemon.Response{OK: err == nil, Data: map[string]any{"result": result}}, err
			default:
				return localdaemon.Response{}, fmt.Errorf("unknown daemon method %q", req.Method)
			}
		})
	})
	workers.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var projects []string
				if db.SelectContext(ctx, &projects, "SELECT id FROM project") == nil {
					for _, projectID := range projects {
						_ = search.ReconcileProject(ctx, projectID)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	})
	if listener != nil {
		fmt.Fprintf(os.Stdout, "trellis daemon listening on %s\n", server.URL(address))
	} else {
		fmt.Fprintln(os.Stdout, "trellis daemon listening on local IPC only (ui.enabled is false)")
	}
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
	}
	cancel()
	_ = ipc.Close()
	if listener != nil {
		_ = listener.Close()
	}
	workers.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
