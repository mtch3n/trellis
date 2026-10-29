package server

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
)

var platTables = []string{
	"blocker", "board", "board_column", "card", "comment", "event", "invocation",
	"member", "org", "project", "project_member", "token",
}

type running struct {
	addr string
	stop func() error
}

func startServe(t *testing.T, dbURL string) running {
	t.Helper()
	return startServeEnv(t, map[string]string{"TRELLIS_DATABASE_URL": dbURL, "TRELLIS_ADDR": "127.0.0.1:0"})
}

func startServeEnv(t *testing.T, env map[string]string) running {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, func(k string) string { return env[k] }, func(a net.Addr) { ready <- a })
	}()
	select {
	case a := <-ready:
		return running{addr: a.String(), stop: func() error { cancel(); return <-done }}
	case err := <-done:
		cancel()
		t.Fatalf("serve stopped before it listened: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("serve did not listen within 30s")
	}
	return running{}
}

func publicTables(t *testing.T, db *sqlx.DB) []string {
	t.Helper()
	var names []string
	if err := db.Select(&names, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name`); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestPlat_PLAT_C1_ServeMigratesAnEmptyDatabaseAndAnswersHealthz(t *testing.T) {
	url := pgtest.URL(t)
	srv := startServe(t, url)
	defer srv.stop()

	resp, err := http.Get("http://" + srv.addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", resp.StatusCode)
	}

	db := sqlx.MustOpen("pgx", url)
	defer db.Close()
	tables := publicTables(t, db)
	for _, name := range platTables {
		if !slices.Contains(tables, name) {
			t.Errorf("table %s is missing after serve; tables: %v", name, tables)
		}
	}
}

func TestPlat_PLAT_C2_ServeOnAMigratedDatabaseRunsNoMigrationAndKeepsData(t *testing.T) {
	url := pgtest.URL(t)
	first := startServe(t, url)
	if err := first.stop(); err != nil {
		t.Fatalf("first serve: %v", err)
	}

	db := sqlx.MustOpen("pgx", url)
	defer db.Close()
	db.MustExec(`INSERT INTO org (name) VALUES ('acme')`)
	var applied []int64
	if err := db.Select(&applied, `SELECT version_id FROM goose_db_version ORDER BY id`); err != nil {
		t.Fatal(err)
	}

	second := startServe(t, url)
	if err := second.stop(); err != nil {
		t.Fatalf("second serve: %v", err)
	}

	var after []int64
	if err := db.Select(&after, `SELECT version_id FROM goose_db_version ORDER BY id`); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(applied, after) {
		t.Fatalf("a migration ran on the second start: before %v, after %v", applied, after)
	}
	var orgs []string
	if err := db.Select(&orgs, `SELECT name FROM org`); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(orgs, []string{"acme"}) {
		t.Fatalf("orgs after the second start = %v, want [acme]", orgs)
	}
}

func TestPlat_PLAT_C3_ServeWithoutAReachableDatabaseExitsNamingTheVariable(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := free.Addr().String()
	free.Close()

	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	for name, dbURL := range map[string]string{
		"unset":       "",
		"unreachable": "postgres://trellis:trellis@" + deadAddr + "/trellis?sslmode=disable&connect_timeout=2",
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{"TRELLIS_DATABASE_URL": dbURL, "TRELLIS_ADDR": addr}
			listened := false
			err := Run(t.Context(), func(k string) string { return env[k] }, func(net.Addr) { listened = true })
			if err == nil {
				t.Fatal("serve succeeded")
			}
			msg := err.Error()
			if !strings.Contains(msg, "TRELLIS_DATABASE_URL") {
				t.Fatalf("the message does not name TRELLIS_DATABASE_URL: %q", msg)
			}
			if strings.Contains(msg, "\n") {
				t.Fatalf("the message is more than one line: %q", msg)
			}
			if listened {
				t.Fatal("serve listened")
			}
			conn, dialErr := net.DialTimeout("tcp", addr, time.Second)
			if dialErr == nil {
				conn.Close()
				t.Fatalf("something listens on %s", addr)
			}
		})
	}
}
