package ui

import (
	"net/url"

	"github.com/mtch3n/trellis/internal/core"
)

// artifactItem is an entry's artifact as the UI receives it: the core reference
// plus the URL to fetch it from. Core does not know the UI's routes, so the URL
// is added here. A missing artifact has no URL.
type artifactItem struct {
	core.ArtifactRef
	URL string `json:"url,omitempty"`
}

// knowledgeItem is a knowledge entry as the UI list endpoints return it. Its
// Artifacts field shadows the embedded one; encoding/json/v2 resolves that in
// favour of the shallower field.
type knowledgeItem struct {
	core.Knowledge
	Artifacts []artifactItem `json:"artifacts,omitempty"`
}

func knowledgeItems(projectKey string, docs []core.Knowledge) []knowledgeItem {
	out := make([]knowledgeItem, len(docs))
	for i, d := range docs {
		out[i].Knowledge = d
		for _, a := range d.Artifacts {
			item := artifactItem{ArtifactRef: a}
			if !a.Missing {
				item.URL = artifactURL(projectKey, a.Name)
			}
			out[i].Artifacts = append(out[i].Artifacts, item)
		}
	}
	return out
}

func artifactURL(projectKey, name string) string {
	return "/api/p/" + url.PathEscape(projectKey) + "/artifacts/" + url.PathEscape(name)
}
