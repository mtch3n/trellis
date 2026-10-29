package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/platform"
)

type Options struct {
	Home   string
	Org    string
	Member string
}

type Report struct {
	Imported        []string         `json:"imported"`
	Skipped         []string         `json:"skipped"`
	Failed          []Failure        `json:"failed"`
	LeftOut         map[string]int64 `json:"left_out"`
	MovedToPersonal []string         `json:"moved_to_personal"`
}

type Failure struct {
	Project string `json:"project"`
	Error   string `json:"error"`
}

func Run(ctx context.Context, pg *sqlx.DB, opt Options) (Report, error) {
	rep := Report{Imported: []string{}, Skipped: []string{}, Failed: []Failure{}, MovedToPersonal: []string{}}
	h, err := openHome(opt.Home)
	if err != nil {
		path := filepath.Join(opt.Home, "trellis.db")
		if errors.Is(err, os.ErrNotExist) {
			return rep, platform.ErrUsage("no_home", "no Trellis home at "+opt.Home+": "+path+" does not exist",
				"trellis import --home ~/.trellis --org <org> --member <handle>")
		}
		return rep, platform.ErrUsage("bad_home", "cannot read "+path+": "+err.Error(), "check --home")
	}
	defer h.close()
	who, err := findMember(ctx, pg, opt.Org, opt.Member)
	if err != nil {
		return rep, err
	}
	projects, err := h.projects()
	if err != nil {
		return rep, err
	}
	if rep.LeftOut, err = h.leftOut(); err != nil {
		return rep, err
	}
	for _, p := range projects {
		var exists bool
		if err := pg.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM project WHERE org_id = $1 AND key = $2)`, who.orgID, p.Key); err != nil {
			return rep, err
		}
		if exists {
			rep.Skipped = append(rep.Skipped, p.Key)
			continue
		}
		moved, err := importProject(ctx, pg, h, who, p)
		if err != nil {
			rep.Failed = append(rep.Failed, Failure{Project: p.Key, Error: err.Error()})
			continue
		}
		rep.Imported = append(rep.Imported, p.Key)
		rep.MovedToPersonal = append(rep.MovedToPersonal, moved...)
	}
	return rep, nil
}

type member struct {
	orgID    string
	memberID string
	handle   string
}

func (m member) actor() string { return "agent:" + m.handle + "/import" }

func findMember(ctx context.Context, pg *sqlx.DB, org, handle string) (member, error) {
	m := member{handle: handle}
	err := pg.QueryRowxContext(ctx, `SELECT id::text FROM org WHERE name = $1`, org).Scan(&m.orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return m, platform.ErrNotFound("org_not_found", fmt.Sprintf("org %q not found", org), "trellis admin org new "+org)
	}
	if err != nil {
		return m, err
	}
	err = pg.QueryRowxContext(ctx, `SELECT id::text FROM member WHERE org_id = $1 AND handle = $2`, m.orgID, handle).Scan(&m.memberID)
	if errors.Is(err, sql.ErrNoRows) {
		return m, platform.ErrNotFound("member_not_found", fmt.Sprintf("member %q is not in org %q", handle, org),
			"trellis admin member add --org "+org+" "+handle)
	}
	return m, err
}

func importProject(ctx context.Context, pg *sqlx.DB, h *home, who member, p localProject) ([]string, error) {
	s, err := h.snapshot(p)
	if err != nil {
		return nil, err
	}
	tx, err := pg.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	w := &writer{tx: tx, who: who, s: s}
	if err := w.write(); err != nil {
		tx.Rollback()
		return nil, err
	}
	return w.moved, tx.Commit()
}

func (r Report) Text() string {
	var b strings.Builder
	for _, k := range r.Imported {
		fmt.Fprintf(&b, "imported %s\n", k)
	}
	for _, k := range r.Skipped {
		fmt.Fprintf(&b, "skipped %s: the org already has this project\n", k)
	}
	for _, slug := range r.MovedToPersonal {
		fmt.Fprintf(&b, "moved to personal %s: an earlier project entry has the same body\n", slug)
	}
	for _, f := range r.Failed {
		fmt.Fprintf(&b, "failed %s: %s\n", f.Project, f.Error)
	}
	parts := []string{}
	for _, kind := range slices.Sorted(maps.Keys(r.LeftOut)) {
		parts = append(parts, fmt.Sprintf("%d %s", r.LeftOut[kind], kind))
	}
	fmt.Fprintf(&b, "left out: %s", strings.Join(parts, ", "))
	return b.String()
}
