package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
)

// Diagnostic is one thing lint reports about the vault. Lint reports; it never
// repairs.
type Diagnostic struct {
	Kind  string `json:"kind"` // template_violation, unknown_template, stub, ambiguous_link, broken_anchor, orphan, wrong_collection, bad_path, missing_artifact, unknown_field, deep_directory, long_directory_name, similar_directory
	Entry string `json:"doc"`  // the address of the entry it is about
	Ref   string `json:"ref,omitempty"`
	Fix   string `json:"fix"`
}

// Lint reports stubs, broken anchors and orphans (§10). Each diagnostic names
// what to do about it; none of them is an error, because a vault under
// construction is full of all three.
func (c *Core) Lint(ctx context.Context, projectID string) ([]Diagnostic, error) {
	out := []Diagnostic{}
	entries, err := c.ListEntries(ctx, projectID, EntryFilter{})
	if err != nil {
		return nil, err
	}
	knownFields, err := c.knownExtraFields(ctx)
	if err != nil {
		return nil, err
	}
	// Anchors are checked on the entry a link resolved to, by id, wherever it
	// lives. The project's own entries are in hand already; a target in
	// another project or in the vault is read from its file the first time a
	// link needs it.
	targets := linkTargets{}
	for _, e := range entries {
		targets[e.ID] = linkTarget{ref: e.Ref, anchors: anchorSet(e.BodyMD)}
	}

	// A template is read once however many entries use it.
	templates := map[string]Template{}
	templateErrs := map[string]error{}
	templateOf := func(name string) (Template, error) {
		if t, ok := templates[name]; ok {
			return t, nil
		}
		if err, ok := templateErrs[name]; ok {
			return Template{}, err
		}
		t, err := c.templateNamed(name)
		if err != nil {
			templateErrs[name] = err
			return Template{}, err
		}
		templates[name] = t
		return t, nil
	}

	err = c.Tx(ctx, func(tx *sqlx.Tx) error {
		for _, e := range entries {
			var rows []struct {
				ToRaw  string         `db:"to_raw"`
				ToID   sql.NullString `db:"to_id"`
				Anchor sql.NullString `db:"anchor"`
			}
			if err := tx.Select(&rows,
				`SELECT to_raw, to_id, anchor FROM link
				 WHERE from_type = 'entry' AND from_id = ? AND rel = 'wikilink' ORDER BY to_raw`, e.ID); err != nil {
				return err
			}
			for _, r := range rows {
				f, ok, err := linkDiagnostic(c, tx, targets, e, r.ToRaw, r.ToID, r.Anchor)
				if err != nil {
					return err
				}
				if ok {
					out = append(out, f)
				}
			}

			var artifactStubs []string
			if err := tx.Select(&artifactStubs,
				`SELECT to_raw FROM link
				 WHERE from_type = 'entry' AND from_id = ? AND rel = 'artifact' AND to_id IS NULL
				 ORDER BY to_raw`, e.ID); err != nil {
				return err
			}
			for _, name := range artifactStubs {
				out = append(out, Diagnostic{Kind: "missing_artifact", Entry: e.Ref, Ref: name,
					Fix: "trellis artifact add <file>   # no artifact is named " + name})
			}

			raw, err := os.ReadFile(e.Path)
			if err != nil {
				return err
			}
			fm, body, err := splitEntryFile(e.Path, raw)
			if err != nil {
				return err
			}
			// A hand edit can break the entry's template; Trellis's own
			// writes cannot, so this is where that shows.
			if fm.Template != "" {
				t, err := templateOf(fm.Template)
				switch {
				case isCode(err, "unknown_template"), isCode(err, "bad_template_name"):
					// A hand-edited value that is not a real template name --
					// whether none exists by that name or the name itself is
					// not a legal one (say, one a path-traversal attempt left
					// behind) -- is reported the same way: neither aborts the
					// rest of the vault's lint.
					out = append(out, Diagnostic{Kind: "unknown_template", Entry: e.Ref, Ref: fm.Template,
						Fix: "trellis knowledge edit " + e.Ref + " --template <name>   # or --template \"\" for none"})
				case err != nil:
					return err
				default:
					problems, err := c.templateProblems(tx, e.ProjectID, t, frontmatterFields(fm), body, true)
					if err != nil {
						return err
					}
					for _, problem := range problems {
						out = append(out, Diagnostic{Kind: "template_violation", Entry: e.Ref, Ref: problem,
							Fix: "trellis knowledge edit " + e.Ref + "   # " + t.Name + ": " + problem})
					}
				}
			}
			var extraKeys []string
			for k := range fm.Extra {
				extraKeys = append(extraKeys, k)
			}
			slices.Sort(extraKeys)
			for _, k := range extraKeys {
				if !knownFields[k] {
					out = append(out, Diagnostic{Kind: "unknown_field", Entry: e.Ref, Ref: k,
						Fix: "trellis knowledge template ls   # " + k + " is not in any template's required or choices"})
				}
			}

			var inbound int
			if err := tx.Get(&inbound,
				`SELECT COUNT(*) FROM link WHERE to_type = 'entry' AND to_id = ?`, e.ID); err != nil {
				return err
			}
			// Stubs count as outbound: an entry whose only link is broken is
			// reported as a stub, and reporting it as an orphan too would be
			// two diagnostics for one fix. An attached artifact is not a
			// connection to another entry or card, so it does not count.
			var outbound int
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'entry' AND from_id = ? AND rel != 'artifact'`,
				e.ID); err != nil {
				return err
			}
			if inbound == 0 && outbound == 0 {
				out = append(out, Diagnostic{Kind: "orphan", Entry: e.Ref,
					Fix: "link it from a card or another entry, or remove it"})
			}

			if dirs := strings.Split(e.Slug, "/"); len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
				if len(dirs) >= 3 {
					out = append(out, Diagnostic{Kind: "deep_directory", Entry: e.Ref,
						Ref: strings.Join(dirs, "/"),
						Fix: "trellis knowledge mv " + e.Slug + " <a shallower path>   # depth is a design smell past two levels"})
				}
				for _, seg := range dirs {
					if len(seg) > 30 {
						out = append(out, Diagnostic{Kind: "long_directory_name", Entry: e.Ref, Ref: seg,
							Fix: "trellis knowledge mv " + e.Slug + " <a shorter directory name>"})
					}
				}
			}
		}

		dirs, derr := c.projectDirectories(tx, projectID)
		if derr != nil {
			return derr
		}
		slices.Sort(dirs)
		for i, a := range dirs {
			for _, b := range dirs[i+1:] {
				if resembles(a, b) {
					out = append(out, Diagnostic{Kind: "similar_directory", Ref: a + ", " + b,
						Fix: "trellis knowledge mv <an entry under one> <the other>   # or leave both if they mean different things"})
				}
			}
		}

		return nil
	})
	slices.SortStableFunc(out, func(a, b Diagnostic) int { return cmp.Compare(a.Kind, b.Kind) })
	return out, err
}

// knownExtraFields is every field name any template on disk currently
// names, in required or choices. A frontmatter key outside this set, and
// outside the Frontmatter struct's own fields, is unrecognised no matter
// which template, if any, produced the entry — an entry does not
// remember which template created it. A template that fails to parse
// names nothing here; Lint reports the entry's key regardless, which is
// the safer default when a template is broken.
func (c *Core) knownExtraFields(ctx context.Context) (map[string]bool, error) {
	dir, err := c.templatesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		t, err := loadTemplate(dir, name)
		if err != nil {
			continue
		}
		for _, f := range t.Required {
			known[f] = true
		}
		for f := range t.Choices {
			known[f] = true
		}
	}
	return known, nil
}

// linkDiagnostic judges one wikilink held by e.
func linkDiagnostic(c *Core, tx *sqlx.Tx, targets linkTargets, e Entry, raw string,
	toID, anchor sql.NullString) (Diagnostic, bool, error) {
	ref := ParseReference(raw)
	if !toID.Valid {
		target, _ := address.SplitAnchor(raw)
		target = strings.TrimSpace(target)
		if strings.HasPrefix(target, "/") && ref.ProjectKey == "" {
			return addressDiagnostic(e, raw, target), true, nil
		}
		kind, fix := "stub", stubFix(ref)
		leaf, _, _ := strings.Cut(ref.Raw, "#")
		if leaf = normalizeSlugPath(leaf); !strings.Contains(leaf, "/") {
			matches, err := entriesWithLeaf(tx, e.ProjectID, leaf)
			if err != nil {
				return Diagnostic{}, false, err
			}
			if len(matches) > 1 {
				kind, fix = "ambiguous_link", "trellis knowledge show <full path>   # "+ref.Raw+" matches more than one entry"
			}
		}
		return Diagnostic{Kind: kind, Entry: e.Ref, Ref: raw, Fix: fix}, true, nil
	}
	if !anchor.Valid || anchor.String == "" {
		return Diagnostic{}, false, nil
	}
	target, found, err := targets.get(c, tx, toID.String)
	if err != nil || !found || target.anchors[anchor.String] {
		return Diagnostic{}, false, err
	}
	return Diagnostic{Kind: "broken_anchor", Entry: e.Ref, Ref: raw,
		Fix: "trellis knowledge show " + target.ref + "   # check its headings"}, true, nil
}

// linkTarget is what an anchor check needs from the entry a link resolved to.
type linkTarget struct {
	ref     string
	anchors map[string]bool
}

// linkTargets caches link targets by entry id.
type linkTargets map[string]linkTarget

// get returns the target with id, reading its file the first time. found is
// false when the row or its file has gone, which is not an anchor problem.
func (t linkTargets) get(c *Core, tx *sqlx.Tx, id string) (linkTarget, bool, error) {
	if lt, ok := t[id]; ok {
		return lt, true, nil
	}
	var row struct {
		Slug   string `db:"slug"`
		Global bool   `db:"global"`
		Key    string `db:"key"`
		Ref    string `db:"ref"`
	}
	err := tx.Get(&row, `SELECT k.slug, k.global, p.key, `+entryAddressSQL+` AS ref
		FROM entry k JOIN project p ON p.id = k.project_id WHERE k.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	path := c.entryPath(row.Key, row.Global, row.Slug)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	_, body, err := splitEntryFile(path, raw)
	if err != nil {
		return linkTarget{}, false, err
	}
	lt := linkTarget{ref: row.Ref, anchors: anchorSet(body)}
	t[id] = lt
	return lt, true, nil
}

func anchorSet(body string) map[string]bool {
	set := map[string]bool{}
	for _, a := range HeadingAnchors(body) {
		set[a] = true
	}
	return set
}

// addressDiagnostic explains an absolute link target that names no
// entry: a malformed address, or one in another collection.
func addressDiagnostic(e Entry, raw, target string) Diagnostic {
	f := Diagnostic{Kind: "wrong_collection", Entry: e.Ref, Ref: raw,
		Fix: "trellis knowledge edit " + e.Ref + " --body @file   # a wikilink names /KEY/vault/<slug>"}
	if _, err := address.Parse(target); err != nil {
		f.Kind = "bad_path"
	}
	return f
}

// stubFix says how to write the entry a stub is waiting for.
func stubFix(ref Reference) string {
	switch ref.ProjectKey {
	case "":
		return `trellis knowledge new --title "` + ref.Raw + `"`
	case address.GlobalKey:
		return `trellis knowledge new --title "` + ref.Slug + `"   # then a human promotes it`
	}
	return "trellis --project " + ref.ProjectKey + ` knowledge new --title "` + ref.Slug + `"`
}
