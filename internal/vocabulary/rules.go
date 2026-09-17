// Package vocabulary holds the words Trellis has retired and the test that
// keeps them retired. The project's glossary entry says what each concept is
// called; this package says what it must no longer be called.
package vocabulary

import "regexp"

// Rule is one retired word or word family.
type Rule struct {
	Name    string // the id allowlist.txt uses
	Pattern *regexp.Regexp
	Use     string // what to write instead
}

// Retired is every rule, in the order the glossary introduces them.
var Retired = []Rule{
	{"kb", regexp.MustCompile(`(?i)\bkb\b|kb(dir|root|core|doctype)|withkbroot`), "vault"},
	{"knowledge-base", regexp.MustCompile(`(?i)knowledge[ -]?base`), "vault"},
	{"knowledge-cli", regexp.MustCompile(`trellis knowledge\b`), "trellis vault"},
	{"knowledge-ident", regexp.MustCompile(`\b\w*Knowledge\w*|\bknowledge_\w+|"knowledge"|/knowledge\b`), "entry or vault"},
	{"doc", regexp.MustCompile(`\bdocs?(?:$|[^/\w])|\bdocs?[A-Z]\w*|\b[a-z]+Docs?\b|\bDoc[A-Z]\w*|\bdoc_\w+|'doc'|"doc"`), "entry"},
	{"document", regexp.MustCompile(`(?i)\bdocuments?\b`), "entry"},
	{"type-field", regexp.MustCompile(`(?i)doc[ _]?types?\b|yaml:"type\b`), "template"},
	{"escalate", regexp.MustCompile(`(?i)escalat`), "promote"},
	{"review", regexp.MustCompile(`(?i)unreviewed|review_?by|reviewed_?at|review clock`), "verify, unverified, verify_by, verified_at"},
	{"lease", regexp.MustCompile(`(?i)(?:^|[^ep])lease`), "claim"},
	{"owner", regexp.MustCompile(`(?i)\bowner(ship)?\b|\bholder\b|\bheld\b`), "claimant, claimed_by"},
	{"finding-umbrella", regexp.MustCompile(`LintFinding|\bfindings\b`), "diagnostic"},
	{"dangling", regexp.MustCompile(`(?i)\bdangling\b`), "stub"},
	{"dupe", regexp.MustCompile(`(?i)\bdupes?\b|dupecluster`), "duplicate"},
	{"orphan-file", regexp.MustCompile(`(?i)orphan[-_ ]?history|stale orphan|orphaned revision`), "leftover"},
	{"stale-other", regexp.MustCompile(`(?i)stale[_ ]leases?|stale[_ ]documents?`), "expired claims, unindexed entries"},
	{"ingestion-path", regexp.MustCompile(`(?i)ingestion paths?`), "provenance"},
	{"unarchive", regexp.MustCompile(`(?i)unarchiv`), "restore"},
	{"feed", regexp.MustCompile(`(?i)\bfeed\b|feedevent|eventfeed`), "event log"},
	{"noms", regexp.MustCompile(`\bnoms\b`), "nominations"},
	{"pin-file", regexp.MustCompile(`(?i)\.trellis pin|pin walk|\bpin(file|error|path|boundary)\b|\b(find|read|parse|parent|existing|write)pin\b|pin_(path|written|exists)|bad_pin|resolve\.pin\b|pin this directory|pinning any directory`), "marker"},
	{"vpath", regexp.MustCompile(`(?i)\bvpath\b|virtual[ -]paths?`), "address"},
	{"attach", regexp.MustCompile(`(?i)\battach(ment|ments|ed|es|ing)?\b|\bdetach`), "link, artifact"},
	{"new-card-id", regexp.MustCompile(`\bNewCardID\b`), "NewID"},
	{"corpus", regexp.MustCompile(`(?i)\bcorpus\b`), "vector index"},
	{"card-note", regexp.MustCompile(`card note\b|\b(Create|List|Delete)Notes?\b|"note"|'note'`), "comment"},
	{"verify-rule", regexp.MustCompile(`(?m)^verify:|yaml:"verify`), "resolve"},
	{"old-address", regexp.MustCompile(`/[A-Z][A-Z0-9]*/knowledge/`), "/KEY/vault/"},
}

// Find returns the names of the rules text breaks.
func Find(text string) []string {
	var names []string
	for _, r := range Retired {
		if r.Pattern.MatchString(text) {
			names = append(names, r.Name)
		}
	}
	return names
}

func ruleNamed(name string) Rule {
	for _, r := range Retired {
		if r.Name == name {
			return r
		}
	}
	return Rule{Name: name, Use: "(unknown rule)"}
}
