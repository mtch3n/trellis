package platform

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string
}

type EmbedConfig struct {
	Provider  string
	Command   string
	Endpoint  string
	Model     string
	Dimension int
}

const (
	embedChunkSize    = 1200
	embedChunkOverlap = 200
	embedBatch        = 50
	localEmbedURL     = "http://127.0.0.1:11434/api/embeddings"
)

func EmbedConfigFromEnv(getenv func(string) string) (EmbedConfig, error) {
	c := EmbedConfig{
		Provider: getenv("TRELLIS_EMBED_PROVIDER"),
		Command:  getenv("TRELLIS_EMBED_COMMAND"),
		Endpoint: getenv("TRELLIS_EMBED_ENDPOINT"),
		Model:    getenv("TRELLIS_EMBED_MODEL"),
	}
	if c.Provider == "" {
		return c, nil
	}
	dim, err := strconv.Atoi(getenv("TRELLIS_EMBED_DIMENSION"))
	if err != nil || dim <= 0 {
		return c, errors.New("TRELLIS_EMBED_DIMENSION must be a positive number when TRELLIS_EMBED_PROVIDER is set")
	}
	c.Dimension = dim
	return c, nil
}

func NewEmbedder(c EmbedConfig) (Embedder, error) {
	switch c.Provider {
	case "":
		return nil, nil
	case "command":
		if strings.TrimSpace(c.Command) == "" {
			return nil, errors.New("TRELLIS_EMBED_PROVIDER is command but TRELLIS_EMBED_COMMAND is empty")
		}
		return commandEmbedder{c}, nil
	case "http":
		if c.Endpoint == "" {
			return nil, errors.New("TRELLIS_EMBED_PROVIDER is http but TRELLIS_EMBED_ENDPOINT is empty")
		}
		return httpEmbedder{c}, nil
	case "local":
		c.Endpoint = cmp.Or(c.Endpoint, localEmbedURL)
		return httpEmbedder{c}, nil
	default:
		return nil, fmt.Errorf("unsupported TRELLIS_EMBED_PROVIDER %q; use command, http or local", c.Provider)
	}
}

type commandEmbedder struct{ c EmbedConfig }

func (e commandEmbedder) Model() string { return "command:" + cmp.Or(e.c.Model, e.c.Command) }

func (e commandEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	cmd := exec.CommandContext(ctx, e.c.Command)
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run embedding command %q: %w", e.c.Command, err)
	}
	var raw []float32
	if err := json.Unmarshal(out, &raw); err != nil {
		var wrapped struct {
			Embedding []float32 `json:"embedding"`
		}
		if json.Unmarshal(out, &wrapped) != nil {
			return nil, fmt.Errorf("embedding command must print a JSON array or embedding object: %w", err)
		}
		raw = wrapped.Embedding
	}
	return checkDimension(raw, e.c.Dimension)
}

type httpEmbedder struct{ c EmbedConfig }

func (e httpEmbedder) Model() string { return e.c.Provider + ":" + e.c.Model }

func (e httpEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	var payload any = map[string]any{"model": e.c.Model, "input": text}
	if e.c.Provider == "local" {
		payload = map[string]string{"model": e.c.Model, "prompt": text}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embedding request returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		Embedding []float32 `json:"embedding"`
		Data      []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	raw := result.Embedding
	if len(raw) == 0 && len(result.Data) > 0 {
		raw = result.Data[0].Embedding
	}
	return checkDimension(raw, e.c.Dimension)
}

func checkDimension(v []float32, dim int) ([]float32, error) {
	if len(v) != dim {
		return nil, fmt.Errorf("embedding dimension %d does not match configured dimension %d", len(v), dim)
	}
	return v, nil
}

func (p *Platform) UseEmbedder(e Embedder) {
	p.embedder = e
}

func (p *Platform) RunEmbedder(ctx context.Context, every time.Duration) {
	if p.embedder == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if _, err := p.EmbedPending(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("embed entries", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

type pendingEntry struct {
	ID       uuid.UUID `db:"id"`
	Version  int64     `db:"version"`
	Title    string    `db:"title"`
	Template string    `db:"template"`
	Body     string    `db:"body"`
}

func (p *Platform) EmbedPending(ctx context.Context) (int, error) {
	if p.embedder == nil {
		return 0, nil
	}
	var pending []pendingEntry
	if err := p.db.SelectContext(ctx, &pending, `SELECT id, version, title, template, body FROM entry
		WHERE embedded_version < version AND deleted_at IS NULL
		ORDER BY embed_tried_at NULLS FIRST, updated_at LIMIT $1`, embedBatch); err != nil {
		return 0, err
	}
	done := 0
	for _, e := range pending {
		if err := p.embedEntry(ctx, e); err != nil {
			if _, markErr := p.db.ExecContext(ctx, `UPDATE entry SET embed_error = $2, embed_tried_at = now() WHERE id = $1`, e.ID, err.Error()); markErr != nil {
				return done, errors.Join(err, markErr)
			}
			return done, fmt.Errorf("embed entry %s: %w", e.ID, err)
		}
		done++
	}
	return done, nil
}

func (p *Platform) embedEntry(ctx context.Context, e pendingEntry) error {
	chunks := splitChunks(e.Title+"\n"+e.Template+"\n"+e.Body, embedChunkSize, embedChunkOverlap)
	vectors := make([]string, len(chunks))
	for i, text := range chunks {
		v, err := p.embedder.Embed(ctx, text)
		if err != nil {
			return err
		}
		vectors[i] = vectorLiteral(v)
	}
	model := p.embedder.Model()
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`UPDATE entry SET embedded_version = version, embed_error = NULL, embed_tried_at = now()
			WHERE id = $1 AND version = $2 AND deleted_at IS NULL`, e.ID, e.Version)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM entry_chunk WHERE entry_id = $1`, e.ID); err != nil {
			return err
		}
		for seq, text := range chunks {
			if _, err := tx.Exec(`INSERT INTO entry_chunk (entry_id, version, seq, text, model, embedding)
				VALUES ($1, $2, $3, $4, $5, $6::vector)`, e.ID, e.Version, seq, text, model, vectors[seq]); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Platform) embedQuery(ctx context.Context, query string) (string, error) {
	v, err := p.embedder.Embed(ctx, query)
	if err != nil {
		return "", err
	}
	return vectorLiteral(v), nil
}

func splitChunks(content string, size, overlap int) []string {
	runes := []rune(content)
	if len(runes) <= size {
		return []string{content}
	}
	step := size - overlap
	var out []string
	for start := 0; start < len(runes); start += step {
		out = append(out, string(runes[start:min(start+size, len(runes))]))
		if start+size >= len(runes) {
			break
		}
	}
	return out
}

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
