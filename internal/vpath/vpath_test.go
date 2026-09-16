package vpath

import (
	"strings"
	"testing"
)

func TestParsePinAcceptsBothShapes(t *testing.T) {
	cases := map[string]Path{
		"/TRELLIS":              ProjectPath("TRELLIS"),
		"  /trellis\n":          ProjectPath("TRELLIS"),
		"/KIOSK-ANALYSE":        ProjectPath("KIOSK-ANALYSE"),
		"/MONO/boards/api":      BoardPath("MONO", "api"),
		"/MONO/boards/api-work": BoardPath("MONO", "api-work"),
		"/MONO/boards/重構":       BoardPath("MONO", "重構"),
		"/P2024/boards/2024-q1": BoardPath("P2024", "2024-q1"),
	}
	for in, want := range cases {
		got, err := ParsePin(in)
		if err != nil {
			t.Errorf("ParsePin(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePin(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParsePinRejects(t *testing.T) {
	cases := map[string]string{
		"":                       "empty",
		"   \n":                  "empty",
		"/A\n/B":                 "more than one line",
		"TRELLIS":                "old bare-key format",
		"trellis":                "old bare-key format",
		"/":                      "not a project key",
		"/1ABC":                  "not a project key",
		"/MY_APP":                "not a project key",
		"/A--B":                  "not a project key",
		"/GLOBAL":                "global knowledge vault",
		"/MONO/boards":           "not a pin",
		"/MONO/cards/MONO-1":     "not a pin",
		"/MONO/boards/api/extra": "not a pin",
		"/MONO/boards/API":       "not a board slug",
		"/MONO/boards/-api":      "not a board slug",
		"/MONO/boards/a--b":      "not a board slug",
		"MONO/boards/api":        "not a pin",
	}
	for in, fragment := range cases {
		_, err := ParsePin(in)
		if err == nil {
			t.Errorf("ParsePin(%q) succeeded, want an error mentioning %q", in, fragment)
			continue
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("ParsePin(%q) error = %q, want it to mention %q", in, err, fragment)
		}
	}
}

func TestPathStringAndBoard(t *testing.T) {
	for _, p := range []Path{ProjectPath("A"), BoardPath("A-B", "api")} {
		got, err := ParsePin(p.String())
		if err != nil || got != p {
			t.Errorf("ParsePin(%q) = %+v, %v; want %+v", p.String(), got, err, p)
		}
	}
	if got := BoardPath("MONO", "api").String(); got != "/MONO/boards/api" {
		t.Errorf("String() = %q", got)
	}
	if got := BoardPath("MONO", "api").Board(); got != "api" {
		t.Errorf("Board() = %q", got)
	}
	if got := ProjectPath("MONO").Board(); got != "" {
		t.Errorf("Board() of a project path = %q", got)
	}
}

func TestKeyFromName(t *testing.T) {
	cases := map[string]string{
		"trellis":         "TRELLIS",
		"kiosk-analyse":   "KIOSK-ANALYSE",
		"my app":          "MY-APP",
		"my__app..v2":     "MY-APP-V2",
		"2024-migrations": "P2024-MIGRATIONS",
		"-leading":        "LEADING",
		"trailing-":       "TRAILING",
		"...":             "",
		"重構":              "",
	}
	for in, want := range cases {
		got := KeyFromName(in)
		if got != want {
			t.Errorf("KeyFromName(%q) = %q, want %q", in, got, want)
		}
		if got != "" && !ValidKey(got) {
			t.Errorf("KeyFromName(%q) = %q, which ValidKey rejects", in, got)
		}
	}
}

func TestValidSlugMatchesBoardSlugs(t *testing.T) {
	for _, s := range []string{"api", "api-work", "board-2", "重構", "é"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "-api", "api-", "a--b", "API", "a b", "a_b", "É"} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}

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
