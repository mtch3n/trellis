package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseWikilinksFormsAndSkips(t *testing.T) {
	body := "See [[design]] and [[XPSCTL/concurrency-model#parallel safety]].\n" +
		"Alias [[design|the design]] is the same target.\n" +
		"```\nnot a [[link]] in code\n```\n" +
		"Nor `[[inline]]`.\n"
	got := ParseWikilinks(body)
	want := []Reference{
		{Raw: "design", Slug: "design"},
		{Raw: "XPSCTL/concurrency-model#parallel safety", ProjectKey: "XPSCTL",
			Slug: "concurrency-model", Anchor: "parallel-safety"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseWikilinks =\n%+v\nwant\n%+v", got, want)
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
	fm := Frontmatter{Title: "Concurrency model", Type: "decision", Tags: []string{"sqlite"}}
	raw := RenderDoc(fm, "# Concurrency model\n\nLeases, not locks.\n")
	back, body, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != fm.Title || back.Type != fm.Type || len(back.Tags) != 1 {
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
	raw := "---\ntitle: Rollback the API\ntype: runbook\nowner: alice\nseverity: high\n---\n# Rollback the API\n"
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Extra["owner"] != "alice" || fm.Extra["severity"] != "high" {
		t.Fatalf("Extra = %+v, want owner and severity kept", fm.Extra)
	}
	out := RenderDoc(fm, body)
	back, _, err := SplitFrontmatter(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Extra["owner"] != "alice" || back.Extra["severity"] != "high" {
		t.Errorf("round trip lost an unknown key: Extra = %+v", back.Extra)
	}
	if back.Title != "Rollback the API" || back.Type != "runbook" {
		t.Errorf("a named field was lost: %+v", back)
	}
}

func TestFrontmatterExtraKeysRenderInStableOrder(t *testing.T) {
	a := Frontmatter{Title: "X", Extra: map[string]any{"zebra": "z", "apple": "a", "mango": "m"}}
	b := Frontmatter{Title: "X", Extra: map[string]any{"mango": "m", "apple": "a", "zebra": "z"}}
	rendered := RenderDoc(a, "body\n")
	for range 20 {
		if got := RenderDoc(b, "body\n"); got != rendered {
			t.Fatalf("rendering is not deterministic:\n%q\n%q", rendered, got)
		}
	}
	if !strings.Contains(rendered, "apple: a\nmango: m\nzebra: z\n") {
		t.Errorf("Extra keys did not render in sorted order:\n%s", rendered)
	}
}

func TestFrontmatterWithNoExtraKeysIsUnchanged(t *testing.T) {
	fm := Frontmatter{Title: "Plain", Type: "note"}
	got := RenderDoc(fm, "body\n")
	want := "---\ntitle: Plain\ntype: note\n---\n\nbody\n"
	if got != want {
		t.Errorf("RenderDoc with no Extra = %q, want %q", got, want)
	}
}
