package vocabulary

import (
	"slices"
	"testing"
)

func TestRules(t *testing.T) {
	cases := []struct {
		rule   string
		hits   []string
		misses []string
	}{
		{"kb", []string{"kbDir", "WithKBRoot", "the kb", "KBDocType"}, []string{"kbps"}},
		{"knowledge-base", []string{"knowledge base", "Knowledge-base", "knowledgebase"}, nil},
		{"knowledge-cli", []string{"trellis knowledge show x"}, []string{"trellis vault show x"}},
		{"knowledge-ident", []string{"ListKnowledge", "knowledge_fts", `"knowledge"`, "/p/:key/knowledge", "KnowledgePage"},
			[]string{"writing-knowledge", "what knowledge to keep"}},
		{"doc", []string{"loadDoc(", "docView", "docsByID", "DocType", "doc_id", "'doc'", "a doc.", "the docs are"},
			[]string{"docs/superpowers/x.md", "godoc", "Docker"}},
		{"document", []string{"a document", "Documents"}, []string{"documentation"}},
		{"type-field", []string{"doc types", "doc_type", `yaml:"type,omitempty"`}, []string{`yaml:"template"`}},
		{"escalate", []string{"escalate", "EscalateKnowledge", "escalation queue"}, []string{"promote"}},
		{"review", []string{"unreviewed", "review_by", "reviewed_at", "ReviewBy", "review clock"}, []string{"Review column"}},
		{"lease", []string{"lease_until", "RenewLease", "a lease", "Lease held"}, []string{"release", "Released", "please"}},
		{"owner", []string{"owner", "ownership", "holder", "held by"}, []string{"household"}},
		{"finding-umbrella", []string{"LintFinding", "no findings"}, []string{"template finding"}},
		{"dangling", []string{"a dangling link"}, nil},
		{"dupe", []string{"--dupes", "DupeCluster", "dupe"}, []string{"duplicate"}},
		{"orphan-file", []string{"--orphan-history", "orphanHistory", "stale orphan", "orphaned revision"}, []string{"orphan entry"}},
		{"stale-other", []string{"stale_leases", "Stale leases", "stale_documents"}, []string{"stale recap"}},
		{"ingestion-path", []string{"ingestion path", "ingestion paths"}, []string{"provenance"}},
		{"unarchive", []string{"UnarchiveCard", "unarchived"}, []string{"archive"}},
		{"feed", []string{"event feed", "EventFeed", "FeedEvent"}, []string{"feedback"}},
		{"noms", []string{`json:"noms"`}, []string{"nominations"}},
		{"pin-file", []string{".trellis pin", "pin walk", "PinFile", "FindPin", "ReadPin", "pin_path", "bad_pin",
			"resolve.Pin", "Pin this directory", "without pinning any directory"},
			[]string{"pin an entry", "PinEntry", "vault pins"}},
		{"vpath", []string{"vpath.Parse", "virtual path", "virtual-paths"}, []string{"address"}},
		{"attach", []string{"attach", "Attachments", "attached", "Detach"}, []string{"artifact"}},
		{"new-card-id", []string{"NewCardID()"}, []string{"NewID()"}},
		{"corpus", []string{"the corpus"}, nil},
		{"card-note", []string{"card note X-1", "CreateNote", `"note"`, "'note'"}, []string{"a note on style", "notes/", "Note:"}},
		{"verify-rule", []string{"verify: [sources]", `yaml:"verify`}, []string{"vault verify"}},
		{"old-address", []string{"/KEY/knowledge/slug", "/GLOBAL/knowledge/x"}, []string{"/KEY/vault/slug"}},
	}
	for _, c := range cases {
		for _, s := range c.hits {
			if !slices.Contains(Find(s), c.rule) {
				t.Errorf("rule %s should match %q", c.rule, s)
			}
		}
		for _, s := range c.misses {
			if slices.Contains(Find(s), c.rule) {
				t.Errorf("rule %s should not match %q", c.rule, s)
			}
		}
	}
}
