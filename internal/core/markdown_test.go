package core

import (
	"reflect"
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
