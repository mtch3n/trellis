package vpath

import "testing"

func TestParseEachCollection(t *testing.T) {
	cases := []struct {
		in   string
		want Path
	}{
		{"/TRELLIS/cards/TRELLIS-12", Path{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/trellis/cards/trellis-12", Path{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/TRELLIS/knowledge/concurrency-model", Path{"TRELLIS", CollectionKnowledge, "concurrency-model"}},
		{"/GLOBAL/knowledge/pain-point-analysis", Path{"GLOBAL", CollectionKnowledge, "pain-point-analysis"}},
		{"/global/knowledge/pain-point-analysis", Path{"GLOBAL", CollectionKnowledge, "pain-point-analysis"}},
		{"/TRELLIS/artifacts/photo.PNG", Path{"TRELLIS", CollectionArtifacts, "photo.PNG"}},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseRejectsMalformedAddresses(t *testing.T) {
	bad := []string{
		"/trellis/cards/not-a-ref",
		"/1BAD/cards/1BAD-1",
		"/TRELLIS/boards/main",
		"/GLOBAL/cards/GLOBAL-1",
		"/TRELLIS/artifacts/..",
		"/TRELLIS/knowledge/Not_A_Slug",
		"not/absolute/at/all",
		"/TRELLIS/knowledge/",
		"/TRELLIS/knowledge",
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want an error", s)
		}
	}
}
