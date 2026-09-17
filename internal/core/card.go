package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

type Card struct {
	ID         string   `db:"id" json:"id"`
	ProjectID  string   `db:"project_id" json:"-"`
	BoardID    string   `db:"board_id" json:"-"`
	Seq        int64    `db:"seq" json:"-"`
	ColumnID   string   `db:"column_id" json:"-"`
	Rank       string   `db:"rank" json:"-"`
	Title      string   `db:"title" json:"title"`
	BodyMD     string   `db:"body_md" json:"body"`
	Priority   Priority `db:"priority" json:"-"`
	ClaimedBy  *string  `db:"claimed_by" json:"owner,omitempty"`
	ClaimUntil *int64   `db:"claim_until" json:"claim_until,omitempty"`
	Version    int64    `db:"version" json:"version"`
	CreatedAt  int64    `db:"created_at" json:"created_at"`
	UpdatedAt  int64    `db:"updated_at" json:"updated_at"`
	ArchivedAt *int64   `db:"archived_at" json:"archived_at,omitzero"`
	Ref        string   `db:"ref" json:"ref"` // stored: a merged card keeps its original prefix

	// Computed for display; never read from the database.
	ColumnName   string   `db:"-" json:"column"`
	PriorityName string   `db:"-" json:"priority"`
	Labels       []string `db:"-" json:"labels,omitempty"`
	Tags         []string `db:"-" json:"tags,omitempty"`
}

type NewCard struct {
	Title    string
	Body     string
	Column   string    // empty means the first column
	Priority *Priority // nil means PriorityNormal
	Labels   []string
	Tags     []string
}

type CardFilter struct {
	Column          string
	Priority        *Priority
	IncludeArchived bool
	Label           string // filter by label name
	Limit           int    // 0 = DefaultCardLimit, negative = no cap
}

type CardEdit struct {
	Title        *string
	Body         *string
	Priority     *Priority
	IfVersion    *int64
	AddLabels    []string
	RemoveLabels []string
	AddTags      []string
	RemoveTags   []string
}

func (c *Core) checkCardClaim(card Card) error {
	if card.ClaimedBy == nil || *card.ClaimedBy == c.actor || card.ClaimUntil == nil || *card.ClaimUntil < c.clock.NowMS() {
		return nil
	}
	return ErrConflict("not_owned", fmt.Sprintf("card %s is claimed by %s", card.Ref, *card.ClaimedBy),
		"trellis card show "+card.Ref)
}

// cardView fills the computed fields.
func (c *Core) cardView(tx *sqlx.Tx, card *Card) error {
	var colName string
	if err := tx.Get(&colName, `SELECT name FROM column_ WHERE id = ?`, card.ColumnID); err != nil {
		return err
	}
	card.ColumnName = colName
	card.PriorityName = card.Priority.String()

	card.Labels = []string{}
	if err := tx.Select(&card.Labels,
		`SELECT l.name FROM label l JOIN card_label cl ON cl.label_id = l.id WHERE cl.card_id = ? ORDER BY l.name`,
		card.ID); err != nil {
		return err
	}
	card.Tags = []string{}
	if err := tx.Select(&card.Tags,
		`SELECT t.name FROM tag t JOIN card_tag ct ON ct.tag_id = t.id WHERE ct.card_id = ? ORDER BY t.name`,
		card.ID); err != nil {
		return err
	}
	return nil
}

// checkCardProject refuses an address that names another project:
// /OTHER/cards/X-12 typed while working in KEY. A ref's prefix is not checked
// here. Refs are stored, and after a merge a card keeps the prefix it was born
// with inside a project with another key; loadCard matches the stored ref and
// explains a prefix that lives elsewhere.
func (c *Core) checkCardProject(tx *sqlx.Tx, projectID string, ref CardRef) error {
	if ref.Project == "" {
		return nil
	}
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	if ref.Project == key {
		return nil
	}
	var exists int
	if err := tx.Get(&exists, `SELECT COUNT(*) FROM project WHERE key = ?`, ref.Project); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound("card_not_found",
			fmt.Sprintf("no card %s: there is no project %s", ref, ref.Project), "trellis project ls")
	}
	return ErrUsage("wrong_project",
		fmt.Sprintf("%s is a card in project %s, not in %s", ref, ref.Project, key),
		"trellis card show "+ref.String()+" --project "+ref.Project)
}

// loadCard fetches a card inside an existing transaction and fills computed fields.
// A qualified ref is matched against the stored ref, so a card that arrived
// through a merge is found under the prefix it was born with; a bare number
// means this project's own prefix.
func (c *Core) loadCard(tx *sqlx.Tx, projectID string, ref CardRef, out *Card) error {
	if err := c.checkCardProject(tx, projectID, ref); err != nil {
		return err
	}
	key, err := projectKeyOf(tx, projectID)
	if err != nil {
		return err
	}
	switch {
	case ref.UUID != "":
		err = tx.Get(out, `SELECT * FROM card WHERE id = ? AND project_id = ?`, ref.UUID, projectID)
	case ref.Seq > 0:
		want := ref.qualified()
		if ref.ProjectKey == "" {
			want = key + "-" + itoa(ref.Seq)
		}
		err = tx.Get(out, `SELECT * FROM card WHERE ref = ? AND project_id = ?`, want, projectID)
		if errors.Is(err, sql.ErrNoRows) && ref.ProjectKey != "" {
			return cardElsewhere(tx, want)
		}
	default:
		return ErrUsage("bad_card_ref", "card reference is empty", "trellis card ls")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("card_not_found", "no card "+ref.String()+" in this project", "trellis card ls")
	}
	if err != nil {
		return err
	}
	return c.cardView(tx, out)
}

// cardElsewhere explains a qualified ref missing from the project it was
// looked up in: it is either a card in another project, or no card at all.
// OTHER-12 typed while working in KEY once opened KEY-12; a ref names one card.
func cardElsewhere(tx *sqlx.Tx, ref string) error {
	var project string
	err := tx.Get(&project,
		`SELECT p.key FROM card c JOIN project p ON p.id = c.project_id WHERE c.ref = ?`, ref)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("card_not_found", "no card "+ref, "trellis card ls")
	}
	if err != nil {
		return err
	}
	return ErrUsage("wrong_project", fmt.Sprintf("%s is a card in project %s", ref, project),
		"trellis card show "+address.Card(project, ref).String())
}

// CardProject finds the project that holds the card a qualified ref names,
// wherever its prefix points: after a merge, API-12 lives in MONO. A bare
// number or a UUID names no project by itself, so found is false.
func (c *Core) CardProject(ctx context.Context, ref string) (Project, bool, error) {
	r := ParseCardRef(ref)
	if r.ProjectKey == "" {
		return Project{}, false, nil
	}
	var p Project
	err := c.db.GetContext(ctx, &p,
		`SELECT p.* FROM project p JOIN card c ON c.project_id = p.id WHERE c.ref = ?`, r.qualified())
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	return p, err == nil, err
}

// CreateCard adds a card to a board. seq is allocated per PROJECT, not per
// board, so XPSCTL-12 stays unambiguous and a card keeps its reference if it
// later moves to another board.
func (c *Core) CreateCard(ctx context.Context, projectID, boardID string, in NewCard) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		var err error
		card, err = c.createCard(ctx, tx, projectID, boardID, in)
		return err
	})
	return card, err
}

// createCard is the transaction-level form. card import needs many cards in one
// transaction, and Tx cannot nest: store.Open caps the pool at one connection.
func (c *Core) createCard(ctx context.Context, tx *sqlx.Tx, projectID, boardID string, in NewCard) (Card, error) {
	var card Card
	err := func() error {
		if c.requireLabels && len(in.Labels) == 0 {
			return ErrUsage("label_required", "cards require at least one label", "trellis card new --label <name>")
		}
		if c.requireTags && len(in.Tags) == 0 {
			return ErrUsage("tag_required", "cards require at least one tag", "trellis card new --tag <name>")
		}
		col, err := c.FirstColumn(tx, boardID)
		if err != nil {
			return err
		}
		if in.Column != "" {
			if col, err = c.ColumnByName(tx, boardID, in.Column); err != nil {
				return err
			}
		}

		// Safe under concurrency: the transaction is IMMEDIATE, so the write
		// lock is already held when MAX(seq) is read.
		var seq int64
		if err := tx.Get(&seq,
			`SELECT COALESCE(MAX(seq), 0) + 1 FROM card WHERE project_id = ?`, projectID); err != nil {
			return err
		}

		var key string
		if err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID); err != nil {
			return err
		}

		if err := c.checkWrite(ctx, ProposedWrite{
			Op: "card.create", EntityType: "card", ProjectID: projectID, BoardID: boardID,
			Fields: map[string]string{"title": in.Title, "body": in.Body},
		}); err != nil {
			return err
		}

		now := c.clock.NowMS()
		prio := PriorityNormal
		if in.Priority != nil {
			prio = *in.Priority
		}
		card = Card{
			ID: NewCardID(), ProjectID: projectID, BoardID: boardID, Seq: seq, Ref: key + "-" + itoa(seq), ColumnID: col.ID,
			Title: in.Title, BodyMD: in.Body, Priority: prio,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		// P0 ranks by creation: uuid v7 is time-ordered, so this sorts
		// correctly. P3 replaces it with midpoint ranks for drag-and-drop.
		card.Rank = card.ID

		if _, err := tx.Exec(
			`INSERT INTO card (id, project_id, board_id, seq, ref, column_id, rank, title, body_md,
			                   priority, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			card.ID, card.ProjectID, card.BoardID, card.Seq, card.Ref, card.ColumnID, card.Rank, card.Title,
			card.BodyMD, int(card.Priority), card.Version, card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}
		if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
			return err
		}

		// Add labels (with validation - hard reject if label doesn't exist)
		for _, labelName := range in.Labels {
			if err := c.AddCardLabel(tx, card.ID, projectID, labelName); err != nil {
				return err
			}
		}

		// Add tags (auto-create if needed)
		for _, tagName := range in.Tags {
			if err := c.AddCardTag(tx, card.ID, projectID, tagName); err != nil {
				return err
			}
		}

		if err := c.recordEvent(tx, "card", card.ID, "created", "", "", card.Title); err != nil {
			return err
		}
		return c.cardView(tx, &card)
	}()
	return card, err
}

// GetCard looks up a card by reference within a project.
func (c *Core) GetCard(ctx context.Context, projectID string, ref CardRef) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		return nil
	})
	return card, err
}

// CardScope says which cards a listing may see: one board, one project, or
// every project. An agent working across repositories needs the last one.
type CardScope struct {
	BoardID   string
	ProjectID string // used when BoardID is empty; empty too means all projects
}

// CardPage is a bounded listing. An unbounded list on a large board dumps the
// whole board into an agent's context, which defeats the injection budget the
// adoption argument depends on — so Total and Truncated always travel with it.
type CardPage struct {
	Cards     []Card `json:"cards"`
	Total     int    `json:"total"`
	Truncated bool   `json:"truncated"`
}

// DefaultCardLimit is the row cap when none is given.
const DefaultCardLimit = 50

// ListCards returns every card in a board matching the filter, unbounded.
func (c *Core) ListCards(ctx context.Context, boardID string, f CardFilter) ([]Card, error) {
	f.Limit = -1
	page, err := c.ListCardsPage(ctx, CardScope{BoardID: boardID}, f)
	return page.Cards, err
}

// ListCardsPage is the bounded, scope-aware listing behind `card ls`.
// Limit 0 means DefaultCardLimit; a negative Limit means no cap.
func (c *Core) ListCardsPage(ctx context.Context, scope CardScope, f CardFilter) (CardPage, error) {
	var where []string
	var args []any

	switch {
	case scope.BoardID != "":
		where = append(where, "c.board_id = ?")
		args = append(args, scope.BoardID)
	case scope.ProjectID != "":
		where = append(where, "c.project_id = ?")
		args = append(args, scope.ProjectID)
	}

	if !f.IncludeArchived {
		where = append(where, "c.archived_at IS NULL")
	}
	if f.Column != "" {
		where = append(where, "col.name = ?")
		args = append(args, f.Column)
	}
	if f.Priority != nil {
		where = append(where, "c.priority = ?")
		args = append(args, int(*f.Priority))
	}

	from := `FROM card c JOIN column_ col ON col.id = c.column_id`
	if f.Label != "" {
		from += ` JOIN card_label cl ON cl.card_id = c.id JOIN label l ON l.id = cl.label_id`
		where = append(where, "l.name = ?")
		args = append(args, f.Label)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	limit := f.Limit
	if limit == 0 {
		limit = DefaultCardLimit
	}

	page := CardPage{Cards: []Card{}}
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := tx.Get(&page.Total, `SELECT COUNT(*) `+from+clause, args...); err != nil {
			return err
		}
		q := `SELECT c.* ` + from + clause + ` ORDER BY col.position, c.priority, c.rank`
		if limit > 0 {
			q += " LIMIT ?"
			args = append(args, limit)
		}
		if err := tx.Select(&page.Cards, q, args...); err != nil {
			return err
		}
		for i := range page.Cards {
			if err := c.cardView(tx, &page.Cards[i]); err != nil {
				return err
			}
		}
		return nil
	})
	page.Truncated = len(page.Cards) < page.Total
	return page, err
}

// MoveCard changes a card's column. It is a delta rather than a wholesale
// replacement, so it does not require --if-version. Moving into a done column
// releases the claim automatically.
func (c *Core) MoveCard(ctx context.Context, projectID, boardID string, ref CardRef, column string) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}
		var targetProject string
		if err := tx.Get(&targetProject, `SELECT project_id FROM board WHERE id = ?`, boardID); err != nil {
			return ErrNotFound("board_not_found", "destination board not found", "trellis board ls")
		}
		if targetProject != projectID {
			return ErrUsage("wrong_project", "a card can only move within its project", "trellis card move "+ref.String()+" --board <name>")
		}
		var from string
		if err := tx.Get(&from, `SELECT name FROM column_ WHERE id = ?`, card.ColumnID); err != nil {
			return err
		}
		to, err := c.ColumnByName(tx, boardID, column)
		if err != nil {
			return err
		}
		if to.ID == card.ColumnID {
			return c.cardView(tx, &card)
		}

		now := c.clock.NowMS()
		// If moving to a done column, release the claim automatically.
		var claimUpdate string
		if to.IsDone {
			claimUpdate = ", claimed_by = NULL, claim_until = NULL"
		}
		if _, err := tx.Exec(
			`UPDATE card SET board_id = ?, column_id = ?, version = version + 1, updated_at = ?`+claimUpdate+` WHERE id = ?`,
			boardID, to.ID, now, card.ID); err != nil {
			return err
		}
		card.BoardID, card.ColumnID, card.Version, card.UpdatedAt = boardID, to.ID, card.Version+1, now
		if to.IsDone {
			card.ClaimedBy = nil
			card.ClaimUntil = nil
		}

		if err := c.recordEvent(tx, "card", card.ID, "moved", "column", from, to.Name); err != nil {
			return err
		}
		return c.cardView(tx, &card)
	})
	return card, err
}

// EditCard applies a partial update. Wholesale replacements (title, body)
// require the version the caller read; deltas do not. Only the fields supplied
// are written, so a title edit cannot erase a body changed moments earlier.
// If the caller claims the card, every write extends the claim (working on a card is the heartbeat).
func (c *Core) EditCard(ctx context.Context, projectID string, ref CardRef, e CardEdit) (Card, error) {
	var card Card
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}

		replaces := e.Title != nil || e.Body != nil
		if replaces && e.IfVersion == nil {
			return ErrUsage("version_required",
				"--title and --body replace the whole field and need the version you read",
				fmt.Sprintf("trellis card show %s --json   # then pass --if-version %d",
					refOrID(card), card.Version))
		}
		if e.IfVersion != nil && *e.IfVersion != card.Version {
			return &Error{
				Code: "conflict", Exit: 4,
				Msg: fmt.Sprintf("%s changed since you read it (you: v%d, now: v%d)",
					refOrID(card), *e.IfVersion, card.Version),
				Fix: "trellis card show " + refOrID(card) + " --json",
			}
		}
		if replaces {
			// Only when the card has no revision at all yet: a move or claim
			// change between two edits bumps card.Version without touching
			// title or body, and capturing that in-between version here would
			// invent a revision nothing actually wrote -- the design captures
			// "before" only to cover a card that predates this feature.
			hasRevision, err := c.cardHasRevision(tx, card.ID)
			if err != nil {
				return err
			}
			if !hasRevision {
				if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
					return err
				}
			}
		}

		w := ProposedWrite{
			Op: "card.edit", EntityType: "card", EntityID: card.ID,
			ProjectID: projectID, BoardID: card.BoardID, Fields: map[string]string{},
		}
		if e.Title != nil {
			w.Fields["title"] = *e.Title
		}
		if e.Body != nil {
			w.Fields["body"] = *e.Body
		}
		if err := c.checkWrite(ctx, w); err != nil {
			return err
		}

		sets := []string{"version = version + 1", "updated_at = ?"}
		args := []any{c.clock.NowMS()}

		// If we claim this card, extend the claim (working on it is the heartbeat).
		if card.ClaimedBy != nil && *card.ClaimedBy == c.actor {
			ttl := c.claimTTL
			sets = append(sets, "claim_until = ?")
			args = append(args, c.clock.NowMS()+ttl)
		}

		record := func(field, oldV, newV string) error {
			return c.recordEvent(tx, "card", card.ID, "edited", field, oldV, newV)
		}
		var pending []func() error

		if e.Title != nil {
			sets = append(sets, "title = ?")
			args = append(args, *e.Title)
			old := card.Title
			card.Title = *e.Title
			pending = append(pending, func() error { return record("title", old, *e.Title) })
		}
		if e.Body != nil {
			sets = append(sets, "body_md = ?")
			args = append(args, *e.Body)
			card.BodyMD = *e.Body
			pending = append(pending, func() error { return record("body", "", "") })
		}
		if e.Priority != nil {
			sets = append(sets, "priority = ?")
			args = append(args, int(*e.Priority))
			old := card.Priority
			card.Priority = *e.Priority
			pending = append(pending, func() error {
				return record("priority", old.String(), e.Priority.String())
			})
		}
		// Handle labels and tags (these don't require version checks)
		for _, labelName := range e.AddLabels {
			if err := c.AddCardLabel(tx, card.ID, projectID, labelName); err != nil {
				return err
			}
		}
		for _, labelName := range e.RemoveLabels {
			if err := c.RemoveCardLabel(tx, card.ID, labelName); err != nil {
				return err
			}
		}
		for _, tagName := range e.AddTags {
			if err := c.AddCardTag(tx, card.ID, projectID, tagName); err != nil {
				return err
			}
		}
		for _, tagName := range e.RemoveTags {
			if err := c.RemoveCardTag(tx, card.ID, tagName); err != nil {
				return err
			}
		}
		if c.requireLabels || c.requireTags {
			var count int
			if c.requireLabels {
				if err := tx.Get(&count, `SELECT COUNT(*) FROM card_label WHERE card_id = ?`, card.ID); err != nil {
					return err
				}
				if count == 0 {
					return ErrUsage("label_required", "cards require at least one label", "trellis card edit "+card.Ref+" --add-label <name>")
				}
			}
			if c.requireTags {
				if err := tx.Get(&count, `SELECT COUNT(*) FROM card_tag WHERE card_id = ?`, card.ID); err != nil {
					return err
				}
				if count == 0 {
					return ErrUsage("tag_required", "cards require at least one tag", "trellis card edit "+card.Ref+" --add-tag <name>")
				}
			}
		}

		if len(pending) == 0 && len(e.AddLabels) == 0 && len(e.RemoveLabels) == 0 &&
			len(e.AddTags) == 0 && len(e.RemoveTags) == 0 {
			return c.cardView(tx, &card)
		}

		if len(pending) > 0 {
			args = append(args, card.ID)
			if _, err := tx.Exec(
				"UPDATE card SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
				return err
			}
			card.Version++
			for _, fn := range pending {
				if err := fn(); err != nil {
					return err
				}
			}
			if replaces {
				if err := c.captureCardRevision(tx, card.ID, card.Version, card.Title, card.BodyMD); err != nil {
					return err
				}
			}
		}
		return c.cardView(tx, &card)
	})
	return card, err
}

// DeleteCard removes a card outright. Archiving (P1) is for finished work;
// this is for the duplicates an agent creates by mistake. The event log is
// never touched — it is the change feed.
func (c *Core) DeleteCard(ctx context.Context, projectID string, ref CardRef) error {
	return c.Tx(ctx, func(tx *sqlx.Tx) error {
		var card Card
		if err := c.loadCard(tx, projectID, ref, &card); err != nil {
			return err
		}
		if err := c.checkCardClaim(card); err != nil {
			return err
		}
		if err := c.recordEvent(tx, "card", card.ID, "deleted", "", card.Title, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM card WHERE id = ?`, card.ID); err != nil {
			return err
		}
		return nil
	})
}

func refOrID(c Card) string {
	if c.Ref != "" {
		return c.Ref
	}
	return c.ID
}
