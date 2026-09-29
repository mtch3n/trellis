package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SearchTrash finds trashed cards and entries in a project whose text holds
// every word of query, newest first. The trash has no index: it is small,
// short-lived, and searched only on request.
func (c *Core) SearchTrash(ctx context.Context, projectID, query string, limit int) ([]SearchHit, error) {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil, nil
	}
	items, err := c.Trash(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	for _, it := range items {
		if limit > 0 && len(hits) >= limit {
			break
		}
		if it.Kind != TrashCard && it.Kind != TrashEntry {
			continue
		}
		snap, err := decodeSnapshot(it.Rows)
		if err != nil {
			return nil, err
		}
		row := it.rootRow(snap)
		text := []string{it.Title}
		hit := SearchHit{Kind: it.Kind, Title: it.Title, Project: it.ProjectKey, TrashedAt: &it.TrashedAt}
		switch it.Kind {
		case TrashCard:
			hit.Ref = it.Name
			body, _ := row["body_md"].(string)
			text = append(text, body)
		case TrashEntry:
			global, _ := row["global"].(int64)
			hit.Ref = EntryAddress(it.ProjectKey, global == 1, it.Name)
			hit.Detail, _ = row["template"].(string)
			summary, _ := row["summary"].(string)
			text = append(text, summary, c.trashedEntryBody(it))
		}
		if containsAll(strings.ToLower(strings.Join(text, "\n")), words) {
			hits = append(hits, hit)
		}
	}
	return hits, nil
}

// trashedEntryBody reads a trashed entry's file from the trash.
func (c *Core) trashedEntryBody(it TrashItem) string {
	var files []trashFile
	if json.Unmarshal([]byte(it.Files), &files) != nil || len(files) == 0 {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(c.root, files[0].To))
	if err != nil {
		return ""
	}
	return string(raw)
}

func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}
