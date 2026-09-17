// Package retrieval contains the stable seams between search policy and
// concrete providers. It deliberately knows nothing about SQLite, QMD, or a
// particular model runtime.
package retrieval

import (
	"cmp"
	"context"
	"slices"
)

type Profile string

const (
	Entries  Profile = "entries"
	Memories Profile = "memories"
)

type Embedder interface {
	EmbedEntry(context.Context, string) ([]float32, error)
	EmbedQuery(context.Context, string) ([]float32, error)
	ModelID() string
	Dimension() int
}

type Candidate struct {
	ID       string
	Score    float64
	Source   string
	Metadata map[string]string
}

type Backend interface {
	Upsert(context.Context, Profile, []Candidate) error
	Delete(context.Context, Profile, []string) error
	Search(context.Context, Profile, string, int) ([]Candidate, error)
	Rebuild(context.Context, Profile) error
}

type Reranker interface {
	Rank(context.Context, string, []Candidate) ([]Candidate, error)
	ModelID() string
}

// ReciprocalRankFusion merges independent lexical/vector result lists while
// preserving exact-term matches. Scores are intentionally not compared across
// providers; rank is the portable signal.
func ReciprocalRankFusion(lists ...[]Candidate) []Candidate {
	const k = 60.0
	type aggregate struct {
		Candidate
		score float64
		best  int
	}
	merged := map[string]*aggregate{}
	for _, list := range lists {
		for rank, c := range list {
			if c.ID == "" {
				continue
			}
			a := merged[c.ID]
			if a == nil {
				a = &aggregate{Candidate: c, best: rank}
				merged[c.ID] = a
			}
			a.score += 1 / (k + float64(rank+1))
			if rank < a.best {
				a.best = rank
			}
		}
	}
	out := make([]Candidate, 0, len(merged))
	for _, a := range merged {
		a.Score = a.score
		out = append(out, a.Candidate)
	}
	slices.SortFunc(out, func(a, b Candidate) int { return cmp.Compare(b.Score, a.Score) })
	return out
}
