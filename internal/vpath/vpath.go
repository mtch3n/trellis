// Package vpath parses Trellis virtual paths: the addresses that name a
// project, and the things inside it, independently of any local directory.
//
// A .trellis pin holds one of two shapes:
//
//	/KEY
//	/KEY/boards/<slug>
package vpath

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// GlobalKey names the global knowledge vault. It is never a project.
const GlobalKey = "GLOBAL"

// CollectionBoards is the collection a pin may name inside a project.
const CollectionBoards = "boards"

// PinShapes lists the forms a pin accepts, for error messages.
const PinShapes = "/KEY or /KEY/boards/<slug>"

var keyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)

// Path is a parsed virtual path: a project, and optionally one named item in
// one of its collections.
type Path struct {
	Project    string // upper-case project key
	Collection string // "" when the path names the project itself
	Name       string // the item within Collection
}

// ProjectPath names a project.
func ProjectPath(key string) Path { return Path{Project: key} }

// BoardPath names a board by slug.
func BoardPath(key, slug string) Path {
	return Path{Project: key, Collection: CollectionBoards, Name: slug}
}

// Board is the board slug the path names, or "".
func (p Path) Board() string {
	if p.Collection == CollectionBoards {
		return p.Name
	}
	return ""
}

// String renders the canonical form.
func (p Path) String() string {
	if p.Collection == "" {
		return "/" + p.Project
	}
	return "/" + p.Project + "/" + p.Collection + "/" + p.Name
}

// ParsePin reads the content of a .trellis file. Surrounding whitespace is
// ignored, and the key is case-insensitive and returned upper-case. A bare key
// -- the pin format before virtual paths -- is rejected with a message that
// says how to fix it.
func ParsePin(s string) (Path, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Path{}, errors.New("empty; expected " + PinShapes)
	}
	if strings.ContainsAny(s, "\r\n") {
		return Path{}, errors.New("more than one line; expected " + PinShapes)
	}
	rest, ok := strings.CutPrefix(s, "/")
	if !ok {
		if ValidKey(strings.ToUpper(s)) {
			return Path{}, fmt.Errorf("%q is the old bare-key format; write /%s instead", s, strings.ToUpper(s))
		}
		return Path{}, fmt.Errorf("%q is not a pin; expected %s", s, PinShapes)
	}
	segs := strings.Split(rest, "/")
	key, err := projectKey(segs[0])
	if err != nil {
		return Path{}, err
	}
	switch {
	case len(segs) == 1:
		return ProjectPath(key), nil
	case len(segs) == 3 && segs[1] == CollectionBoards:
		if !ValidSlug(segs[2]) {
			return Path{}, fmt.Errorf("%q is not a board slug: use lower-case letters and digits joined by single hyphens", segs[2])
		}
		return BoardPath(key, segs[2]), nil
	default:
		return Path{}, fmt.Errorf("%q is not a pin; expected %s", s, PinShapes)
	}
}

// projectKey validates the first segment of a path as a project key.
func projectKey(seg string) (string, error) {
	key := strings.ToUpper(seg)
	if !ValidKey(key) {
		return "", fmt.Errorf("%q is not a project key: use letters, digits and single hyphens, starting with a letter", seg)
	}
	if key == GlobalKey {
		return "", errors.New("GLOBAL is the global knowledge vault, not a project")
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
