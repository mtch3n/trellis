package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
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
		Short: "Run the local Trellis application daemon",
		Long:  "Serve the embedded UI and API from one long-lived Trellis process.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApplicationServerContext(cmd.Context(), bind, port)
		},
		Args: cobra.NoArgs,
	}
	cmd.Flags().IntVar(&port, "port", 7788, "HTTP port")
	cmd.Flags().StringVar(&bind, "bind", "127.0.0.1", "HTTP bind address")
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
	if port == 0 {
		port = 7788
	}
	dbPath, err := home.DBPath()
	if err != nil {
		return err
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	root, err := home.Root()
	if err != nil {
		return err
	}
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
	c := core.New(db, core.RealClock{}, actor)
	if err := c.SyncKnowledgeSearch(parent); err != nil {
		return err
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	search := retrieval.NewService(c, db, dbPath, cfg)
	c.SetKnowledgeChanged(search.ReconcileProject)
	address := net.JoinHostPort(bind, fmt.Sprint(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := ui.NewServerWithSearch(c, db, address, search)
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	errorsCh := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(func() { errorsCh <- server.ServeContext(ctx, listener) })
	workers.Go(func() {
		errorsCh <- localdaemon.ServeContext(ctx, ipc, func(ctx context.Context, req localdaemon.Request) (localdaemon.Response, error) {
			switch req.Method {
			case "health":
				return localdaemon.Response{OK: true, Data: map[string]any{"url": server.URL(address)}}, nil
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
	fmt.Fprintf(os.Stdout, "trellis daemon listening on %s\n", server.URL(address))
	select {
	case <-ctx.Done():
	case err = <-errorsCh:
	}
	cancel()
	_ = ipc.Close()
	_ = listener.Close()
	workers.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
