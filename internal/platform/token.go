package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

const (
	TokenAgent  = "agent"
	TokenPerson = "person"
)

var TokenKinds = []string{TokenAgent, TokenPerson}

type Token struct {
	Name      string     `json:"name" db:"name"`
	Kind      string     `json:"kind" db:"kind"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
}

type TokenIdentity struct {
	TokenID   uuid.UUID
	TokenName string
	Kind      string
	OrgID     uuid.UUID
	MemberID  uuid.UUID
	Handle    string
}

func (t TokenIdentity) Principal(session string) Principal {
	return MemberAgent(t.OrgID, t.MemberID, t.Handle, t.TokenName, session)
}

func hashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func (p *Platform) IssueToken(ctx context.Context, org, handle, name, kind string) (string, error) {
	if !slices.Contains(TokenKinds, kind) {
		return "", ErrUsage("bad_kind", fmt.Sprintf("token kind %q is not allowed; the allowed kinds are %s", kind, strings.Join(TokenKinds, ", ")),
			"use one of: "+strings.Join(TokenKinds, ", "))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrUsage("missing_name", "a token needs a name, such as claude-laptop", "trellis admin token issue --org <org> <handle> <name>")
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	secret := base64.RawURLEncoding.EncodeToString(raw)
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, err := memberByHandle(ctx, tx, org, handle)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO token (member_id, name, kind, hash) VALUES ($1, $2, $3, $4)`,
			m.ID.String(), name, kind, hashToken(secret))
		if isUniqueViolation(err) {
			return ErrConflict("token_exists", fmt.Sprintf("member %q already has a live token %q", handle, name),
				"revoke it first, or choose another name")
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (p *Platform) ListTokens(ctx context.Context, org, handle string) ([]Token, error) {
	out := []Token{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, err := memberByHandle(ctx, tx, org, handle)
		if err != nil {
			return err
		}
		return tx.SelectContext(ctx, &out, `SELECT name, kind, created_at, revoked_at FROM token WHERE member_id = $1 ORDER BY created_at, name`, m.ID.String())
	})
	return out, err
}

func (p *Platform) RevokeToken(ctx context.Context, org, handle, name string) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		m, err := memberByHandle(ctx, tx, org, handle)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE token SET revoked_at = now() WHERE member_id = $1 AND name = $2 AND revoked_at IS NULL`, m.ID.String(), name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound("token_not_found", fmt.Sprintf("member %q has no live token %q", handle, name), "trellis admin token ls --org <org> <handle>")
		}
		return nil
	})
}

func (p *Platform) VerifyToken(ctx context.Context, secret string) (TokenIdentity, error) {
	var id TokenIdentity
	err := p.db.QueryRowxContext(ctx, `SELECT t.id, t.name, t.kind, m.org_id, m.id, m.handle
		FROM token t JOIN member m ON m.id = t.member_id
		WHERE t.hash = $1 AND t.revoked_at IS NULL`, hashToken(secret)).
		Scan(uuidDest(&id.TokenID), &id.TokenName, &id.Kind, uuidDest(&id.OrgID), uuidDest(&id.MemberID), &id.Handle)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenIdentity{}, ErrNotFound("bad_token", "the token is unknown or revoked", "ask an admin for a token: trellis admin token issue")
	}
	return id, err
}
