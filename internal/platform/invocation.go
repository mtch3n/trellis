package platform

import (
	"context"
	"time"
	"uuid"
)

const CodeOK = "0"

type Invocation struct {
	OrgID    uuid.UUID
	MemberID uuid.UUID
	TokenID  uuid.UUID
	Actor    string
	Tool     string
	Action   string
	ArgNames []string
	Code     string
	Duration time.Duration
}

func (p *Platform) LogInvocation(ctx context.Context, in Invocation) error {
	names := in.ArgNames
	if names == nil {
		names = []string{}
	}
	_, err := p.db.ExecContext(ctx, `INSERT INTO invocation (org_id, member_id, token_id, actor, tool, action, arg_names, code, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		in.OrgID, in.MemberID, in.TokenID, in.Actor, in.Tool, in.Action, names, in.Code, in.Duration.Milliseconds())
	return err
}
