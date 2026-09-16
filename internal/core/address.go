package core

import "github.com/mtch3n/trellis/internal/vpath"

// DocAddress is a knowledge entry's canonical address: /KEY/knowledge/<slug>,
// or /GLOBAL/knowledge/<slug> once it is in the vault. key is ignored for a
// vault entry.
func DocAddress(key string, global bool, slug string) string {
	if global {
		return vpath.GlobalKnowledgePath(slug).String()
	}
	return vpath.KnowledgePath(key, slug).String()
}

// docAddressSQL is DocAddress in SQL, for queries that alias knowledge as k
// and project as p. TestDocAddressSQLMatchesGo holds the two together.
const docAddressSQL = `'/' || CASE WHEN k.global = 1 THEN '` + vpath.GlobalKey +
	`' ELSE p.key END || '/knowledge/' || k.slug`
