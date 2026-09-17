package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/mtch3n/trellis/internal/vpath"
	"gopkg.in/yaml.v3"
)

// Frontmatter is the YAML header of a knowledge file. Everything else about a
// doc — which project, which links, when it was read — lives in the database;
// these are the fields a human editing the file in Obsidian would expect to own.
type Frontmatter struct {
	Title    string `yaml:"title"`
	Template string `yaml:"template,omitempty"`
	Status   string `yaml:"status,omitempty"`
	Summary  string `yaml:"summary,omitempty"`
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
	// Sources cites what a claim in this entry is based on: a URL, a
	// path:lines pointer, a card ref, a wikilink, an absolute address, or
	// free prose. Free-form by design — recording that a claim was checked
	// against something, not that the something is true. A template's
	// verify rule (TRELLIS-35) checks only the internal-reference forms
	// (wikilinks and absolute addresses); everything else passes unchecked.
	Sources []string `yaml:"sources,omitempty"`
	Created string   `yaml:"created,omitempty"`
	Updated string   `yaml:"updated,omitempty"`
	// Extra keeps every frontmatter key this struct does not name. A
	// template may ask for a field ("owner", "severity") that has no
	// dedicated column here; without this, yaml.Unmarshal would silently
	// drop it, and the first `knowledge edit` — which re-renders the
	// frontmatter from this struct — would erase it from the file.
	Extra map[string]any `yaml:",inline"`
}

// splitHeader separates a "---\n...\n---\n" YAML header from the body of any
// file using that convention, without assuming what the header unmarshals
// into. SplitFrontmatter and the template parser in template.go both build
// on this, so the delimiter rule exists in exactly one place.
func splitHeader(raw string) (header, body string, ok bool) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", s, false
	}
	return s[4 : 4+end], strings.TrimPrefix(s[4+end+4:], "\n"), true
}

// SplitFrontmatter separates the YAML header from the body. A file without one
// is not an error: a hand-written note is still a note.
func SplitFrontmatter(raw string) (Frontmatter, string, error) {
	var fm Frontmatter
	header, body, ok := splitHeader(raw)
	if !ok {
		return fm, body, nil
	}
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

// Reference is one [[wikilink]] target, or a link target typed on the command
// line.
type Reference struct {
	Raw        string // the target as written, anchor included: "design", "/XPSCTL/vault/design#why"
	ProjectKey string // "" for a relative target, "GLOBAL" for the vault, else the key an address names
	Slug       string
	Anchor     string // heading slug, without the #
}

// ParseWikilinks finds every [[link]], [[link#anchor]] and
// [[/KEY/vault/link]] in a body. Code spans and fenced blocks are skipped:
// an agent pasting a snippet that happens to contain brackets is not making a
// reference.
func ParseWikilinks(body string) []Reference {
	clean := fenceRE.ReplaceAllString(body, "")
	seen := map[string]bool{}
	var refs []Reference
	for _, m := range wikiLinkRE.FindAllStringSubmatch(clean, -1) {
		ref := ParseReference(strings.TrimSpace(m[1]) + m[2])
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

// ParseReference reads one link target -- "slug", "slug#anchor",
// "/KEY/vault/slug#anchor" or "/GLOBAL/vault/slug" -- into a
// Reference. It is the inverse of Raw, so a link recovered from the database
// resolves exactly as it did when the body was parsed.
//
// A relative target is path-shaped (normalizeSlugPath) instead of flattened
// by a single Slugify call, per the knowledge-paths layer. An absolute target
// that names no knowledge entry -- a card address, or a malformed one -- keeps
// its text as the slug. No row can match that, so the link stays a stub and
// lint says why.
func ParseReference(raw string) Reference {
	raw = strings.TrimSpace(raw)
	target, anchor := vpath.SplitAnchor(raw)
	target = strings.TrimSpace(target)
	ref := Reference{Raw: raw, Anchor: Slugify(anchor)}
	if !strings.HasPrefix(target, "/") {
		ref.Slug = normalizeSlugPath(target)
		return ref
	}
	p, err := vpath.Parse(target)
	if err != nil || p.Collection != vpath.CollectionKnowledge {
		ref.Slug = target
		return ref
	}
	ref.ProjectKey, ref.Slug = p.Project, p.Name
	return ref
}

// RewriteWikilinks replaces link targets in text. fn sees every wikilink that
// ParseWikilinks would see -- code spans and fenced blocks are skipped the
// same way -- and returns the new target, anchor included, or false to leave
// the link alone. An alias after | is kept as written.
func RewriteWikilinks(text string, fn func(Reference) (string, bool)) string {
	code := fenceRE.FindAllStringIndex(text, -1)
	inCode := func(start, end int) bool {
		for _, span := range code {
			if start < span[1] && span[0] < end {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	last := 0
	for _, m := range wikiLinkRE.FindAllStringSubmatchIndex(text, -1) {
		if inCode(m[0], m[1]) {
			continue
		}
		raw := strings.TrimSpace(text[m[2]:m[3]])
		if m[4] >= 0 {
			raw += text[m[4]:m[5]]
		}
		next, ok := fn(ParseReference(raw))
		if !ok {
			continue
		}
		alias := ""
		if m[6] >= 0 {
			alias = text[m[6]:m[7]]
		}
		b.WriteString(text[last:m[0]])
		b.WriteString("[[" + next + alias + "]]")
		last = m[1]
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}
