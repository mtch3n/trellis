package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
)

func TestServe_EmbedsANewEntryWithTheConfiguredProvider(t *testing.T) {
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"embedding": [0.1, 0.2, 0.3]}`))
	}))
	defer provider.Close()

	url := pgtest.URL(t)
	srv := startServeEnv(t, map[string]string{
		"TRELLIS_DATABASE_URL": url, "TRELLIS_ADDR": "127.0.0.1:0",
		"TRELLIS_EMBED_PROVIDER": "http", "TRELLIS_EMBED_ENDPOINT": provider.URL,
		"TRELLIS_EMBED_MODEL": "fake", "TRELLIS_EMBED_DIMENSION": "3",
	})
	defer srv.stop()

	db := sqlx.MustOpen("pgx", url)
	defer db.Close()
	var entry uuid.UUID
	if err := db.Get(&entry, `WITH o AS (INSERT INTO org (name) VALUES ('acme') RETURNING id),
		p AS (INSERT INTO project (org_id, key) SELECT id, 'APP' FROM o RETURNING id, org_id),
		m AS (INSERT INTO member (org_id, handle) SELECT id, 'ann' FROM o RETURNING id)
		INSERT INTO entry (org_id, project_id, owner_id, slug, title, body, created_by)
		SELECT p.org_id, p.id, m.id, 'notes', 'notes', 'a body to embed', 'test' FROM p, m RETURNING id`); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		var chunks int
		if err := db.Get(&chunks, `SELECT count(*) FROM entry_chunk WHERE entry_id = $1`, entry); err != nil {
			t.Fatal(err)
		}
		if chunks > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no chunk after 30s; the provider was called %d times", calls.Load())
		}
		time.Sleep(200 * time.Millisecond)
	}
}
