package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the YAML header of a knowledge file. Everything else about a
// doc — which project, which links, when it was read — lives in the database;
// these are the fields a human editing the file in Obsidian would expect to own.
type Frontmatter struct {
	Title   string `yaml:"title"`
	Type    string `yaml:"type,omitempty"`
	Status  string `yaml:"status,omitempty"`
	Summary string `yaml:"summary,omitempty"`
	// Provenance names the ingestion path, not the author:
	// authored, prompted or extracted. Empty means unrecorded.
	Provenance string `yaml:"provenance,omitempty"`
	// Private is the author's declaration that this body must not be
	// transmitted automatically. Egress, not access: see
	// docs/superpowers/specs/2026-09-16-knowledge-disclosure-policy-design.md.
	// Typed bool on purpose — a non-boolean value is a parse failure rather
	// than a silent false, because failing open here cannot be undone.
	Private bool     `yaml:"private,omitempty"`
	Board   string   `yaml:"board,omitempty"` // association, never ownership (§10.1)
	Tags    []string `yaml:"tags,omitempty"`
	Labels  []string `yaml:"labels,omitempty"`
	// Artifacts names the files attached to this entry, by stored artifact
	// name. The list is the record; link rows are derived from it.
	Artifacts []string `yaml:"artifacts,omitempty"`
	Created   string   `yaml:"created,omitempty"`
	Updated   string   `yaml:"updated,omitempty"`
}

// SplitFrontmatter separates the YAML header from the body. A file without one
// is not an error: a hand-written note is still a note.
func SplitFrontmatter(raw string) (Frontmatter, string, error) {
	var fm Frontmatter
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return fm, s, nil
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return fm, s, nil
	}
	header := s[4 : 4+end]
	body := strings.TrimPrefix(s[4+end+4:], "\n")
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return fm, body, ErrUsage("bad_frontmatter", "the YAML frontmatter does not parse: "+err.Error(), "")
	}
	return fm, body, nil
}

// splitDocFile is SplitFrontmatter for a file read from path, and names that
// file when the header does not parse. SplitFrontmatter sees only the text, but
// its callers sweep the whole vault, and one bad value in any file fails every
// command; an error that does not say which file is one nobody can act on.
func splitDocFile(path string, raw []byte) (Frontmatter, string, error) {
	fm, body, err := SplitFrontmatter(string(raw))
	if e, ok := errors.AsType[*Error](err); ok {
		named := *e
		named.Msg = path + ": " + e.Msg
		return fm, body, &named
	}
	return fm, body, err
}

// RenderDoc writes frontmatter and body back to file form.
func RenderDoc(fm Frontmatter, body string) string {
	header, err := yaml.Marshal(fm)
	if err != nil { // a struct of strings cannot fail to marshal
		panic(err)
	}
	return "---\n" + string(header) + "---\n\n" + strings.TrimLeft(body, "\n")
}

// ContentHash is what detects an external edit (§5). It covers the whole file,
// frontmatter included: retagging a doc is a change.
func ContentHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

var (
	nonSlug    = regexp.MustCompile(`[^a-z0-9]+`)
	wikiLinkRE = regexp.MustCompile(`\[\[([^\]|#]+)(#[^\]|]+)?(\|[^\]]+)?\]\]`)
	inlineTag  = regexp.MustCompile(`(^|\s)#([a-z0-9][a-z0-9\-_/]*)`)
	headingRE  = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
	fenceRE    = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")
)

// Slugify turns a title into a filename-safe slug. Collisions are resolved by
// the caller the same way board slugs are: design, then design-2.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlug.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Reference is one [[wikilink]] found in a body.
type Reference struct {
	Raw        string // the literal text between the brackets, e.g. "XPSCTL/design"
	ProjectKey string // set when the reference was qualified
	Slug       string
	Anchor     string // heading slug, without the #
}

// ParseWikilinks finds every [[link]], [[link#anchor]] and [[KEY/link]] in a
// body. Code spans and fenced blocks are skipped: an agent pasting a snippet
// that happens to contain brackets is not making a reference.
func ParseWikilinks(body string) []Reference {
	clean := fenceRE.ReplaceAllString(body, "")
	seen := map[string]bool{}
	var refs []Reference
	for _, m := range wikiLinkRE.FindAllStringSubmatch(clean, -1) {
		target := strings.TrimSpace(m[1])
		anchor := Slugify(strings.TrimPrefix(m[2], "#"))
		key := ""
		if k, rest, ok := strings.Cut(target, "/"); ok {
			key, target = strings.ToUpper(k), rest
		}
		ref := Reference{
			Raw: strings.TrimSpace(m[1]) + m[2], ProjectKey: key,
			Slug: Slugify(target), Anchor: anchor,
		}
		if ref.Slug == "" || seen[ref.Raw] {
			continue
		}
		seen[ref.Raw] = true
		refs = append(refs, ref)
	}
	return refs
}

// ParseInlineTags finds #tags outside code. A heading (`## Thing`) is not a tag:
// it is followed by a space, which the pattern requires not to be there.
func ParseInlineTags(body string) []string {
	clean := fenceRE.ReplaceAllString(body, "")
	clean = headingRE.ReplaceAllString(clean, "")
	seen := map[string]bool{}
	var tags []string
	for _, m := range inlineTag.FindAllStringSubmatch(clean, -1) {
		t := strings.ToLower(m[2])
		if seen[t] {
			continue
		}
		seen[t] = true
		tags = append(tags, t)
	}
	return tags
}

// HeadingAnchors lists the slugified headings of a body, which is what an
// anchor in a reference has to match for the link to resolve.
func HeadingAnchors(body string) []string {
	clean := fenceRE.ReplaceAllString(body, "")
	var out []string
	for _, m := range headingRE.FindAllStringSubmatch(clean, -1) {
		out = append(out, Slugify(m[1]))
	}
	return out
}

// FirstParagraph is the last fallback for a pinned recap (§10.5).
func FirstParagraph(body string) string {
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, "#") || strings.HasPrefix(block, "<!--") {
			continue
		}
		return strings.Join(strings.Fields(block), " ")
	}
	return ""
}

// ParseReference reads one reference in its stored form — "slug",
// "slug#anchor" or "KEY/slug#anchor" — back into a Reference. It is the
// inverse of the Raw field ParseWikilinks writes, so a link recovered from
// the database resolves exactly as it did when the body was parsed.
func ParseReference(raw string) Reference {
	target, anchor, _ := strings.Cut(strings.TrimSpace(raw), "#")
	ref := Reference{Raw: strings.TrimSpace(raw), Anchor: Slugify(anchor)}
	if key, rest, ok := strings.Cut(target, "/"); ok {
		ref.ProjectKey, target = strings.ToUpper(key), rest
	}
	ref.Slug = Slugify(target)
	return ref
}
