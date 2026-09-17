package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/daemon"
	"github.com/mtch3n/trellis/internal/home"
	vecsearch "github.com/mtch3n/trellis/internal/vector"
	"github.com/spf13/cobra"
)

func newVectorCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vector", Short: "Manage the optional document vector index"}
	cmd.AddCommand(newVectorStatusCmd(), newVectorRebuildCmd(), newVectorPruneCmd(), newVectorReindexCmd(), newVectorCompactCmd())
	return cmd
}

func effectiveVectorConfig(ctx context.Context, db *sqlx.DB, projectID string) (config.VectorSearchConfig, error) {
	cfg := config.Defaults()
	present := map[string]bool{}
	if root, err := home.Root(); err == nil {
		if loaded, loadedPresent, err := config.LoadWithPresence(root); err == nil {
			cfg, present = loaded, loadedPresent
		}
	}
	// No search.vector.* key is repository-safe (Global Constraints), so an
	// empty RepoDoc is correct here, not a placeholder to fill in later.
	get := func(key, fallback string) string {
		v, _, e := config.EffectiveValue(ctx, cfg, present, config.RepoDoc{}, db, projectID, key)
		if e != nil {
			return fallback
		}
		return v
	}
	enabled, err := strconv.ParseBool(get("search.vector.enabled", "false"))
	if err != nil {
		enabled = false
	}
	dim, _ := strconv.Atoi(get("search.vector.dimension", "0"))
	chunkSize, _ := strconv.Atoi(get("search.vector.chunk_size", "1200"))
	chunkOverlap, _ := strconv.Atoi(get("search.vector.chunk_overlap", "200"))
	lim, _ := strconv.Atoi(get("search.vector.limit", "10"))
	return config.VectorSearchConfig{Enabled: enabled, Provider: get("search.vector.provider", "command"), EmbedCommand: get("search.vector.embed_command", ""), Endpoint: get("search.vector.endpoint", ""), Model: get("search.vector.model", ""), Dimension: dim, Limit: lim, ChunkSize: chunkSize, ChunkOverlap: chunkOverlap}, nil
}

func currentVector() (*vecsearch.Index, *projectContext, error) {
	pctx, err := currentProject()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := effectiveVectorConfig(context.Background(), pctx.db, pctx.Project.ID)
	if err != nil {
		pctx.db.Close()
		return nil, nil, err
	}
	path, err := home.VectorDBPath(pctx.Project.Key)
	if err != nil {
		pctx.db.Close()
		return nil, nil, err
	}
	idx, err := vecsearch.New(pctx.db.DB, path, cfg)
	if err != nil {
		pctx.db.Close()
		return nil, nil, err
	}
	return idx, pctx, nil
}

func knowledgeVectorDocs(docs []core.Knowledge) []vecsearch.Document {
	out := make([]vecsearch.Document, 0, len(docs))
	for _, d := range docs {
		out = append(out, vecsearch.Document{ID: d.ID, Title: d.Title, Slug: d.Slug, Template: d.Template, Content: d.BodyMD})
	}
	return out
}

func newVectorStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show vector configuration and index health", RunE: func(cmd *cobra.Command, _ []string) error {
		pctx, err := currentProject()
		if err != nil {
			return err
		}
		defer pctx.db.Close()
		cfg, err := effectiveVectorConfig(cmd.Context(), pctx.db, pctx.Project.ID)
		if err != nil {
			return err
		}
		if !cfg.Enabled {
			return Emit(cmd, map[string]any{"enabled": false, "configured_documents": 0, "indexed_documents": 0, "stale_documents": 0}, func() string { return "vector disabled (FTS search remains active)" })
		}
		idx, err := currentVectorFrom(pctx, cfg)
		if err != nil {
			return err
		}
		defer idx.Close()
		docs, err := pctx.Core.ListSearchKnowledge(cmd.Context(), pctx.Project.ID)
		if err != nil {
			return err
		}
		count, err := idx.Count(cmd.Context(), pctx.Project.ID)
		if err != nil {
			return err
		}
		staleCount := len(docs) - count
		if staleCount < 0 {
			staleCount = 0
		}
		return Emit(cmd, map[string]any{"enabled": true, "configured_documents": len(docs), "indexed_documents": count, "stale_documents": staleCount}, func() string { return fmt.Sprintf("vector enabled; %d/%d documents indexed", count, len(docs)) })
	}}
}

func currentVectorFrom(pctx *projectContext, cfg config.VectorSearchConfig) (*vecsearch.Index, error) {
	path, err := home.VectorDBPath(pctx.Project.Key)
	if err != nil {
		return nil, err
	}
	return vecsearch.New(pctx.db.DB, path, cfg)
}

func newVectorRebuildCmd() *cobra.Command {
	var useDaemon bool
	cmd := &cobra.Command{Use: "rebuild", Short: "Embed and rebuild the current project's document index", RunE: func(cmd *cobra.Command, _ []string) error {
		if useDaemon {
			return vectorDaemon(cmd, "vector_rebuild")
		}
		idx, pctx, err := currentVector()
		if err != nil {
			return err
		}
		if idx == nil {
			return fmt.Errorf("vector search is disabled; set search.vector.enabled=true first")
		}
		defer idx.Close()
		defer pctx.db.Close()
		docs, err := pctx.Core.ListSearchKnowledge(cmd.Context(), pctx.Project.ID)
		if err != nil {
			return err
		}
		n, err := idx.Rebuild(cmd.Context(), knowledgeVectorDocs(docs), pctx.Project.ID)
		if err != nil {
			return err
		}
		return Emit(cmd, map[string]any{"rebuilt": n}, func() string { return fmt.Sprintf("rebuilt %d document vectors", n) })
	}}
	cmd.Flags().BoolVar(&useDaemon, "daemon", false, "run through the application daemon")
	return cmd
}

func newVectorPruneCmd() *cobra.Command {
	var useDaemon bool
	cmd := &cobra.Command{Use: "prune", Short: "Remove vectors for documents no longer in the knowledge base", RunE: func(cmd *cobra.Command, _ []string) error {
		if useDaemon {
			return vectorDaemon(cmd, "vector_prune")
		}
		idx, pctx, err := currentVector()
		if err != nil {
			return err
		}
		if idx == nil {
			return fmt.Errorf("vector search is disabled; set search.vector.enabled=true first")
		}
		defer idx.Close()
		defer pctx.db.Close()
		docs, err := pctx.Core.ListSearchKnowledge(cmd.Context(), pctx.Project.ID)
		if err != nil {
			return err
		}
		keep := make([]string, 0, len(docs))
		for _, d := range docs {
			keep = append(keep, d.ID)
		}
		n, err := idx.Prune(cmd.Context(), keep, pctx.Project.ID)
		if err != nil {
			return err
		}
		return Emit(cmd, map[string]any{"pruned": n}, func() string { return fmt.Sprintf("pruned %d stale document vectors", n) })
	}}
	cmd.Flags().BoolVar(&useDaemon, "daemon", false, "run through the application daemon")
	return cmd
}

func newVectorReindexCmd() *cobra.Command {
	var useDaemon bool
	cmd := &cobra.Command{Use: "reindex", Short: "Force the extension to rebuild its persisted ANN cache", RunE: func(cmd *cobra.Command, _ []string) error {
		if useDaemon {
			return vectorDaemon(cmd, "vector_reindex")
		}
		idx, pctx, err := currentVector()
		if err != nil {
			return err
		}
		if idx == nil {
			return fmt.Errorf("vector search is disabled; set search.vector.enabled=true first")
		}
		defer idx.Close()
		defer pctx.db.Close()
		result, err := idx.Reindex(cmd.Context(), pctx.Project.ID)
		if err != nil {
			return err
		}
		return Emit(cmd, map[string]any{"result": result}, func() string { return result })
	}}
	cmd.Flags().BoolVar(&useDaemon, "daemon", false, "run through the application daemon")
	return cmd
}

func newVectorCompactCmd() *cobra.Command {
	return &cobra.Command{Use: "compact", Short: "Checkpoint and VACUUM the current project vector database", RunE: func(cmd *cobra.Command, _ []string) error {
		idx, pctx, err := currentVector()
		if err != nil {
			return err
		}
		if idx == nil {
			return fmt.Errorf("vector search is disabled; set search.vector.enabled=true first")
		}
		defer idx.Close()
		defer pctx.db.Close()
		if err := idx.Compact(cmd.Context()); err != nil {
			return err
		}
		return Emit(cmd, map[string]string{"status": "compacted"}, func() string { return "project vector database compacted" })
	}}
}

func vectorDaemon(cmd *cobra.Command, method string) error {
	pctx, err := currentProject()
	if err != nil {
		return err
	}
	defer pctx.db.Close()
	root, err := home.Root()
	if err != nil {
		return err
	}
	resp, err := daemon.Call(cmd.Context(), daemon.Endpoint(root), daemon.Request{Method: method, ProjectID: pctx.Project.ID})
	if err != nil {
		return err
	}
	return Emit(cmd, resp.Data, func() string { return fmt.Sprint(resp.Data) })
}
