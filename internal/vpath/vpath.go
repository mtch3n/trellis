// Package vpath parses absolute Trellis addresses: /KEY/cards/<ref>,
// /KEY/knowledge/<slug>, /GLOBAL/knowledge/<slug> and
// /KEY/artifacts/<name>.
//
// This is a deliberately small subset of the grammar in
// docs/superpowers/specs/2026-09-16-virtual-paths-design.md — cards,
// knowledge and artifacts only, no boards, no anchors, no cross-project
// argument handling. That spec's own layer is not built yet; TRELLIS-35
// pulls this much of it forward because a template's verify rule needs to
// recognise an address without waiting on it. When the full layer lands,
// it extends this package — Parse's signature and Path's fields come from
// that spec, not from this one — rather than replacing it.
package vpath

import (
	"fmt"
	"regexp"
	"strings"
)

// Collection names which kind of object a Path addresses.
type Collection string

const (
	CollectionCards     Collection = "cards"
	CollectionKnowledge Collection = "knowledge"
	CollectionArtifacts Collection = "artifacts"
)

// Path is a parsed absolute address, /Project/Collection/Name. Project is
// the project key, upper-cased, or GLOBAL for a vault knowledge entry.
// Name is collection-specific: a card's full ref, a knowledge slug, or an
// artifact's file name — never further parsed here.
type Path struct {
	Project    string
	Collection Collection
	Name       string
}

var (
	keyRE     = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)
	cardRefRE = regexp.MustCompile(`(?i)^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-[0-9]+$`)
	slugRE    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Parse reads an absolute address. It checks shape only: whether the
// object it names actually exists is for the caller to check against the
// database.
func Parse(s string) (Path, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return Path{}, fmt.Errorf("%q is not an absolute address", s)
	}
	project, collection, name := parts[1], parts[2], parts[3]

	if strings.EqualFold(project, "GLOBAL") && collection != "knowledge" {
		return Path{}, fmt.Errorf("GLOBAL is only valid with knowledge, not %s", collection)
	}

	switch collection {
	case "cards":
		if !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if !cardRefRE.MatchString(name) {
			return Path{}, fmt.Errorf("%q is not a valid card ref", name)
		}
		return Path{Project: strings.ToUpper(project), Collection: CollectionCards, Name: strings.ToUpper(name)}, nil
	case "knowledge":
		project = strings.ToUpper(project)
		if project != "GLOBAL" && !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if !slugRE.MatchString(name) {
			return Path{}, fmt.Errorf("%q is not a valid knowledge slug", name)
		}
		return Path{Project: project, Collection: CollectionKnowledge, Name: name}, nil
	case "artifacts":
		if !keyRE.MatchString(project) {
			return Path{}, fmt.Errorf("%q is not a valid project key", project)
		}
		if name == "." || name == ".." || strings.ContainsRune(name, 0) {
			return Path{}, fmt.Errorf("%q is not a valid artifact name", name)
		}
		return Path{Project: strings.ToUpper(project), Collection: CollectionArtifacts, Name: name}, nil
	default:
		return Path{}, fmt.Errorf("%q is not a known collection", collection)
	}
}
