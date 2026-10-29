package platform

import (
	"bytes"
	"testing"
)

func TestPLAT_C9_AnIssuedAgentTokenVerifiesAndOnlyItsHashIsStored(t *testing.T) {
	p, db := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "acme")
	mustDo(t, err)
	_, err = p.AddMember(ctx, "acme", "ming", "")
	mustDo(t, err)
	secret, err := p.IssueToken(ctx, "acme", "ming", "claude-laptop", TokenAgent)
	mustDo(t, err)

	id, err := p.VerifyToken(ctx, secret)
	mustDo(t, err)
	if id.Kind != TokenAgent || id.Handle != "ming" {
		t.Fatalf("identity = %+v", id)
	}

	var stored []byte
	mustDo(t, db.Get(&stored, `SELECT hash FROM token`))
	if !bytes.Equal(stored, hashToken(secret)) {
		t.Fatalf("stored %x, want the sha256 of the token", stored)
	}
	var leaks int
	mustDo(t, db.Get(&leaks, `SELECT count(*) FROM token t WHERE position(convert_to($1, 'UTF8') IN t.hash) > 0 OR t.name = $1`, secret))
	if leaks != 0 {
		t.Fatal("the database holds the token itself")
	}
}

func TestPLAT_C10_AWrongRevokedOrPersonTokenDoesNotVerifyAsAnAgent(t *testing.T) {
	p, _ := tenancyPlatform(t)
	ctx := t.Context()
	_, err := p.CreateOrg(ctx, "acme")
	mustDo(t, err)
	_, err = p.AddMember(ctx, "acme", "ming", "")
	mustDo(t, err)
	revoked, err := p.IssueToken(ctx, "acme", "ming", "old", TokenAgent)
	mustDo(t, err)
	mustDo(t, p.RevokeToken(ctx, "acme", "ming", "old"))
	person, err := p.IssueToken(ctx, "acme", "ming", "browser", TokenPerson)
	mustDo(t, err)

	for name, secret := range map[string]string{"none": "", "wrong": "not-a-token", "revoked": revoked} {
		t.Run(name, func(t *testing.T) {
			_, err := p.VerifyToken(ctx, secret)
			assertCode(t, err, "bad_token", KindNotFound)
		})
	}
	id, err := p.VerifyToken(ctx, person)
	mustDo(t, err)
	if id.Kind != TokenPerson {
		t.Fatalf("person token kind = %q", id.Kind)
	}

	tokens, err := p.ListTokens(ctx, "acme", "ming")
	mustDo(t, err)
	if len(tokens) != 2 || tokens[0].RevokedAt == nil || tokens[1].RevokedAt != nil {
		t.Fatalf("tokens = %+v, want old revoked and browser live", tokens)
	}
	_, err = p.IssueToken(ctx, "acme", "ming", "x", "system")
	assertCode(t, err, "bad_kind", KindUsage)
}
