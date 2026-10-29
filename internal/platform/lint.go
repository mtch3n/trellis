package platform

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

type Diagnostic struct {
	Kind  string `json:"kind"`
	Entry string `json:"entry"`
	Ref   string `json:"ref,omitempty"`
	Fix   string `json:"fix"`
}

type HealthLine struct {
	What  string `json:"what"`
	Count int    `json:"count"`
	Fix   string `json:"fix"`
}

func (p *Platform) Lint(ctx context.Context, pr Principal, project string) ([]Diagnostic, error) {
	var out []Diagnostic
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		out, _, err = lint(tx, pr, proj)
		return err
	})
	return out, err
}

func lint(tx *sqlx.Tx, pr Principal, proj projectAccess) ([]Diagnostic, []Entry, error) {
	var entries []Entry
	if err := tx.Select(&entries, entrySelect+` WHERE `+visibleEntrySQL+` ORDER BY e.slug, e.id`, visibleEntryArgs(pr, proj)...); err != nil {
		return nil, nil, err
	}
	out := []Diagnostic{}
	for _, e := range entries {
		var stubs []string
		if err := tx.Select(&stubs, `SELECT target FROM entry_link WHERE from_entry_id = $1 AND rel = 'wikilink'
			AND to_entry_id IS NULL AND to_card_id IS NULL ORDER BY target`, e.ID); err != nil {
			return nil, nil, err
		}
		for _, target := range stubs {
			out = append(out, Diagnostic{Kind: "stub", Entry: e.Slug, Ref: target, Fix: "vault_write new with slug " + target})
		}
		var linked bool
		if err := tx.Get(&linked, `SELECT EXISTS (SELECT 1 FROM entry_link WHERE from_entry_id = $1 OR to_entry_id = $1)
			OR EXISTS (SELECT 1 FROM card_entry WHERE entry_id = $1)`, e.ID); err != nil {
			return nil, nil, err
		}
		if !linked {
			out = append(out, Diagnostic{Kind: "orphan", Entry: e.Slug,
				Fix: "link it with [[" + e.Slug + "]] from another entry, or vault_write link it to a card"})
		}
		if e.Template == "" {
			continue
		}
		t, err := templateNamed(tx, proj.ID, e.Template)
		if IsCode(err, "unknown_template") {
			out = append(out, Diagnostic{Kind: "unknown_template", Entry: e.Slug, Ref: e.Template, Fix: "template list"})
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		for _, problem := range t.violations(e.fieldValues(), e.Body, true) {
			out = append(out, Diagnostic{Kind: "template_violation", Entry: e.Slug, Ref: problem, Fix: "vault_write edit " + e.Slug})
		}
	}
	return out, entries, nil
}

func (p *Platform) Health(ctx context.Context, pr Principal, project string) ([]HealthLine, error) {
	var out []HealthLine
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		diags, entries, err := lint(tx, pr, proj)
		if err != nil {
			return err
		}
		kinds := map[string]int{}
		for _, d := range diags {
			kinds[d.Kind]++
		}
		var private, overdue int
		for _, e := range entries {
			if e.Private {
				private++
			}
			if e.Scope == ScopeGlobal && e.VerifyBy != nil && e.VerifyBy.Before(time.Now()) {
				overdue++
			}
		}
		var revisions, nominations int
		if err := tx.Get(&revisions, `SELECT count(*) FROM entry_revision r JOIN entry e ON e.id = r.entry_id WHERE `+visibleEntrySQL,
			visibleEntryArgs(pr, proj)...); err != nil {
			return err
		}
		if err := tx.Get(&nominations, `SELECT count(DISTINCT n.entry_id) FROM nomination n JOIN entry e ON e.id = n.entry_id
			WHERE n.project_id = $1 AND e.scope = 'project' AND e.deleted_at IS NULL`, proj.ID); err != nil {
			return err
		}
		out = []HealthLine{
			{What: "entries", Count: len(entries), Fix: "vault_list"},
			{What: "private", Count: private, Fix: "vault_list"},
			{What: "global past verify_by", Count: overdue, Fix: "vault_share verify"},
			{What: "waiting nominations", Count: nominations, Fix: "vault_check nominations"},
			{What: "orphans", Count: kinds["orphan"], Fix: "vault_check lint"},
			{What: "stubs", Count: kinds["stub"], Fix: "vault_check lint"},
			{What: "template problems", Count: kinds["template_violation"] + kinds["unknown_template"], Fix: "vault_check lint"},
			{What: "revisions", Count: revisions, Fix: "vault_check history"},
		}
		return nil
	})
	return out, err
}
