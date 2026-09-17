package core

import (
	"fmt"
	"strings"
	"uuid"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

// EntryAddress is a knowledge entry's canonical address: /KEY/vault/<slug>,
// or /GLOBAL/vault/<slug> once it is in the vault. key is ignored for a
// vault entry.
func EntryAddress(key string, global bool, slug string) string {
	if global {
		return address.GlobalEntry(slug).String()
	}
	return address.Entry(key, slug).String()
}

// entryAddressSQL is DocAddress in SQL, for queries that alias knowledge as k
// and project as p. TestEntryAddressSQLMatchesGo holds the two together.
const entryAddressSQL = `'/' || CASE WHEN k.global = 1 THEN '` + address.GlobalKey +
	`' ELSE p.key END || '/vault/' || k.slug`

func ParseAddress(arg, collection string) (address.Address, error) {
	p, err := address.Parse(arg)
	if err != nil {
		return address.Address{}, ErrUsage("bad_path", err.Error(), "trellis search <words>")
	}
	if p.Collection != collection {
		return address.Address{}, ErrUsage("wrong_collection", fmt.Sprintf("%s names %s, not %s", arg, p.Collection, collection), commandFor(p, arg))
	}
	return p, nil
}
func commandFor(p address.Address, arg string) string {
	switch p.Collection {
	case address.CollectionBoards:
		return "trellis board show --board " + arg
	case address.CollectionCards:
		return "trellis card show " + arg
	case address.CollectionVault:
		return "trellis knowledge show " + arg
	case address.CollectionArtifacts:
		return "trellis artifact ls --project " + p.Project
	}
	return "trellis card ls --project " + arg
}
func wrongProject(arg string, p address.Address, current string) error {
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

type entryScope int

const (
	entryRelative entryScope = iota
	entryOwn
	entryVault
)

type entryArg struct {
	slug  string
	scope entryScope
}

func readEntryArg(arg, projectKey string) (entryArg, error) {
	target, _ := address.SplitAnchor(strings.TrimSpace(arg))
	if !strings.HasPrefix(target, "/") {
		return entryArg{target, entryRelative}, nil
	}
	p, err := ParseAddress(target, address.CollectionVault)
	if err != nil {
		return entryArg{}, err
	}
	if p.Project == address.GlobalKey {
		return entryArg{p.Name, entryVault}, nil
	}
	if p.Project == projectKey {
		return entryArg{p.Name, entryOwn}, nil
	}
	return entryArg{}, wrongProject(arg, p, projectKey)
}
func vaultSlug(arg string) (string, error) {
	target, _ := address.SplitAnchor(strings.TrimSpace(arg))
	if !strings.HasPrefix(target, "/") {
		return target, nil
	}
	p, err := ParseAddress(target, address.CollectionVault)
	if err != nil {
		return "", err
	}
	if p.Project != address.GlobalKey {
		return "", ErrUsage("not_global", arg+" is a project entry, not a vault entry", "trellis knowledge show "+arg)
	}
	return p.Name, nil
}

// ArtifactAddress is an artifact's canonical address.
func ArtifactAddress(key, name string) string {
	return address.Artifact(key, name).String()
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
