// Package vector provides the optional semantic index for knowledge files.
// SQLite/FTS remains the default search path; this package is a derived cache.
package vector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/viant/sqlite-vec/engine"
	vec "github.com/viant/sqlite-vec/vec"
	"github.com/viant/sqlite-vec/vecadmin"
	vecencoding "github.com/viant/sqlite-vec/vector"
	"github.com/viant/sqlite-vec/vecutil"
)

type Document struct {
	ID      string
	Title   string
	Slug    string
	DocType string
	Content string
}

type chunk struct {
	ID      string
	RootID  string
	Title   string
	Slug    string
	DocType string
	Content string
}

type Hit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

type Index struct {
	db           *sql.DB
	vectorDB     *sql.DB
	virtualTable string
	shadowTable  string
	cfg          config.VectorSearchConfig
	embed        vecutil.EmbedFunc
	registered   bool
}

// sqlite-vec modules are installed on newly opened modernc connections. The
// CLI opens its primary database eagerly before constructing an Index, so the
// modules must be registered on the driver before store.Open creates that
// connection. The registrations are idempotent; New repeats them for callers
// that use a different driver setup.
func init() {
	_ = vec.Register(nil)
	_ = vecadmin.Register(nil)
}

var indexMu sync.Mutex

func (i *Index) Count(ctx context.Context, dataset string) (int, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	var n int
	err := i.vectorDB.QueryRowContext(ctx, "SELECT COUNT(DISTINCT COALESCE(json_extract(meta, '$.root_id'), id)) FROM "+i.shadowTable+" WHERE dataset_id = ?", dataset).Scan(&n)
	return n, err
}

func New(db *sql.DB, dbPath string, cfg config.VectorSearchConfig) (*Index, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	provider := cfg.Provider
	if provider == "" {
		provider = "command"
	}
	if provider == "command" && strings.TrimSpace(cfg.EmbedCommand) == "" {
		return nil, fmt.Errorf("vector search is enabled but search.vector.embed_command is empty")
	}
	if provider != "command" && provider != "http" && provider != "local" {
		return nil, fmt.Errorf("unsupported vector provider %q; use command, http, or local", provider)
	}
	if cfg.Dimension <= 0 {
		return nil, fmt.Errorf("search.vector.dimension must be positive when vector search is enabled")
	}
	virtualTable := tableName(dbPath)
	i := &Index{db: db, cfg: cfg, virtualTable: virtualTable, shadowTable: vecutil.ShadowTableName(virtualTable)}
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("vector index requires a file-backed database path")
	}
	vectorDB, err := engine.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open vector database handle: %w", err)
	}
	vectorDB.SetMaxOpenConns(1)
	_, _ = vectorDB.Exec("PRAGMA busy_timeout=10000; PRAGMA journal_mode=WAL")
	i.vectorDB = vectorDB
	i.embed = providerEmbed(cfg)
	if err := vec.Register(db); err != nil && !strings.Contains(err.Error(), "already registered") {
		vectorDB.Close()
		return nil, fmt.Errorf("register sqlite-vec: %w", err)
	}
	// The shadow handle also needs the extension's invalidation scalar because
	// its triggers may be created lazily by sqlite-vec.
	if err := vec.Register(vectorDB); err != nil && !strings.Contains(err.Error(), "already registered") {
		vectorDB.Close()
		return nil, fmt.Errorf("register sqlite-vec shadow handle: %w", err)
	}
	if err := vecadmin.Register(db); err != nil && !strings.Contains(err.Error(), "already registered") {
		vectorDB.Close()
		return nil, fmt.Errorf("register sqlite-vec admin: %w", err)
	}
	path := strings.ReplaceAll(dbPath, "'", "''")
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS " + i.virtualTable + " USING vec(doc_id, dbpath='" + path + "')")
	if err != nil {
		vectorDB.Close()
		return nil, fmt.Errorf("create vector table: %w", err)
	}
	// sqlite-vec creates its shadow schema lazily when the virtual table is
	// connected. Touch it once before vecutil writes directly to that schema.
	var initialized int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+i.virtualTable+" WHERE dataset_id = ?", "__trellis_init__").Scan(&initialized); err != nil {
		// The extension creates its shadow lazily on a MATCH query. Create the
		// stable payload schema explicitly so rebuild/prune can run before the
		// first semantic query.
		createShadow := `CREATE TABLE IF NOT EXISTS ` + i.shadowTable + ` (dataset_id TEXT NOT NULL, id TEXT NOT NULL, content TEXT, meta TEXT, embedding BLOB, PRIMARY KEY(dataset_id, id)); CREATE TABLE IF NOT EXISTS vector_storage (shadow_table_name TEXT NOT NULL, dataset_id TEXT NOT NULL DEFAULT '', "index" BLOB, PRIMARY KEY(shadow_table_name, dataset_id)); CREATE TABLE IF NOT EXISTS vector_storage_locks (shadow_table_name TEXT NOT NULL, dataset_id TEXT NOT NULL DEFAULT '', owner TEXT NOT NULL, locked_at INTEGER NOT NULL, PRIMARY KEY(shadow_table_name, dataset_id));`
		if _, schemaErr := vectorDB.Exec(createShadow); schemaErr != nil {
			vectorDB.Close()
			return nil, fmt.Errorf("initialize vector table: %w", schemaErr)
		}
	}
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS " + "vec_admin_" + strings.TrimPrefix(i.virtualTable, "vec_knowledge_") + " USING vec_admin(op, dbpath='" + path + "')")
	if err != nil {
		vectorDB.Close()
		return nil, fmt.Errorf("create vector admin table: %w", err)
	}
	i.registered = true
	return i, nil
}

func tableName(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "vec_knowledge_" + hex.EncodeToString(sum[:])[:16]
}

func (i *Index) Close() error {
	if i == nil || i.vectorDB == nil {
		return nil
	}
	return i.vectorDB.Close()
}

// Compact checkpoints and vacuums the disposable per-project vector store.
func (i *Index) Compact(ctx context.Context) error {
	if i == nil || i.vectorDB == nil {
		return fmt.Errorf("vector index is not initialized")
	}
	if _, err := i.vectorDB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint vector WAL: %w", err)
	}
	if _, err := i.vectorDB.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum vector database: %w", err)
	}
	return nil
}

func providerEmbed(cfg config.VectorSearchConfig) vecutil.EmbedFunc {
	provider := cfg.Provider
	if provider == "" {
		provider = "command"
	}
	if provider == "http" || provider == "local" {
		return httpEmbed(cfg, provider == "local")
	}
	return commandEmbed(cfg)
}

func commandEmbed(cfg config.VectorSearchConfig) vecutil.EmbedFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		cmd := exec.CommandContext(ctx, cfg.EmbedCommand)
		cmd.Stdin = strings.NewReader(text)
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("run embedding command %q: %w", cfg.EmbedCommand, err)
		}
		var raw []float32
		if err := json.Unmarshal(out, &raw); err != nil {
			var wrapped struct {
				Embedding []float32 `json:"embedding"`
			}
			if wrapErr := json.Unmarshal(out, &wrapped); wrapErr != nil {
				return nil, fmt.Errorf("embedding command must print a JSON array or embedding object: %w", err)
			}
			raw = wrapped.Embedding
		}
		if len(raw) != cfg.Dimension {
			return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(raw), cfg.Dimension)
		}
		return raw, nil
	}
}

func httpEmbed(cfg config.VectorSearchConfig, local bool) vecutil.EmbedFunc {
	endpoint := cfg.Endpoint
	if endpoint == "" && local {
		endpoint = "http://127.0.0.1:11434/api/embeddings"
	}
	return func(ctx context.Context, text string) ([]float32, error) {
		if endpoint == "" {
			return nil, fmt.Errorf("vector provider %q requires search.vector.endpoint", cfg.Provider)
		}
		var payload any
		if local {
			payload = map[string]string{"model": cfg.Model, "prompt": text}
		} else {
			payload = map[string]any{"model": cfg.Model, "input": text}
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("embedding request: %w", err)
		}
		defer resp.Body.Close()
		responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("embedding request returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
		}
		var result struct {
			Embedding []float32 `json:"embedding"`
			Data      []struct {
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(responseBody, &result); err != nil {
			return nil, fmt.Errorf("decode embedding response: %w", err)
		}
		raw := result.Embedding
		if len(raw) == 0 && len(result.Data) > 0 {
			raw = result.Data[0].Embedding
		}
		if len(raw) != cfg.Dimension {
			return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(raw), cfg.Dimension)
		}
		return raw, nil
	}
}

func splitDocument(d Document, size, overlap int) []chunk {
	content := d.Title + "\n" + d.DocType + "\n" + d.Content
	if size <= 0 || len([]rune(content)) <= size {
		return []chunk{{ID: d.ID + ":0", RootID: d.ID, Title: d.Title, Slug: d.Slug, DocType: d.DocType, Content: content}}
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 6
	}
	runes := []rune(content)
	step := size - overlap
	out := make([]chunk, 0, (len(runes)+step-1)/step)
	for start, n := 0, 0; start < len(runes); start, n = start+step, n+1 {
		end := min(start+size, len(runes))
		out = append(out, chunk{ID: fmt.Sprintf("%s:%d", d.ID, n), RootID: d.ID, Title: d.Title, Slug: d.Slug, DocType: d.DocType, Content: string(runes[start:end])})
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (i *Index) Upsert(ctx context.Context, docs []Document, dataset string) (int, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	return i.upsert(ctx, docs, dataset)
}

func (i *Index) upsert(ctx context.Context, docs []Document, dataset string) (int, error) {
	if i == nil || !i.registered {
		return 0, fmt.Errorf("vector index is not initialized")
	}
	type replacement struct {
		doc    Document
		chunks []chunk
	}
	var replacements []replacement
	for _, d := range docs {
		chunks := splitDocument(d, i.cfg.ChunkSize, i.cfg.ChunkOverlap)
		unchanged, err := i.documentUnchanged(ctx, dataset, d, chunks)
		if err != nil {
			return 0, err
		}
		if !unchanged {
			replacements = append(replacements, replacement{doc: d, chunks: chunks})
		}
	}
	if len(replacements) == 0 {
		return 0, nil
	}
	// Compute every replacement before deleting its old rows. A provider error
	// therefore leaves the previous derived index usable.
	type encodedChunk struct {
		chunk
		meta string
		blob []byte
	}
	encoded := make([][]encodedChunk, len(replacements))
	for n, r := range replacements {
		encoded[n] = make([]encodedChunk, 0, len(r.chunks))
		for _, c := range r.chunks {
			meta, _ := json.Marshal(map[string]string{"root_id": c.RootID, "title": c.Title, "slug": c.Slug, "type": c.DocType})
			emb, err := i.embed(ctx, c.Content)
			if err != nil {
				return 0, err
			}
			blob, err := vecencoding.EncodeEmbedding(emb)
			if err != nil {
				return 0, err
			}
			encoded[n] = append(encoded[n], encodedChunk{chunk: c, meta: string(meta), blob: blob})
		}
	}
	tx, err := i.vectorDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM vector_storage WHERE shadow_table_name = ? AND dataset_id = ?`, "main."+i.shadowTable, dataset); err != nil {
		return 0, err
	}
	n := 0
	for dIndex, r := range replacements {
		// Remove all previous chunks first. A document that shrinks must not
		// leave its old tail chunks in the derived index.
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+i.shadowTable+` WHERE dataset_id = ? AND (id = ? OR id LIKE ? || ':%')`, dataset, r.doc.ID, r.doc.ID); err != nil {
			return n, err
		}
		for _, c := range encoded[dIndex] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+i.shadowTable+`(dataset_id, id, content, meta, embedding) VALUES (?, ?, ?, ?, ?) ON CONFLICT(dataset_id, id) DO UPDATE SET content=excluded.content, meta=excluded.meta, embedding=excluded.embedding`, dataset, c.ID, c.Content, c.meta, c.blob); err != nil {
				return n, err
			}
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func (i *Index) documentUnchanged(ctx context.Context, dataset string, d Document, chunks []chunk) (bool, error) {
	rows, err := i.vectorDB.QueryContext(ctx, `SELECT id, content, meta FROM `+i.shadowTable+` WHERE dataset_id = ? AND (id = ? OR id LIKE ? || ':%')`, dataset, d.ID, d.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	existing := make(map[string][2]string, len(chunks))
	for rows.Next() {
		var id, content, meta string
		if err := rows.Scan(&id, &content, &meta); err != nil {
			return false, err
		}
		existing[id] = [2]string{content, meta}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(existing) != len(chunks) {
		return false, nil
	}
	for _, c := range chunks {
		meta, _ := json.Marshal(map[string]string{"root_id": c.RootID, "title": c.Title, "slug": c.Slug, "type": c.DocType})
		old, ok := existing[c.ID]
		if !ok || old[0] != c.Content || old[1] != string(meta) {
			return false, nil
		}
	}
	return true, nil
}

func (i *Index) Delete(ctx context.Context, ids []string, dataset string) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	return i.delete(ctx, ids, dataset)
}

func (i *Index) delete(ctx context.Context, ids []string, dataset string) error {
	if err := i.prepareShadowWrite(ctx, dataset); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := i.vectorDB.ExecContext(ctx, `DELETE FROM `+i.shadowTable+` WHERE dataset_id = ? AND (id = ? OR id LIKE ? || ':%')`, dataset, id, id); err != nil {
			return err
		}
	}
	return nil
}

func (i *Index) prepareShadowWrite(ctx context.Context, dataset string) error {
	// Invalidate explicitly because direct shadow writes bypass the virtual-table
	// module. Keep sqlite-vec's triggers intact: they protect against writes from
	// another process and call the same invalidation path after each change.
	if _, err := i.vectorDB.ExecContext(ctx, `DELETE FROM vector_storage WHERE shadow_table_name = ? AND dataset_id = ?`, "main."+i.shadowTable, dataset); err != nil {
		return err
	}
	return nil
}

func (i *Index) Search(ctx context.Context, dataset, query string, limit int) ([]Hit, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	if limit <= 0 {
		limit = i.cfg.Limit
	}
	if limit <= 0 {
		limit = i.cfg.Limit
	}
	ids, err := vecutil.MatchText(ctx, i.db, i.virtualTable, i.embed, dataset, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		var meta string
		if err := i.vectorDB.QueryRowContext(ctx, `SELECT meta FROM `+i.shadowTable+` WHERE dataset_id = ? AND id = ?`, dataset, id).Scan(&meta); err != nil {
			return nil, err
		}
		var m struct {
			RootID string `json:"root_id"`
		}
		_ = json.Unmarshal([]byte(meta), &m)
		root := m.RootID
		if root == "" {
			root = id
		}
		if !seen[root] {
			out = append(out, Hit{ID: root})
			seen[root] = true
		}
	}
	return out, nil
}

func (i *Index) Rebuild(ctx context.Context, docs []Document, dataset string) (int, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	if _, err := i.upsert(ctx, docs, dataset); err != nil {
		return 0, err
	}
	keep := make([]string, 0, len(docs))
	for _, d := range docs {
		keep = append(keep, d.ID)
	}
	if _, err := i.prune(ctx, keep, dataset); err != nil {
		return 0, err
	}
	return len(docs), nil
}

func (i *Index) Prune(ctx context.Context, keep []string, dataset string) (int, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	return i.prune(ctx, keep, dataset)
}

func (i *Index) prune(ctx context.Context, keep []string, dataset string) (int, error) {
	rows, err := i.vectorDB.QueryContext(ctx, "SELECT id, meta FROM "+i.shadowTable+" WHERE dataset_id = ?", dataset)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	want := make(map[string]bool, len(keep))
	for _, id := range keep {
		want[id] = true
	}
	var stale []string
	for rows.Next() {
		var id, meta string
		if err := rows.Scan(&id, &meta); err != nil {
			return 0, err
		}
		var m struct {
			RootID string `json:"root_id"`
		}
		_ = json.Unmarshal([]byte(meta), &m)
		root := m.RootID
		if root == "" {
			root = id
		}
		if !want[root] {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := i.delete(ctx, stale, dataset); err != nil {
		return 0, err
	}
	return len(stale), nil
}

func (i *Index) Reindex(ctx context.Context, dataset string) (string, error) {
	indexMu.Lock()
	defer indexMu.Unlock()
	// Dropping the persisted slice is the portable administrative operation;
	// sqlite-vec rebuilds it lazily on the next MATCH query.
	if _, err := i.vectorDB.ExecContext(ctx, `DELETE FROM vector_storage WHERE shadow_table_name = ? AND dataset_id = ?`, "main."+i.shadowTable, dataset); err != nil {
		return "", err
	}
	return "invalidated; rebuilt lazily on next vector search", nil
}
