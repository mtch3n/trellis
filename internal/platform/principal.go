package platform

import "uuid"

type PrincipalKind string

const (
	PrincipalMemberAgent PrincipalKind = "member-agent"
	PrincipalPerson      PrincipalKind = "person"
	PrincipalSystem      PrincipalKind = "system"
)

type Principal struct {
	Kind      PrincipalKind
	OrgID     uuid.UUID
	MemberID  uuid.UUID
	Handle    string
	TokenName string
	Session   string
	Job       string
}

func MemberAgent(orgID, memberID uuid.UUID, handle, tokenName, session string) Principal {
	return Principal{Kind: PrincipalMemberAgent, OrgID: orgID, MemberID: memberID, Handle: handle, TokenName: tokenName, Session: session}
}

func System(orgID uuid.UUID, job string) Principal {
	return Principal{Kind: PrincipalSystem, OrgID: orgID, Job: job}
}

func (p Principal) IsSystem() bool { return p.Kind == PrincipalSystem }

func (p Principal) Actor() string {
	if p.IsSystem() {
		return "system:" + p.Job
	}
	if p.Kind == PrincipalPerson {
		return "person:" + p.Handle
	}
	actor := "agent:" + p.Handle + "/" + p.TokenName
	if p.Session != "" {
		actor += "/" + p.Session
	}
	return actor
}
