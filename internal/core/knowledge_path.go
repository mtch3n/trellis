package core

import (
	"fmt"
	"strings"
)

// maxPathSegmentLen and maxRelSlugLen are the two length ceilings from the
// knowledge-paths design. NAME_MAX (255) is not the binding constraint on any
// of the three platforms Trellis supports; Windows MAX_PATH (260) is. The
// storage root plus username reserves roughly 80 of it, leaving 180 for the
// slug. 96 per segment is chosen against the live vault: its longest slug is
// 65 characters, so a lower ceiling would reject a document that already
// exists.
const (
	maxPathSegmentLen = 96
	maxRelSlugLen     = 180
)

// reservedDeviceNames are the Windows device names that stay reserved no
// matter what extension follows them: "con.md" cannot be opened on Windows.
// Slugify lower-cases, so these are checked in lower-case form; the input to
// this map is always something Slugify has already produced.
var reservedDeviceNames = buildReservedDeviceNames()

func buildReservedDeviceNames() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("com%d", i)] = true
		m[fmt.Sprintf("lpt%d", i)] = true
	}
	return m
}

// reservedLeafTaken reports whether slug's final path segment is a Windows
// reserved device name. "deployment/con" is exactly as unwritable on Windows
// as "con" alone; the directory it sits in does not matter.
func reservedLeafTaken(slug string) bool {
	leaf := slug
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		leaf = slug[i+1:]
	}
	return reservedDeviceNames[leaf]
}

// SlugifyPath validates and slugifies caller-supplied path input: `--in` on
// `knowledge new`, and the destination of `knowledge mv`. Every segment is
// slugified the way a flat slug already is, then rejoined with "/". Unlike a
// title-derived slug, this is text the caller typed on purpose, so every
// violation is rejected rather than silently fixed — see "A generated slug is
// truncated; an explicit path is rejected" in the design.
func SlugifyPath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "/") || strings.Contains(raw, `\`) {
		return "", ErrUsage("bad_path",
			fmt.Sprintf("%q must be a relative path using / to separate directories", raw), "")
	}
	parts := strings.Split(raw, "/")
	segs := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "." || trimmed == ".." {
			return "", ErrUsage("bad_path", fmt.Sprintf("%q may not contain \".\" or \"..\"", raw), "")
		}
		seg := Slugify(trimmed)
		if seg == "" {
			return "", ErrUsage("bad_path", fmt.Sprintf("%q has an empty path segment", raw), "")
		}
		if reservedDeviceNames[seg] {
			return "", ErrUsage("reserved_name",
				seg+" is a reserved device name on Windows and cannot be a directory or a slug", "")
		}
		if len(seg) > maxPathSegmentLen {
			return "", ErrUsage("path_too_long",
				fmt.Sprintf("%q is %d characters; one path segment is limited to %d", seg, len(seg), maxPathSegmentLen), "")
		}
		segs = append(segs, seg)
	}
	joined := strings.Join(segs, "/")
	if len(joined) > maxRelSlugLen {
		return "", ErrUsage("path_too_long",
			fmt.Sprintf("%q is %d characters; a knowledge path is limited to %d", joined, len(joined), maxRelSlugLen), "")
	}
	return joined, nil
}

// truncateSegment enforces the per-segment ceiling on a slug derived from a
// title rather than typed explicitly. A long title must still be writable —
// the full title survives in frontmatter and the slug is only an address —
// so it is truncated instead of rejected.
func truncateSegment(seg string) string {
	if len(seg) <= maxPathSegmentLen {
		return seg
	}
	return strings.TrimRight(seg[:maxPathSegmentLen], "-")
}

// normalizeSlugPath turns lookup input — a CLI argument, or (from Task 9
// onward) a wikilink target — into the path-shaped form stored in the slug
// column, without rejecting anything: a lookup that cannot possibly match
// should report "not found", not a validation error. Empty, "." and ".."
// segments are dropped rather than rejected, which is why a traversal
// attempt like "../etc" simply fails to match anything rather than escaping
// anywhere: it normalizes to "etc".
func normalizeSlugPath(raw string) string {
	parts := strings.Split(raw, "/")
	segs := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := Slugify(p); s != "" {
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// resembles reports whether two directory names are close enough to be the
// vocabulary drift the design guards against: "deploy" beside "deployment",
// or "deployment" beside "deplyoment". It leans toward false positives on
// purpose. Two names are never said to resemble themselves.
func resembles(a, b string) bool {
	if a == b {
		return false
	}
	shorter, longer := a, b
	if len(a) > len(b) {
		shorter, longer = b, a
	}
	if len(shorter) >= 4 && strings.HasPrefix(longer, shorter) {
		return true
	}
	if len(a) >= 5 && len(b) >= 5 && boundedEditDistance(a, b, 2) <= 2 {
		return true
	}
	return false
}

// boundedEditDistance computes the Levenshtein distance between a and b, or
// returns max+1 as soon as it can prove the true distance exceeds max. The
// resemblance check only ever needs to know "is it at most 2", so a length
// mismatch beyond max short-circuits immediately, and every row of the table
// bails out the moment its own minimum already exceeds max.
func boundedEditDistance(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if cur[j] < rowMin {
				rowMin = cur[j]
			}
		}
		if rowMin > max {
			return max + 1
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
