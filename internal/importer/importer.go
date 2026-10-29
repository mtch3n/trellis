package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/platform"
)

type Options struct {
	Home   string
	Org    string
	Member string
}

type Report struct {
	Imported []string         `json:"imported"`
	Skipped  []string         `json:"skipped"`
	Failed   []Failure        `json:"failed"`
	LeftOut  map[string]int64 `json:"left_out"`
}

type Failure struct {
	Project string `json:"project"`
	Error   string `json:"error"`
}

func Run(ctx context.Context, pg *sqlx.DB, opt Options) (Report, error) {
	rep := Report{Imported: []string{}, Skipped: []string{}, Failed: []Failure{}}
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
		if err := importProject(ctx, pg, h, who, p); err != nil {
			rep.Failed = append(rep.Failed, Failure{Project: p.Key, Error: err.Error()})
			continue
		}
		rep.Imported = append(rep.Imported, p.Key)
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

func importProject(ctx context.Context, pg *sqlx.DB, h *home, who member, p localProject) error {
	s, err := h.snapshot(p)
	if err != nil {
		return err
	}
	tx, err := pg.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if err := (&writer{tx: tx, who: who, s: s}).write(); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
