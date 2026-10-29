package platform

import (
	"encoding/json/v2"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

type Event struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	Actor     string         `json:"actor"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

func recordEvent(tx *sqlx.Tx, pr Principal, project projectAccess, boardID uuid.UUID, cardID *uuid.UUID, kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event (org_id, project_id, board_id, card_id, kind, actor, member_id, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		project.OrgID, project.ID, boardID, cardID, kind, pr.Actor(), pr.MemberID, raw)
	return err
}

func cardEvents(tx *sqlx.Tx, cardID uuid.UUID) ([]Event, error) {
	var rows []struct {
		ID        int64     `db:"id"`
		Kind      string    `db:"kind"`
		Actor     string    `db:"actor"`
		Data      []byte    `db:"data"`
		CreatedAt time.Time `db:"created_at"`
	}
	if err := tx.Select(&rows, `SELECT id, kind, actor, data, created_at FROM event WHERE card_id = $1 ORDER BY id`, cardID); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(rows))
	for _, r := range rows {
		e := Event{ID: r.ID, Kind: r.Kind, Actor: r.Actor, CreatedAt: r.CreatedAt}
		if err := json.Unmarshal(r.Data, &e.Data); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
