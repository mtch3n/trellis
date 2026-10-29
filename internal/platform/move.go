package platform

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/aymanbagabas/go-udiff"
	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

type RevisionDiff struct {
	From int64  `json:"from"`
	To   int64  `json:"to"`
	Diff string `json:"diff"`
}

func checkVersion(e Entry, ifVersion int64, action string) error {
	if ifVersion == 0 {
		return ErrUsage("version_required", action+" needs the version you read",
			fmt.Sprintf("vault_show %s, then pass if_version %d", e.Slug, e.Version))
	}
	if ifVersion != e.Version {
		return ErrConflict("conflict", fmt.Sprintf("%s changed since you read it (you: v%d, now: v%d)", e.Slug, ifVersion, e.Version),
			"vault_show "+e.Slug)
	}
	return nil
}

func (p *Platform) MoveEntry(ctx context.Context, pr Principal, project, slug, to string, ifVersion int64) (Entry, error) {
	var out Entry
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpWrite, true, false)
		if err != nil {
			return err
		}
		if err := checkVersion(e, ifVersion, "a move"); err != nil {
			return err
		}
		to = strings.TrimSpace(to)
		if !address.ValidEntrySlug(to) {
			return ErrUsage("bad_slug", `"`+to+`" is not a valid slug: use lower-case letters, digits and hyphens, with / between directories`, "vault_write move with another to")
		}
		if to == e.Slug {
			return ErrUsage("same_slug", e.Slug+" already has that slug", "vault_write move with another to")
		}
		before := e.gate()
		from := e.Slug
		e.Slug = to
		if err := gateEntryWrite(tx, pr, proj, &before, e.gate()); err != nil {
			return err
		}
		twin, err := liveTwin(tx, e)
		if err != nil {
			return err
		}
		if twin != (uuid.UUID{}) {
			return errEntryExists(e.Slug, twin)
		}
		taken, err := execUnique(tx, `UPDATE entry SET slug = $2, version = version + 1, updated_at = now() WHERE id = $1`, e.ID, e.Slug)
		if err != nil {
			return err
		}
		if taken {
			return errEntryExists(e.Slug, uuid.UUID{})
		}
		e.Version++
		if err := saveRevision(tx, pr, e); err != nil {
			return err
		}
		if err := fillStubs(tx, e); err != nil {
			return err
		}
		if err := recordEntryEvent(tx, pr, proj, e.ID, "entry.moved", map[string]any{"from": from, "to": e.Slug}); err != nil {
			return err
		}
		out, err = loadEntry(tx, pr, proj, e.ID)
		return err
	})
	return out, err
}

func (p *Platform) DiffEntry(ctx context.Context, pr Principal, project, slug string, from, to int64) (RevisionDiff, error) {
	var out RevisionDiff
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		e, err := findEntry(tx, pr, proj, slug, OpRead, false, false)
		if err != nil {
			return err
		}
		var revs []Revision
		if err := tx.Select(&revs, `SELECT version, title, summary, body, actor, created_at FROM entry_revision
			WHERE entry_id = $1 ORDER BY version`, e.ID); err != nil {
			return err
		}
		versions := make([]int64, len(revs))
		rendered := map[int64]string{}
		for i, r := range revs {
			versions[i] = r.Version
			rendered[r.Version] = "# " + r.Title + "\n\n" + r.Body
		}
		if from, to, err = diffRange(versions, from, to, "vault_check history "+e.Slug); err != nil {
			return err
		}
		out = RevisionDiff{From: from, To: to, Diff: udiff.Unified(
			fmt.Sprintf("%s@v%d", e.Slug, from), fmt.Sprintf("%s@v%d", e.Slug, to), rendered[from], rendered[to])}
		return nil
	})
	return out, err
}

func diffRange(versions []int64, from, to int64, fix string) (int64, int64, error) {
	retained := func(v int64) bool {
		_, ok := slices.BinarySearch(versions, v)
		return ok
	}
	span := "no revisions are retained"
	if len(versions) > 0 {
		span = fmt.Sprintf("the retained range is %d-%d", versions[0], versions[len(versions)-1])
	}
	switch {
	case to != 0 && !retained(to):
		return 0, 0, ErrUsage("revision_not_retained", fmt.Sprintf("version %d is not retained; %s", to, span), fix)
	case to == 0 && len(versions) == 0:
		return 0, 0, ErrUsage("no_revisions", span, fix)
	case to == 0:
		to = versions[len(versions)-1]
	}
	switch {
	case from != 0 && !retained(from):
		return 0, 0, ErrUsage("revision_not_retained", fmt.Sprintf("version %d is not retained; %s", from, span), fix)
	case from == 0:
		i, _ := slices.BinarySearch(versions, to)
		if i == 0 {
			return 0, 0, ErrUsage("no_earlier_revision", fmt.Sprintf("version %d has no earlier retained revision; %s", to, span), fix)
		}
		from = versions[i-1]
	}
	return from, to, nil
}
