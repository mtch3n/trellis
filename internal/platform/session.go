package platform

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
	"uuid"
)

const SessionTTL = 12 * time.Hour

type WebSession struct {
	OrgID    uuid.UUID
	Org      string
	MemberID uuid.UUID
	Handle   string
	CSRF     string
}

func Person(orgID, memberID uuid.UUID, handle string) Principal {
	return Principal{Kind: PrincipalPerson, OrgID: orgID, MemberID: memberID, Handle: handle}
}

func (s WebSession) Principal() Principal {
	return Person(s.OrgID, s.MemberID, s.Handle)
}

func randomSecret() string {
	raw := make([]byte, 32)
	rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func errBadSession() *Error {
	return ErrPolicy("bad_session", "no live session: it is unknown, ended or older than 12 hours", "sign in again at /login")
}

func (p *Platform) StartSession(ctx context.Context, token string) (string, error) {
	id, err := p.VerifyToken(ctx, token)
	if err != nil {
		return "", err
	}
	if id.Kind != TokenPerson {
		return "", ErrPolicy("not_person_token", "only a token of kind person signs in to the web", "trellis admin token issue --kind person")
	}
	secret := randomSecret()
	_, err = p.db.ExecContext(ctx, `INSERT INTO web_session (hash, token_id, member_id, csrf) VALUES ($1, $2, $3, $4)`,
		hashToken(secret), id.TokenID, id.MemberID, randomSecret())
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (p *Platform) Session(ctx context.Context, secret string) (WebSession, error) {
	var s WebSession
	err := p.db.QueryRowxContext(ctx, `SELECT m.org_id, o.name, m.id, m.handle, s.csrf
		FROM web_session s
		JOIN token t ON t.id = s.token_id
		JOIN member m ON m.id = s.member_id
		JOIN org o ON o.id = m.org_id
		WHERE s.hash = $1 AND s.ended_at IS NULL AND t.revoked_at IS NULL
			AND s.created_at > now() - make_interval(secs => $2)`, hashToken(secret), SessionTTL.Seconds()).
		Scan(uuidDest(&s.OrgID), &s.Org, uuidDest(&s.MemberID), &s.Handle, &s.CSRF)
	if errors.Is(err, sql.ErrNoRows) {
		return WebSession{}, errBadSession()
	}
	return s, err
}

func (p *Platform) EndSession(ctx context.Context, secret string) error {
	_, err := p.db.ExecContext(ctx, `UPDATE web_session SET ended_at = now() WHERE hash = $1 AND ended_at IS NULL`, hashToken(secret))
	return err
}
