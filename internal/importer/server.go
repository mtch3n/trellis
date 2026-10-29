package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

var personalColumns = []localColumn{
	{Name: "backlog"}, {Name: "in-progress", Position: 1}, {Name: "review", Position: 2}, {Name: "done", Position: 3, Done: true},
}

type writer struct {
	tx        *sqlx.Tx
	who       member
	s         snapshot
	projectID string
	boards    map[string]string
	columns   map[string]string
	cards     map[string]string
	entries   map[string]string
	artifacts map[string]string
	moved     []string
}

func (w *writer) get(dst any, q string, args ...any) error {
	return w.tx.Get(dst, q, args...)
}

func (w *writer) exec(q string, args ...any) error {
	_, err := w.tx.Exec(q, args...)
	return err
}

func (w *writer) write() error {
	w.boards, w.columns, w.cards = map[string]string{}, map[string]string{}, map[string]string{}
	w.entries, w.artifacts = map[string]string{}, map[string]string{}
	steps := []struct {
		what string
		fn   func() error
	}{
		{"project", w.project},
		{"boards", w.writeBoards},
		{"cards", w.writeCards},
		{"artifacts", w.writeArtifacts},
		{"entries", w.writeEntries},
		{"links", w.writeLinks},
	}
	for _, st := range steps {
		if err := st.fn(); err != nil {
			return fmt.Errorf("%s: %w", st.what, err)
		}
	}
	return nil
}

func (w *writer) writeEntries() error {
	byID := slices.SortedFunc(slices.Values(w.s.Entries), func(a, b localEntry) int { return strings.Compare(a.ID, b.ID) })
	first := map[string]bool{}
	personal := map[string]bool{}
	for _, e := range byID {
		if e.Global {
			continue
		}
		key := strings.Join(strings.Fields(strings.ToLower(e.Body)), " ")
		if first[key] {
			personal[e.ID] = true
			continue
		}
		first[key] = true
	}
	for _, e := range w.s.Entries {
		if personal[e.ID] {
			w.moved = append(w.moved, w.s.Project.Key+"/"+e.Slug)
		}
		if err := w.writeEntry(e, personal[e.ID]); err != nil {
			return fmt.Errorf("entry %s: %w", e.Slug, err)
		}
	}
	return nil
}

func (w *writer) project() error {
	var counter int64
	for _, c := range w.s.Cards {
		counter = max(counter, c.Seq)
	}
	if err := w.get(&w.projectID, `INSERT INTO project (org_id, key, name, card_counter) VALUES ($1, $2, $3, $4) RETURNING id::text`,
		w.who.orgID, w.s.Project.Key, w.s.Project.Name, counter); err != nil {
		return err
	}
	return w.exec(`INSERT INTO project_member (org_id, project_id, member_id, role) VALUES ($1, $2, $3, 'maintainer')`,
		w.who.orgID, w.projectID, w.who.memberID)
}

func (w *writer) board(name, scope string, at time.Time, cols []localColumn) (string, error) {
	var id string
	if err := w.get(&id, `INSERT INTO board (project_id, owner_id, name, scope, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5) RETURNING id::text`,
		w.projectID, w.who.memberID, name, scope, at); err != nil {
		return "", err
	}
	for _, c := range cols {
		var colID string
		if err := w.get(&colID, `INSERT INTO board_column (board_id, name, position, done) VALUES ($1, $2, $3, $4) RETURNING id::text`,
			id, c.Name, c.Position, c.Done); err != nil {
			return "", err
		}
		if c.ID != "" {
			w.columns[c.ID] = colID
		}
	}
	return id, nil
}

func (w *writer) writeBoards() error {
	for _, b := range w.s.Boards {
		cols := slices.DeleteFunc(slices.Clone(w.s.Columns), func(c localColumn) bool { return c.BoardID != b.ID })
		id, err := w.board(b.Name, "project", time.UnixMilli(b.CreatedAt), cols)
		if err != nil {
			return fmt.Errorf("board %s: %w", b.Name, err)
		}
		w.boards[b.ID] = id
	}
	if slices.ContainsFunc(w.s.Boards, func(b localBoard) bool { return b.Name == w.who.handle }) {
		return nil
	}
	_, err := w.board(w.who.handle, "personal", time.Now(), personalColumns)
	return err
}

func (w *writer) writeCards() error {
	for _, c := range w.s.Cards {
		var id string
		if err := w.get(&id, `INSERT INTO card (project_id, board_id, column_id, number, ref, title, body, position, version, created_by, created_at, updated_at, archived_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id::text`,
			w.projectID, w.boards[c.BoardID], w.columns[c.ColumnID], c.Seq, c.Ref, c.Title, c.Body, c.Position, c.Version,
			w.who.actor(), time.UnixMilli(c.CreatedAt), time.UnixMilli(c.UpdatedAt), nullMilli(c.ArchivedAt)); err != nil {
			return fmt.Errorf("card %s: %w", c.Ref, err)
		}
		w.cards[c.ID] = id
	}
	for _, n := range w.s.Comments {
		if err := w.exec(`INSERT INTO comment (card_id, member_id, actor, body, created_at) VALUES ($1, $2, $3, $4, $5)`,
			w.cards[n.CardID], w.who.memberID, w.who.actor(), n.Body, time.UnixMilli(n.CreatedAt)); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) writeEntry(e localEntry, personal bool) error {
	scope, labels := "project", []string{"import"}
	switch {
	case e.Global:
		scope, labels = "global", []string{}
	case personal:
		scope = "personal"
	}
	var verifyBy any
	if e.Global {
		verifyBy = time.Now()
		if e.VerifyBy != nil {
			verifyBy = time.UnixMilli(*e.VerifyBy)
		}
	}
	provenance := e.Provenance
	if provenance == "" {
		provenance = "authored"
	}
	fields, err := json.Marshal(e.Fields)
	if err != nil {
		return err
	}
	sources := e.Sources
	if sources == nil {
		sources = []string{}
	}
	var id string
	if err := w.get(&id, `INSERT INTO entry (org_id, project_id, owner_id, scope, slug, title, summary, body, template, provenance, private,
			labels, sources, fields, recap, version, verify_by, verified_at, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21) RETURNING id::text`,
		w.who.orgID, w.projectID, w.who.memberID, scope, e.Slug, e.Title, e.Summary, e.Body, e.Template, provenance, e.Private,
		labels, sources, string(fields), e.Recap, e.Version, verifyBy, nullMilli(e.VerifiedAt), w.who.actor(),
		time.UnixMilli(e.CreatedAt), time.UnixMilli(e.UpdatedAt)); err != nil {
		return err
	}
	w.entries[e.ID] = id
	for _, r := range e.Revisions {
		meta, err := json.Marshal(map[string]any{"scope": scope, "template": e.Template, "provenance": provenance, "private": e.Private})
		if err != nil {
			return err
		}
		if err := w.exec(`INSERT INTO entry_revision (entry_id, version, title, summary, body, meta, actor, member_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, r.Version, r.Title, r.Summary, r.Body, string(meta), w.who.actor(), w.who.memberID, r.At); err != nil {
			return fmt.Errorf("revision %d: %w", r.Version, err)
		}
	}
	return nil
}

func (w *writer) writeArtifacts() error {
	for _, a := range w.s.Artifacts {
		sum := sha256.Sum256(a.Content)
		var id string
		if err := w.get(&id, `INSERT INTO artifact (org_id, project_id, owner_id, scope, name, kind, mime, size, content_hash, content, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, 'project', $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id::text`,
			w.who.orgID, w.projectID, w.who.memberID, a.Name, a.Kind, a.Mime, len(a.Content), hex.EncodeToString(sum[:]), a.Content,
			w.who.actor(), time.UnixMilli(a.CreatedAt), time.UnixMilli(a.UpdatedAt)); err != nil {
			return fmt.Errorf("artifact %s: %w", a.Name, err)
		}
		w.artifacts[a.ID] = id
	}
	return nil
}

func (w *writer) writeLinks() error {
	for _, l := range w.s.Links {
		if err := w.writeLink(l); err != nil {
			return fmt.Errorf("link %s %s: %w", l.Rel, l.ToRaw, err)
		}
	}
	for _, p := range w.s.Pins {
		var board *string
		if p.BoardID != nil {
			id, ok := w.boards[*p.BoardID]
			if !ok {
				continue
			}
			board = &id
		}
		e := w.entry(p.EntryID)
		if err := w.exec(`INSERT INTO pin (project_id, entry_id, board_id, recap, member_id, actor, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING`, w.projectID, w.entries[p.EntryID], board, e.Recap, w.who.memberID, w.who.actor(), time.UnixMilli(p.CreatedAt)); err != nil {
			return fmt.Errorf("pin %s: %w", e.Slug, err)
		}
	}
	for _, n := range w.s.Nominations {
		if err := w.exec(`INSERT INTO nomination (project_id, entry_id, member_id, actor, reason, created_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT DO NOTHING`, w.projectID, w.entries[n.EntryID], w.who.memberID, w.who.actor(), n.Reason, time.UnixMilli(n.CreatedAt)); err != nil {
			return fmt.Errorf("nomination %s: %w", w.entry(n.EntryID).Slug, err)
		}
	}
	return nil
}

func (w *writer) entry(id string) localEntry {
	i := slices.IndexFunc(w.s.Entries, func(e localEntry) bool { return e.ID == id })
	return w.s.Entries[i]
}

func (w *writer) writeLink(l localLink) error {
	if l.FromType == "entry" && l.Rel == "wikilink" {
		var to *string
		if l.ToID != nil {
			if id, ok := w.entries[*l.ToID]; ok {
				to = &id
			}
		}
		return w.exec(`INSERT INTO entry_link (from_entry_id, writer_id, rel, target, to_entry_id) VALUES ($1, $2, 'wikilink', $3, $4) ON CONFLICT DO NOTHING`,
			w.entries[l.FromID], w.who.memberID, l.ToRaw, to)
	}
	if l.ToID == nil {
		return nil
	}
	from := map[string]map[string]string{"card": w.cards, "entry": w.entries}[l.FromType][l.FromID]
	switch l.Rel {
	case "blocked_by":
		if to, ok := w.cards[*l.ToID]; ok {
			return w.exec(`INSERT INTO blocker (card_id, blocker_id, member_id, actor) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				from, to, w.who.memberID, w.who.actor())
		}
	case "cites":
		if to, ok := w.entries[*l.ToID]; ok {
			return w.exec(`INSERT INTO card_entry (card_id, entry_id, member_id, actor) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
				from, to, w.who.memberID, w.who.actor())
		}
	case "artifact":
		to, ok := w.artifacts[*l.ToID]
		if !ok {
			return nil
		}
		col := "card_id"
		if l.FromType == "entry" {
			col = "entry_id"
		}
		return w.exec(`INSERT INTO artifact_link (artifact_id, `+col+`, member_id, actor) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			to, from, w.who.memberID, w.who.actor())
	}
	return nil
}
