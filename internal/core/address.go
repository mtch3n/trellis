package core

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)

// DocAddress is a knowledge entry's canonical address: /KEY/knowledge/<slug>,
// or /GLOBAL/knowledge/<slug> once it is in the vault. key is ignored for a
// vault entry.
func DocAddress(key string, global bool, slug string) string {
	if global {
		return vpath.GlobalKnowledgePath(slug).String()
	}
	return vpath.KnowledgePath(key, slug).String()
}

// docAddressSQL is DocAddress in SQL, for queries that alias knowledge as k
// and project as p. TestDocAddressSQLMatchesGo holds the two together.
const docAddressSQL = `'/' || CASE WHEN k.global = 1 THEN '` + vpath.GlobalKey +
	`' ELSE p.key END || '/knowledge/' || k.slug`

func ParseAddress(arg, collection string) (vpath.Path, error) {
	p, err := vpath.Parse(arg)
	if err != nil {
		return vpath.Path{}, ErrUsage("bad_path", err.Error(), "trellis search <words>")
	}
	if p.Collection != collection {
		return vpath.Path{}, ErrUsage("wrong_collection", fmt.Sprintf("%s names %s, not %s", arg, p.Collection, collection), commandFor(p, arg))
	}
	return p, nil
}
func commandFor(p vpath.Path, arg string) string {
	switch p.Collection {
	case vpath.CollectionBoards:
		return "trellis board show --board " + arg
	case vpath.CollectionCards:
		return "trellis card show " + arg
	case vpath.CollectionKnowledge:
		return "trellis knowledge show " + arg
	case vpath.CollectionArtifacts:
		return "trellis artifact ls --project " + p.Project
	}
	return "trellis card ls --project " + arg
}
func wrongProject(arg string, p vpath.Path, current string) error {
	return ErrUsage("wrong_project", fmt.Sprintf("%s is in project %s, and this call acts in %s", arg, p.Project, current), commandFor(p, arg))
}
func projectKeyOf(tx *sqlx.Tx, projectID string) (string, error) {
	if projectID == "" {
		return "", nil
	}
	var key string
	err := tx.Get(&key, `SELECT key FROM project WHERE id = ?`, projectID)
	return key, err
}

type docScope int

const (
	docRelative docScope = iota
	docOwn
	docVault
)

type docArg struct {
	slug  string
	scope docScope
}

func readDocArg(arg, projectKey string) (docArg, error) {
	target, _ := vpath.SplitAnchor(strings.TrimSpace(arg))
	if !strings.HasPrefix(target, "/") {
		return docArg{Slugify(target), docRelative}, nil
	}
	p, err := ParseAddress(target, vpath.CollectionKnowledge)
	if err != nil {
		return docArg{}, err
	}
	if p.Project == vpath.GlobalKey {
		return docArg{p.Name, docVault}, nil
	}
	if p.Project == projectKey {
		return docArg{p.Name, docOwn}, nil
	}
	return docArg{}, wrongProject(arg, p, projectKey)
}
func vaultSlug(arg string) (string, error) {
	target, _ := vpath.SplitAnchor(strings.TrimSpace(arg))
	if !strings.HasPrefix(target, "/") {
		return Slugify(target), nil
	}
	p, err := ParseAddress(target, vpath.CollectionKnowledge)
	if err != nil {
		return "", err
	}
	if p.Project != vpath.GlobalKey {
		return "", ErrUsage("not_global", arg+" is a project entry, not a vault entry", "trellis knowledge show "+arg)
	}
	return p.Name, nil
}
