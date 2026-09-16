package retrieval

import (
	"context"
	"fmt"
	"time"

	"github.com/gofrs/flock"
	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/vector"
)

// Service is the one search/index boundary used by the CLI, UI and daemon.
// FTS and vector data are both derived from the same core source of truth.
type Service struct {
	core   *core.Core
	db     *sqlx.DB
	dbPath string
	cfg    config.Config
}

func NewService(c *core.Core, db *sqlx.DB, dbPath string, cfg config.Config) *Service {
	return &Service{core: c, db: db, dbPath: dbPath, cfg: cfg}
}

func (s *Service) vectorConfig(ctx context.Context, projectID string) (config.VectorSearchConfig, string, error) {
	get := func(key, fallback string) string {
		value, _, err := config.EffectiveValue(ctx, s.cfg, map[string]bool{}, config.RepoDoc{}, s.db, projectID, key)
		if err != nil {
			return fallback
		}
		return value
	}
	method := get("search.method", s.cfg.Search.Method)
	enabled := get("search.vector.enabled", "false") == "true"
	return config.VectorSearchConfig{
		Enabled: enabled, Provider: get("search.vector.provider", "command"),
		EmbedCommand: get("search.vector.embed_command", ""), Endpoint: get("search.vector.endpoint", ""),
		Model: get("search.vector.model", ""), Dimension: atoi(get("search.vector.dimension", "0")),
		Limit: atoi(get("search.vector.limit", "10")), ChunkSize: atoi(get("search.vector.chunk_size", "1200")),
		ChunkOverlap: atoi(get("search.vector.chunk_overlap", "200")),
	}, method, nil
}

func atoi(value string) int {
	var n int
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n
}

// Search applies the configured FTS, vector, or hybrid policy. A vector
// provider outage degrades to FTS because the vector index is derived state.
func (s *Service) Search(ctx context.Context, projectID, query string, opts core.SearchOpts) ([]core.SearchHit, error) {
	fts, err := s.core.Search(ctx, projectID, query, opts)
	if err != nil {
		return nil, err
	}
	vcfg, method, _ := s.vectorConfig(ctx, projectID)
	if opts.Method != "" {
		method = opts.Method
	} else if opts.AllProjects {
		// Each project supplies its own vector enablement and method below;
		// combine whichever semantic results are enabled with the global FTS set.
		method = "hybrid"
	}
	if method == "fts" || (!opts.AllProjects && !vcfg.Enabled) {
		return fts, nil
	}
	vectorHits, err := s.vectorHits(ctx, projectID, query, opts, vcfg)
	if err != nil {
		return fts, nil
	}
	if method == "vector" {
		return vectorHits, nil
	}
	return fuseHits(fts, vectorHits, opts.Limit), nil
}

func (s *Service) vectorHits(ctx context.Context, projectID, query string, opts core.SearchOpts, cfg config.VectorSearchConfig) ([]core.SearchHit, error) {
	projects := []string{projectID}
	if opts.AllProjects {
		if err := s.db.SelectContext(ctx, &projects, "SELECT id FROM project ORDER BY key"); err != nil {
			return nil, err
		}
	}
	var out []core.SearchHit
	seen := make(map[string]bool)
	for _, dataset := range projects {
		datasetCfg := cfg
		datasetMethod := opts.Method
		if opts.AllProjects {
			var err error
			var configuredMethod string
			datasetCfg, configuredMethod, err = s.vectorConfig(ctx, dataset)
			if err != nil {
				return nil, err
			}
			if datasetMethod == "" {
				datasetMethod = configuredMethod
			}
		}
		if !datasetCfg.Enabled || datasetMethod == "fts" {
			continue
		}
		var projectKey string
		if err := s.db.GetContext(ctx, &projectKey, `SELECT key FROM project WHERE id = ?`, dataset); err != nil {
			return nil, err
		}
		vectorPath, err := home.VectorDBPath(projectKey)
		if err != nil {
			return nil, err
		}
		var matches []vector.Hit
		err = withVectorLock(ctx, vectorPath, func() error {
			idx, err := vector.New(s.db.DB, vectorPath, datasetCfg)
			if err != nil {
				return err
			}
			defer idx.Close()
			if err := s.Reconcile(ctx, dataset, idx); err != nil {
				return err
			}
			matches, err = idx.Search(ctx, dataset, query, opts.Limit)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			var hit core.SearchHit
			query := `SELECT 'knowledge' AS kind,
		 CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref,
		 k.title, CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project,
		 k.doc_type AS detail, 0 AS unreviewed FROM knowledge k JOIN project p ON p.id = k.project_id
		 WHERE k.id = ? AND (k.project_id = ? OR k.global = 1)`
			if opts.AllProjects {
				query = `SELECT 'knowledge' AS kind, CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END || '/' || k.slug AS ref, k.title, CASE WHEN k.global = 1 THEN 'GLOBAL' ELSE p.key END AS project, k.doc_type AS detail, 0 AS unreviewed FROM knowledge k JOIN project p ON p.id = k.project_id WHERE k.id = ?`
			}
			args := []any{match.ID}
			if !opts.AllProjects {
				args = append(args, projectID)
			}
			if opts.Label != "" {
				query += ` AND EXISTS (SELECT 1 FROM knowledge_label kl JOIN label l ON l.id = kl.label_id WHERE kl.doc_id = k.id AND l.name = ?)`
				args = append(args, opts.Label)
			}
			if err := s.db.GetContext(ctx, &hit, query, args...); err != nil {
				continue
			}
			if !seen[hit.Ref] {
				out = append(out, hit)
				seen[hit.Ref] = true
			}
		}
	}
	return out, nil
}

func (s *Service) Reconcile(ctx context.Context, projectID string, idx *vector.Index) error {
	docs, err := s.core.ListSearchKnowledge(ctx, projectID)
	if err != nil {
		return err
	}
	items := make([]vector.Document, 0, len(docs))
	keep := make([]string, 0, len(docs))
	for _, doc := range docs {
		items = append(items, vector.Document{ID: doc.ID, Title: doc.Title, Slug: doc.Slug, DocType: doc.DocType, Content: doc.BodyMD})
		keep = append(keep, doc.ID)
	}
	if _, err := idx.Upsert(ctx, items, projectID); err != nil {
		return err
	}
	_, err = idx.Prune(ctx, keep, projectID)
	return err
}

func withVectorLock(ctx context.Context, path string, fn func() error) error {
	lock := flock.New(path + ".lock")
	for {
		ok, err := lock.TryLock()
		if err != nil {
			return err
		}
		if ok {
			defer lock.Unlock()
			return fn()
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) ReconcileProject(ctx context.Context, projectID string) error {
	cfg, _, err := s.vectorConfig(ctx, projectID)
	if err != nil || !cfg.Enabled {
		return err
	}
	var projectKey string
	if err := s.db.GetContext(ctx, &projectKey, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
		return err
	}
	vectorPath, err := home.VectorDBPath(projectKey)
	if err != nil {
		return err
	}
	idx, err := vector.New(s.db.DB, vectorPath, cfg)
	if err != nil {
		return err
	}
	defer idx.Close()
	return s.Reconcile(ctx, projectID, idx)
}

func (s *Service) projectIndex(ctx context.Context, projectID string, cfg config.VectorSearchConfig) (*vector.Index, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("vector search is disabled; set search.vector.enabled=true first")
	}
	var key string
	if err := s.db.GetContext(ctx, &key, "SELECT key FROM project WHERE id = ?", projectID); err != nil {
		return nil, err
	}
	path, err := home.VectorDBPath(key)
	if err != nil {
		return nil, err
	}
	return vector.New(s.db.DB, path, cfg)
}

func (s *Service) VectorRebuild(ctx context.Context, projectID string) (int, error) {
	cfg, _, err := s.vectorConfig(ctx, projectID)
	if err != nil {
		return 0, err
	}
	idx, err := s.projectIndex(ctx, projectID, cfg)
	if err != nil {
		return 0, err
	}
	defer idx.Close()
	docs, err := s.core.ListSearchKnowledge(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if err := s.Reconcile(ctx, projectID, idx); err != nil {
		return 0, err
	}
	return len(docs), nil
}

func (s *Service) VectorPrune(ctx context.Context, projectID string) (int, error) {
	cfg, _, err := s.vectorConfig(ctx, projectID)
	if err != nil {
		return 0, err
	}
	idx, err := s.projectIndex(ctx, projectID, cfg)
	if err != nil {
		return 0, err
	}
	defer idx.Close()
	docs, err := s.core.ListSearchKnowledge(ctx, projectID)
	if err != nil {
		return 0, err
	}
	keep := make([]string, 0, len(docs))
	for _, doc := range docs {
		keep = append(keep, doc.ID)
	}
	return idx.Prune(ctx, keep, projectID)
}

func (s *Service) VectorReindex(ctx context.Context, projectID string) (string, error) {
	cfg, _, err := s.vectorConfig(ctx, projectID)
	if err != nil {
		return "", err
	}
	idx, err := s.projectIndex(ctx, projectID, cfg)
	if err != nil {
		return "", err
	}
	defer idx.Close()
	return idx.Reindex(ctx, projectID)
}

func fuseHits(fts, semantic []core.SearchHit, limit int) []core.SearchHit {
	left := make([]Candidate, 0, len(fts))
	right := make([]Candidate, 0, len(semantic))
	byID := map[string]core.SearchHit{}
	for _, hit := range fts {
		left = append(left, Candidate{ID: hit.Ref})
		byID[hit.Ref] = hit
	}
	for _, hit := range semantic {
		right = append(right, Candidate{ID: hit.Ref})
		byID[hit.Ref] = hit
	}
	fused := ReciprocalRankFusion(left, right)
	if limit <= 0 {
		limit = 50
	}
	out := make([]core.SearchHit, 0, limit)
	for _, hit := range fused {
		out = append(out, byID[hit.ID])
		if len(out) == limit {
			break
		}
	}
	return out
}
