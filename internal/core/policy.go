package core

import "context"

// ProposedWrite is what a policy inspects: the operation, what it touches, and
// the text being written. Fields carries only user-authored content — titles,
// bodies, note text — because that is what a policy has an opinion about.
type ProposedWrite struct {
	Op         string            `json:"op"` // "card.create", "card.edit", "comment.create", "entry.write"
	EntityType string            `json:"entity_type"`
	EntityID   string            `json:"entity_id,omitempty"` // empty on create
	ProjectID  string            `json:"project_id,omitempty"`
	BoardID    string            `json:"board_id,omitempty"`
	Actor      string            `json:"actor"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// Policy inspects a proposed write and may reject it. trellis ships none: a
// bundled scanner would make its opinion the only one available (§14). The
// interface exists so a user can add theirs without patching core.
//
// Consequence, stated plainly: nothing here prevents a credential being written
// into a card, a comment or an entry. That is a deliberate non-goal.
type Policy interface {
	Name() string
	Check(ctx context.Context, w ProposedWrite) error
}

// WithPolicies attaches policies to a Core. The default set is empty.
func (c *Core) WithPolicies(ps ...Policy) *Core {
	c.policies = append(c.policies, ps...)
	return c
}

// checkWrite is the single chokepoint every mutating path in core passes
// through. Because the call site is here rather than in a command handler,
// `card comment` — where an agent pastes command output — is covered by whatever
// a user attaches, without card comment knowing a policy exists.
func (c *Core) checkWrite(ctx context.Context, w ProposedWrite) error {
	if len(c.policies) == 0 {
		return nil
	}
	w.Actor = c.actor
	for _, p := range c.policies {
		if err := p.Check(ctx, w); err != nil {
			if _, ok := err.(*Error); ok {
				return err
			}
			return ErrPolicy("policy_rejected", p.Name()+" rejected this write: "+err.Error(), "")
		}
	}
	return nil
}
