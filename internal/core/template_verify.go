package core

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)

// wikilinkTarget reports whether s is written as a wikilink, [[target]] or
// [[target|alias]], and if so returns target.
func wikilinkTarget(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[[") || !strings.HasSuffix(s, "]]") || len(s) < 4 {
		return "", false
	}
	inner := s[2 : len(s)-2]
	inner, _, _ = strings.Cut(inner, "|")
	return strings.TrimSpace(inner), true
}

// addressPattern is the shape of an absolute Trellis address: a slash, a
// project key, one of the three known collections, and a name. It is
// deliberately loose about the key and name — vpath.Parse validates those
// — so this only decides which values, or substrings of a document body,
// are worth attempting to resolve as an address at all.
const addressPattern = `/[A-Za-z][A-Za-z0-9-]*/(?:cards|knowledge|artifacts)/\S+`

// addressShapeRE matches a whole value shaped like an absolute address. A
// source or other verified field value earns a vpath.Parse attempt only
// when it has this shape in full; any other "/"-prefixed value — a
// filesystem path such as "/usr/share/doc/x.txt:10" — is external prose
// and passes verify unchecked.
var addressShapeRE = regexp.MustCompile(`^` + addressPattern + `$`)

// absoluteAddressRE finds an address-shaped substring in free text, but
// only when it starts the text or immediately follows whitespace or "(" —
// otherwise a URL ("https://example.com/foo/cards/bar") or a relative path
// ("src/api/cards/handler.go") would yield a false address, since in both
// the character right before the matching slash is neither a boundary nor
// the start of the string. Group 1 is that boundary character (or empty,
// at the very start of the text); group 2 is the address itself.
var absoluteAddressRE = regexp.MustCompile(`(^|[\s(])(` + addressPattern + `)`)

// bodyAbsoluteAddresses finds every substring of body shaped like an
// absolute address, skipping code spans and fences the same way
// ParseWikilinks does, and trimming trailing punctuation a sentence would
// leave attached ("... see /KEY/cards/KEY-12.").
func bodyAbsoluteAddresses(body string) []string {
	clean := fenceRE.ReplaceAllString(body, "")
	seen := map[string]bool{}
	var out []string
	for _, m := range absoluteAddressRE.FindAllStringSubmatch(clean, -1) {
		addr := strings.TrimRight(m[2], ".,;:)]")
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}

// resolvesInternalReference reports whether s is recognised as an internal
// reference — a wikilink or an absolute Trellis address — and, when it is,
// whether the object it names exists. A value that is neither form is not
// a reference at all: isRef is false and ok means nothing. A "/"-prefixed
// value only counts as an address candidate when it has the address shape
// in full (addressShapeRE); any other "/"-prefixed value, such as a
// filesystem path, is external and is never even offered to vpath.Parse.
// Every accepted form is resolved here, in one place, so a later layer
// that widens what resolves (cross-project wikilinks, once virtual paths
// ship) changes only this function.
func (c *Core) resolvesInternalReference(tx *sqlx.Tx, projectID, s string) (ok, isRef bool, err error) {
	if target, is := wikilinkTarget(s); is {
		toID, err := c.resolveDocRef(tx, projectID, ParseReference(target))
		return toID != nil, true, err
	}
	trimmed := strings.TrimSpace(s)
	if !addressShapeRE.MatchString(trimmed) {
		return false, false, nil
	}
	p, perr := vpath.Parse(trimmed)
	if perr != nil {
		// It has the shape of an address and does not even parse: an
		// unresolved reference, not prose that happens to look like one.
		return false, true, nil
	}
	if p.Collection == vpath.CollectionKnowledge && p.Project == "GLOBAL" {
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM knowledge WHERE slug = ? AND global = 1`, p.Name); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	}
	var destProjectID string
	if err := tx.Get(&destProjectID, `SELECT id FROM project WHERE key = ?`, p.Project); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, true, nil
		}
		return false, true, err
	}
	switch p.Collection {
	case vpath.CollectionCards:
		ref := ParseCardRef(p.Name)
		if ref.Seq <= 0 {
			return false, true, nil
		}
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM card WHERE seq = ? AND project_id = ?`, ref.Seq, destProjectID); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	case vpath.CollectionKnowledge:
		var n int
		if err := tx.Get(&n, `SELECT COUNT(*) FROM knowledge WHERE slug = ? AND project_id = ?`, p.Name, destProjectID); err != nil {
			return false, true, err
		}
		return n > 0, true, nil
	case vpath.CollectionArtifacts:
		toID, err := c.resolveArtifactName(tx, destProjectID, p.Name)
		if err != nil {
			return false, true, err
		}
		return toID != nil, true, nil
	}
	return false, true, nil
}

// verifyFieldValues checks every value of a verified field, returning the
// ones that do not resolve. A value that is not itself a wikilink or an
// absolute address is not an internal reference and is never flagged: a
// URL, a path:lines pointer, and prose all pass unchecked, which is the
// point of keeping sources free-form.
func (c *Core) verifyFieldValues(tx *sqlx.Tx, projectID string, values []string) ([]string, error) {
	var unresolved []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		ok, isRef, err := c.resolvesInternalReference(tx, projectID, v)
		if err != nil {
			return nil, err
		}
		if isRef && !ok {
			unresolved = append(unresolved, v)
		}
	}
	return unresolved, nil
}

// verifyBody checks every wikilink and absolute address written in a
// document body. Wikilinks resolve exactly as resolveDocRef does today —
// today's project-and-vault scope, not the cross-project resolution a
// later virtual-paths layer adds.
func (c *Core) verifyBody(tx *sqlx.Tx, projectID, body string) ([]string, error) {
	var unresolved []string
	for _, ref := range ParseWikilinks(body) {
		toID, err := c.resolveDocRef(tx, projectID, ref)
		if err != nil {
			return nil, err
		}
		if toID == nil {
			unresolved = append(unresolved, "[["+ref.Raw+"]]")
		}
	}
	for _, addr := range bodyAbsoluteAddresses(body) {
		ok, isRef, err := c.resolvesInternalReference(tx, projectID, addr)
		if err != nil {
			return nil, err
		}
		if isRef && !ok {
			unresolved = append(unresolved, addr)
		}
	}
	return unresolved, nil
}

// templateVerifyViolations runs a template's verify rule: every value of
// each named field, or every reference in the body when the field named is
// "body", must resolve. It opens its own read-only transaction rather than
// sharing the write transaction CreateKnowledge opens later, so a reject
// template still writes nothing when a reference fails to resolve.
func (c *Core) templateVerifyViolations(ctx context.Context, projectID string, t Template,
	fields map[string][]string, body string) ([]string, error) {
	if len(t.Verify) == 0 {
		return nil, nil
	}
	var out []string
	err := c.Tx(ctx, func(tx *sqlx.Tx) error {
		for _, name := range t.Verify {
			var refs []string
			var err error
			if name == "body" {
				refs, err = c.verifyBody(tx, projectID, body)
			} else {
				refs, err = c.verifyFieldValues(tx, projectID, fields[name])
			}
			if err != nil {
				return err
			}
			for _, v := range refs {
				out = append(out, name+" cites "+v+", which does not resolve")
			}
		}
		return nil
	})
	return out, err
}
