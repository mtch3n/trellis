// Package address parses Trellis addresses: the names of a project, and of
// the things inside it, independently of any local directory.
//
// A .trellis marker holds one of two shapes:
//
//	/KEY
//	/KEY/boards/<slug>
package address

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// GlobalKey names the global vault. It is never a project.
const GlobalKey = "GLOBAL"

// CollectionBoards is the collection a marker may name inside a project.
const CollectionBoards = "boards"

// MarkerShapes lists the forms a marker accepts, for error messages.
const MarkerShapes = "/KEY or /KEY/boards/<slug>"

var keyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)

// Address is a parsed address: a project, and optionally one named item in
// one of its collections.
type Address struct {
	Project    string // upper-case project key
	Collection string // "" when the path names the project itself
	Name       string // the item within Collection
}

// Project names a project.
func Project(key string) Address { return Address{Project: key} }

// Board names a board by slug.
func Board(key, slug string) Address {
	return Address{Project: key, Collection: CollectionBoards, Name: slug}
}

// Board is the board slug the path names, or "".
func (p Address) Board() string {
	if p.Collection == CollectionBoards {
		return p.Name
	}
	return ""
}

// String renders the canonical form.
func (p Address) String() string {
	if p.Collection == "" {
		return "/" + p.Project
	}
	return "/" + p.Project + "/" + p.Collection + "/" + p.Name
}

// ParseMarker reads the content of a .trellis file. Surrounding whitespace is
// ignored, and the key is case-insensitive and returned upper-case. A bare key
// -- the marker format before addresses -- is rejected with a message that
// says how to fix it.
func ParseMarker(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Address{}, errors.New("empty; expected " + MarkerShapes)
	}
	if strings.ContainsAny(s, "\r\n") {
		return Address{}, errors.New("more than one line; expected " + MarkerShapes)
	}
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		if ValidKey(strings.ToUpper(s)) {
			return Address{}, fmt.Errorf("%q is the old bare-key format; write /%s instead", s, strings.ToUpper(s))
		}
		return Address{}, fmt.Errorf("%q is not a marker; expected %s", s, MarkerShapes)
	}
	segs := strings.Split(rest, "/")
	key, err := projectKey(segs[0])
	if err != nil {
		return Address{}, err
	}
	switch {
	case len(segs) == 1:
		return Project(key), nil
	case len(segs) == 3 && segs[1] == CollectionBoards:
		if !ValidSlug(segs[2]) {
			return Address{}, fmt.Errorf("%q is not a board slug: use lower-case letters and digits joined by single hyphens", segs[2])
		}
		return Board(key, segs[2]), nil
	default:
		return Address{}, fmt.Errorf("%q is not a marker; expected %s", s, MarkerShapes)
	}
}

// projectKey validates the first segment of a path as a project key.
func projectKey(seg string) (string, error) {
	key := strings.ToUpper(seg)
	if !ValidKey(key) {
		return "", fmt.Errorf("%q is not a project key: use letters, digits and single hyphens, starting with a letter", seg)
	}
	if key == GlobalKey {
		return "", errors.New("GLOBAL is the global vault, not a project")
	}
	return key, nil
}

// ValidKey reports whether key is a well-formed project key. It does not
// check reservation: GLOBAL is well-formed and reserved.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// ValidSlug reports whether s has the shape createBoard gives a slug:
// lower-case letters and digits, in runs joined by single hyphens. Letters
// are Unicode letters, matching slugify in internal/core/board.go.
func ValidSlug(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") || strings.Contains(s, "--") {
		return false
	}
	for _, r := range s {
		switch {
		case r == '-', unicode.IsDigit(r):
		case unicode.IsLetter(r) && !unicode.IsUpper(r) && !unicode.IsTitle(r):
		default:
			return false
		}
	}
	return true
}

// KeyFromName derives a default project key from a directory name:
// upper-case, every run of characters outside A-Z and 0-9 becomes one
// hyphen, and a leading digit gets a P prefix. It returns "" when nothing
// usable remains; the caller must then ask for an explicit key.
func KeyFromName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	key := strings.TrimSuffix(b.String(), "-")
	if key != "" && key[0] >= '0' && key[0] <= '9' {
		key = "P" + key
	}
	return key
}

// The collections an absolute address can name, beside CollectionBoards.
const (
	CollectionCards     = "cards"
	CollectionVault     = "vault"
	CollectionArtifacts = "artifacts"
)

var (
	cardRefRE = regexp.MustCompile(`^[^/#\s-][^/#\s]*-[0-9]+$`)
	// An entry slug is a path: one or more segments, each a plain slug.
	entrySlugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(/[a-z0-9]+(-[a-z0-9]+)*)*$`)
)

func Entry(key, slug string) Address {
	return Address{Project: key, Collection: CollectionVault, Name: slug}
}
func GlobalEntry(slug string) Address { return Entry(GlobalKey, slug) }
func Card(key, ref string) Address {
	return Address{Project: key, Collection: CollectionCards, Name: ref}
}
func Artifact(key, name string) Address {
	return Address{Project: key, Collection: CollectionArtifacts, Name: name}
}
func SplitAnchor(s string) (target, anchor string) { target, anchor, _ = strings.Cut(s, "#"); return }

// Parse reads an absolute address: /KEY/cards/<ref>, /KEY/vault/<slug>,
// /GLOBAL/vault/<slug> or /KEY/artifacts/<name>. Keys and card refs are
// case-insensitive and come back upper-case.
//
// It checks shape only: whether the object exists is for the caller to check
// against the database. This is the subset of the grammar in
// docs/superpowers/specs/2026-09-16-virtual-paths-design.md that a template's
// verify rule needs — no boards, no anchors — and the full layer extends it.
func Parse(s string) (Address, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		return Address{}, fmt.Errorf("%q is not an address: an address starts with /", s)
	}
	if strings.Contains(s, "#") {
		return Address{}, fmt.Errorf("%q carries an anchor; an address names the entry, not a heading", s)
	}
	segs := strings.Split(rest, "/")
	key := strings.ToUpper(segs[0])
	if !ValidKey(key) {
		return Address{}, fmt.Errorf("%q is not a project key", segs[0])
	}
	if len(segs) == 1 {
		if key == GlobalKey {
			return Address{}, errors.New("GLOBAL holds only vault entries")
		}
		return Project(key), nil
	}
	// Only an entry slug has more than one segment: its directories.
	if len(segs) < 3 || (len(segs) > 3 && segs[1] != CollectionVault) {
		return Address{}, fmt.Errorf("%q is not an address; expected /KEY/<collection>/<name>", s)
	}
	collection, name := segs[1], strings.Join(segs[2:], "/")
	if key == GlobalKey && collection != CollectionVault {
		return Address{}, errors.New("GLOBAL holds only vault entries")
	}
	switch collection {
	case CollectionBoards:
		if !ValidSlug(name) {
			return Address{}, fmt.Errorf("%q is not a board slug", name)
		}
	case CollectionCards:
		name = strings.ToUpper(name)
		if !ValidCardRef(name) {
			return Address{}, fmt.Errorf("%q is not a card ref", segs[2])
		}
	case CollectionVault:
		if !ValidEntrySlug(name) {
			return Address{}, fmt.Errorf("%q is not an entry slug", name)
		}
	case CollectionArtifacts:
		if !ValidArtifactName(name) {
			return Address{}, fmt.Errorf("%q is not an artifact name", name)
		}
	default:
		return Address{}, fmt.Errorf("%q is not a collection", collection)
	}
	return Address{Project: key, Collection: collection, Name: name}, nil
}

func ValidCardRef(s string) bool   { return s == strings.ToUpper(s) && cardRefRE.MatchString(s) }
func ValidEntrySlug(s string) bool { return entrySlugRE.MatchString(s) }
func ValidArtifactName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}
