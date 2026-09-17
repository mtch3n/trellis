package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseWikilinksFormsAndSkips(t *testing.T) {
	body := "See [[design]] and [[/XPSCTL/vault/concurrency-model#parallel safety]].\n" +
		"Alias [[design|the design]] is the same target.\n" +
		"```\nnot a [[link]] in code\n```\n" +
		"Nor `[[inline]]`.\n" +
		"A card is no entry: [[/XPSCTL/cards/XPSCTL-1]]. A relative path: [[xpsctl/design]].\n"
	got := ParseWikilinks(body)
	want := []Reference{
		{Raw: "design", Slug: "design"},
		{Raw: "/XPSCTL/vault/concurrency-model#parallel safety", ProjectKey: "XPSCTL",
			Slug: "concurrency-model", Anchor: "parallel-safety"},
		{Raw: "/XPSCTL/cards/XPSCTL-1", Slug: "/XPSCTL/cards/XPSCTL-1"},
		{Raw: "xpsctl/design", Slug: "xpsctl/design"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseWikilinks =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseReference(t *testing.T) {
	cases := map[string]Reference{
		"design#Why Not":              {Raw: "design#Why Not", Slug: "design", Anchor: "why-not"},
		"a/b":                         {Raw: "a/b", Slug: "a/b"},
		"/other/vault/runbook":        {Raw: "/other/vault/runbook", ProjectKey: "OTHER", Slug: "runbook"},
		"/GLOBAL/vault/conventions#x": {Raw: "/GLOBAL/vault/conventions#x", ProjectKey: "GLOBAL", Slug: "conventions", Anchor: "x"},
		"/bad_key/vault/x":            {Raw: "/bad_key/vault/x", Slug: "/bad_key/vault/x"},
	}
	for in, want := range cases {
		if got := ParseReference(in); got != want {
			t.Errorf("ParseReference(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseInlineTagsIgnoresHeadings(t *testing.T) {
	body := "## Heading\nA #finding about #sqlite/wal.\n```\n#not-a-tag\n```\n"
	got := ParseInlineTags(body)
	want := []string{"finding", "sqlite/wal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseInlineTags = %v, want %v", got, want)
	}
}

func TestFrontmatterRoundTrip(t *testing.T) {
	fm := Frontmatter{Title: "Concurrency model", Template: "decision", Tags: []string{"sqlite"}}
	raw := RenderEntry(fm, "# Concurrency model\n\nLeases, not locks.\n")
	back, body, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != fm.Title || back.Template != fm.Template || len(back.Tags) != 1 {
		t.Errorf("round trip lost fields: %+v", back)
	}
	if FirstParagraph(body) != "Leases, not locks." {
		t.Errorf("FirstParagraph = %q", FirstParagraph(body))
	}
}

func TestSplitFrontmatterToleratesNone(t *testing.T) {
	fm, body, err := SplitFrontmatter("# Just a note\n")
	if err != nil || fm.Title != "" || body != "# Just a note\n" {
		t.Errorf("SplitFrontmatter(no header) = %+v, %q, %v", fm, body, err)
	}
}

func TestHeadingAnchors(t *testing.T) {
	got := HeadingAnchors("# Title\n\n## Parallel safety\n\n### Why it matters\n")
	want := []string{"title", "parallel-safety", "why-it-matters"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HeadingAnchors = %v, want %v", got, want)
	}
}

func TestFrontmatterExtraKeysRoundTrip(t *testing.T) {
	raw := "---\ntitle: Rollback the API\ntemplate: runbook\nowner: alice\nseverity: high\n---\n# Rollback the API\n"
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Extra["owner"] != "alice" || fm.Extra["severity"] != "high" {
		t.Fatalf("Extra = %+v, want owner and severity kept", fm.Extra)
	}
	out := RenderEntry(fm, body)
	back, _, err := SplitFrontmatter(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Extra["owner"] != "alice" || back.Extra["severity"] != "high" {
		t.Errorf("round trip lost an unknown key: Extra = %+v", back.Extra)
	}
	if back.Title != "Rollback the API" || back.Template != "runbook" {
		t.Errorf("a named field was lost: %+v", back)
	}
}

func TestFrontmatterExtraKeysRenderInStableOrder(t *testing.T) {
	a := Frontmatter{Title: "X", Extra: map[string]any{"zebra": "z", "apple": "a", "mango": "m"}}
	b := Frontmatter{Title: "X", Extra: map[string]any{"mango": "m", "apple": "a", "zebra": "z"}}
	rendered := RenderEntry(a, "body\n")
	for range 20 {
		if got := RenderEntry(b, "body\n"); got != rendered {
			t.Fatalf("rendering is not deterministic:\n%q\n%q", rendered, got)
		}
	}
	if !strings.Contains(rendered, "apple: a\nmango: m\nzebra: z\n") {
		t.Errorf("Extra keys did not render in sorted order:\n%s", rendered)
	}
}

func TestFrontmatterWithNoExtraKeysIsUnchanged(t *testing.T) {
	fm := Frontmatter{Title: "Plain", Template: "decision"}
	got := RenderEntry(fm, "body\n")
	want := "---\ntitle: Plain\ntemplate: decision\n---\n\nbody\n"
	if got != want {
		t.Errorf("RenderDoc with no Extra = %q, want %q", got, want)
	}
}

func TestFrontmatterSourcesRoundTrip(t *testing.T) {
	fm := Frontmatter{Title: "X", Sources: []string{"https://example.com", "[[design-doc]]"}}
	raw := RenderEntry(fm, "body\n")
	back, _, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Sources) != 2 || back.Sources[0] != "https://example.com" || back.Sources[1] != "[[design-doc]]" {
		t.Errorf("Sources = %v", back.Sources)
	}
}

func TestRewriteWikilinks(t *testing.T) {
	text := "See [[/API/vault/runbook#Roll back|the runbook]] and [[design]].\n" +
		"Inline `[[/API/vault/runbook]]` stays.\n" +
		"```\n[[/API/vault/runbook]]\n```\n" +
		"Also [[/api/vault/runbook]] and [[/OTHER/vault/runbook]].\n"
	got := RewriteWikilinks(text, func(ref Reference) (string, bool) {
		if ref.ProjectKey != "API" || ref.Slug != "runbook" {
			return "", false
		}
		target := "/MONO/vault/runbook-api"
		if i := strings.Index(ref.Raw, "#"); i >= 0 {
			target += ref.Raw[i:]
		}
		return target, true
	})
	want := "See [[/MONO/vault/runbook-api#Roll back|the runbook]] and [[design]].\n" +
		"Inline `[[/API/vault/runbook]]` stays.\n" +
		"```\n[[/API/vault/runbook]]\n```\n" +
		"Also [[/MONO/vault/runbook-api]] and [[/OTHER/vault/runbook]].\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if same := RewriteWikilinks(text, func(Reference) (string, bool) { return "", false }); same != text {
		t.Error("a rewrite that changes nothing altered the text")
	}
}
