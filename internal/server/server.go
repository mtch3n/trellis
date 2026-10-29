package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mtch3n/trellis/internal/mcpserver"
	"github.com/mtch3n/trellis/internal/pgstore"
	"github.com/mtch3n/trellis/internal/platform"
)

const (
	DefaultAddr = "127.0.0.1:8080"
	embedEvery  = 2 * time.Second
	coordEvery  = 10 * time.Minute
)

type Server struct {
	Platform *platform.Platform
	Mux      *http.ServeMux
}

func New(p *platform.Platform) *Server {
	s := &Server{Platform: p, Mux: http.NewServeMux()}
	s.Mux.HandleFunc("GET /healthz", s.healthz)
	s.Mux.Handle("/mcp", requireAgent(p, mcpserver.Handler(p)))
	registerWeb(s.Mux, p)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Mux.ServeHTTP(w, r)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Platform.Ping(r.Context()); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

func Run(ctx context.Context, getenv func(string) string, ready func(net.Addr)) error {
	dbURL := getenv("TRELLIS_DATABASE_URL")
	if dbURL == "" {
		return errors.New("TRELLIS_DATABASE_URL is not set: set it to a Postgres URL such as postgres://trellis@localhost:5432/trellis")
	}
	db, err := pgstore.Open(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("cannot open the database that TRELLIS_DATABASE_URL names: %w", err)
	}
	defer db.Close()

	embedConfig, err := platform.EmbedConfigFromEnv(getenv)
	if err != nil {
		return err
	}
	embedder, err := platform.NewEmbedder(embedConfig)
	if err != nil {
		return err
	}
	coordInterval, err := coordIntervalFromEnv(getenv)
	if err != nil {
		return err
	}
	p := platform.New(db)
	if embedder != nil {
		p.UseEmbedder(embedder)
	}

	addr := cmp.Or(getenv("TRELLIS_ADDR"), DefaultAddr)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on TRELLIS_ADDR %s: %w", addr, err)
	}
	srv := &http.Server{Handler: New(p), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	var embedding sync.WaitGroup
	defer embedding.Wait()
	embedCtx, stopEmbed := context.WithCancel(ctx)
	defer stopEmbed()
	embedding.Go(func() { p.RunEmbedder(embedCtx, embedEvery) })
	embedding.Go(func() { p.RunCoordinator(embedCtx, coordInterval) })
	ready(ln.Addr())

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func coordIntervalFromEnv(getenv func(string) string) (time.Duration, error) {
	raw := getenv("TRELLIS_COORD_INTERVAL")
	if raw == "" {
		return coordEvery, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("TRELLIS_COORD_INTERVAL must be a positive duration such as 10m, not %q", raw)
	}
	return d, nil
}
