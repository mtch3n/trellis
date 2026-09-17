package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/vpath"
)

// LintFinding is one problem with the vault. Lint reports; it never repairs.
type LintFinding struct {
	Kind string `json:"kind"` // template_violation, unknown_template, stub, ambiguous_link, broken_anchor, orphan, wrong_collection, bad_path, missing_artifact, unknown_field, deep_directory, long_directory_name, similar_directory
	Doc  string `json:"doc"`  // the address of the entry that holds the problem
	Ref  string `json:"ref,omitempty"`
	Fix  string `json:"fix"`
}

// Lint reports stubs, broken anchors and orphans (§10). Each finding names what
// to do about it; none of them is an error, because a vault under construction
// is full of all three.
func (c *Core) Lint(ctx context.Context, projectID string) ([]LintFinding, error) {
	out := []LintFinding{}
	docs, err := c.ListKnowledge(ctx, projectID, KnowledgeFilter{})
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
	for _, d := range docs {
		targets[d.ID] = linkTarget{ref: d.Ref, anchors: anchorSet(d.BodyMD)}
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
		for _, d := range docs {
			var rows []struct {
				ToRaw  string         `db:"to_raw"`
				ToID   sql.NullString `db:"to_id"`
				Anchor sql.NullString `db:"anchor"`
			}
			if err := tx.Select(&rows,
				`SELECT to_raw, to_id, anchor FROM link
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'wikilink' ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, r := range rows {
				f, ok, err := linkFinding(tx, targets, d, r.ToRaw, r.ToID, r.Anchor)
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
				 WHERE from_type = 'doc' AND from_id = ? AND rel = 'artifact' AND to_id IS NULL
				 ORDER BY to_raw`, d.ID); err != nil {
				return err
			}
			for _, name := range artifactStubs {
				var matches int
				if err := tx.Get(&matches,
					`SELECT COUNT(*) FROM artifact WHERE project_id = ? AND name = ?`,
					d.ProjectID, name); err != nil {
					return err
				}
				f := LintFinding{Kind: "missing_artifact", Doc: d.Ref, Ref: name,
					Fix: "trellis artifact add <file>   # no artifact is named " + name}
				if matches > 1 {
					f.Fix = "trellis artifact ls   # " + strconv.Itoa(matches) +
						" artifacts are named " + name + "; remove the extra ones"
				}
				out = append(out, f)
			}

			raw, err := os.ReadFile(d.Path)
			if err != nil {
				return err
			}
			fm, body, err := splitDocFile(d.Path, raw)
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
					out = append(out, LintFinding{Kind: "unknown_template", Doc: d.Ref, Ref: fm.Template,
						Fix: "trellis knowledge edit " + d.Ref + " --template <name>   # or --template \"\" for none"})
				case err != nil:
					return err
				default:
					problems, err := c.templateProblems(tx, d.ProjectID, t, frontmatterFields(fm), body, true)
					if err != nil {
						return err
					}
					for _, problem := range problems {
						out = append(out, LintFinding{Kind: "template_violation", Doc: d.Ref, Ref: problem,
							Fix: "trellis knowledge edit " + d.Ref + "   # " + t.Name + ": " + problem})
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
					out = append(out, LintFinding{Kind: "unknown_field", Doc: d.Ref, Ref: k,
						Fix: "trellis knowledge template ls   # " + k + " is not in any template's required or choices"})
				}
			}

			var inbound int
			if err := tx.Get(&inbound,
				`SELECT COUNT(*) FROM link WHERE to_type = 'doc' AND to_id = ?`, d.ID); err != nil {
				return err
			}
			// Stubs count as outbound: an entry whose only link is broken is
			// reported as a stub, and reporting it as an orphan too would be
			// two findings for one fix. An attached artifact is not a connection
			// to another entry or card, so it does not count.
			var outbound int
			if err := tx.Get(&outbound,
				`SELECT COUNT(*) FROM link WHERE from_type = 'doc' AND from_id = ? AND rel != 'artifact'`,
				d.ID); err != nil {
				return err
			}
			if inbound == 0 && outbound == 0 {
				out = append(out, LintFinding{Kind: "orphan", Doc: d.Ref,
					Fix: "link it from a card or another entry, or remove it"})
			}

			if dirs := strings.Split(d.Slug, "/"); len(dirs) > 1 {
				dirs = dirs[:len(dirs)-1]
				if len(dirs) >= 3 {
					out = append(out, LintFinding{Kind: "deep_directory", Doc: d.Ref,
						Ref: strings.Join(dirs, "/"),
						Fix: "trellis knowledge mv " + d.Slug + " <a shallower path>   # depth is a design smell past two levels"})
				}
				for _, seg := range dirs {
					if len(seg) > 30 {
						out = append(out, LintFinding{Kind: "long_directory_name", Doc: d.Ref, Ref: seg,
							Fix: "trellis knowledge mv " + d.Slug + " <a shorter directory name>"})
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
					out = append(out, LintFinding{Kind: "similar_directory", Ref: a + ", " + b,
						Fix: "trellis knowledge mv <an entry under one> <the other>   # or leave both if they mean different things"})
				}
			}
		}

		return nil
	})
	slices.SortStableFunc(out, func(a, b LintFinding) int { return cmp.Compare(a.Kind, b.Kind) })
	return out, err
}

// knownExtraFields is every field name any template on disk currently
// names, in required or choices. A frontmatter key outside this set, and
// outside the Frontmatter struct's own fields, is unrecognised no matter
// which template, if any, produced the document — a document does not
// remember which template created it. A template that fails to parse
// names nothing here; Lint reports the document's key regardless, which is
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

// linkFinding judges one wikilink held by d.
func linkFinding(tx *sqlx.Tx, targets linkTargets, d Knowledge, raw string,
	toID, anchor sql.NullString) (LintFinding, bool, error) {
	ref := ParseReference(raw)
	if !toID.Valid {
		target, _ := vpath.SplitAnchor(raw)
		target = strings.TrimSpace(target)
		if strings.HasPrefix(target, "/") && ref.ProjectKey == "" {
			return addressFinding(d, raw, target), true, nil
		}
		kind, fix := "stub", stubFix(ref)
		leaf, _, _ := strings.Cut(ref.Raw, "#")
		if leaf = normalizeSlugPath(leaf); !strings.Contains(leaf, "/") {
			matches, err := entriesWithLeaf(tx, d.ProjectID, leaf)
			if err != nil {
				return LintFinding{}, false, err
			}
			if len(matches) > 1 {
				kind, fix = "ambiguous_link", "trellis knowledge show <full path>   # "+ref.Raw+" matches more than one entry"
			}
		}
		return LintFinding{Kind: kind, Doc: d.Ref, Ref: raw, Fix: fix}, true, nil
	}
	if !anchor.Valid || anchor.String == "" {
		return LintFinding{}, false, nil
	}
	target, found, err := targets.get(tx, toID.String)
	if err != nil || !found || target.anchors[anchor.String] {
		return LintFinding{}, false, err
	}
	return LintFinding{Kind: "broken_anchor", Doc: d.Ref, Ref: raw,
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
func (t linkTargets) get(tx *sqlx.Tx, id string) (linkTarget, bool, error) {
	if lt, ok := t[id]; ok {
		return lt, true, nil
	}
	var row struct {
		Path string `db:"path"`
		Ref  string `db:"ref"`
	}
	err := tx.Get(&row, `SELECT k.path, `+docAddressSQL+` AS ref
		FROM knowledge k JOIN project p ON p.id = k.project_id WHERE k.id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	raw, err := os.ReadFile(row.Path)
	if errors.Is(err, os.ErrNotExist) {
		return linkTarget{}, false, nil
	}
	if err != nil {
		return linkTarget{}, false, err
	}
	_, body, err := splitDocFile(row.Path, raw)
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

// addressFinding explains an absolute link target that names no knowledge
// entry: a malformed address, or one in another collection.
func addressFinding(d Knowledge, raw, target string) LintFinding {
	f := LintFinding{Kind: "wrong_collection", Doc: d.Ref, Ref: raw,
		Fix: "trellis knowledge edit " + d.Ref + " --body @file   # a wikilink names /KEY/knowledge/<slug>"}
	if _, err := vpath.Parse(target); err != nil {
		f.Kind = "bad_path"
	}
	return f
}

// stubFix says how to write the entry a stub is waiting for.
func stubFix(ref Reference) string {
	switch ref.ProjectKey {
	case "":
		return `trellis knowledge new --title "` + ref.Raw + `"`
	case vpath.GlobalKey:
		return `trellis knowledge new --title "` + ref.Slug + `"   # then a human escalates it`
	}
	return "trellis --project " + ref.ProjectKey + ` knowledge new --title "` + ref.Slug + `"`
}
