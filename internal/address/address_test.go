package address

import (
	"strings"
	"testing"
)

func TestParseMarkerAcceptsBothShapes(t *testing.T) {
	cases := map[string]Address{
		"/TRELLIS":              Project("TRELLIS"),
		"  /trellis\n":          Project("TRELLIS"),
		"/KIOSK-ANALYSE":        Project("KIOSK-ANALYSE"),
		"/MONO/boards/api":      Board("MONO", "api"),
		"/MONO/boards/api-work": Board("MONO", "api-work"),
		"/MONO/boards/重構":       Board("MONO", "重構"),
		"/P2024/boards/2024-q1": Board("P2024", "2024-q1"),
	}
	for in, want := range cases {
		got, err := ParseMarker(in)
		if err != nil {
			t.Errorf("ParseMarker(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMarker(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseMarkerRejects(t *testing.T) {
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
		"/GLOBAL":                "global vault",
		"/MONO/boards":           "not a marker",
		"/MONO/cards/MONO-1":     "not a marker",
		"/MONO/boards/api/extra": "not a marker",
		"/MONO/boards/API":       "not a board slug",
		"/MONO/boards/-api":      "not a board slug",
		"/MONO/boards/a--b":      "not a board slug",
		"MONO/boards/api":        "not a marker",
	}
	for in, fragment := range cases {
		_, err := ParseMarker(in)
		if err == nil {
			t.Errorf("ParseMarker(%q) succeeded, want an error mentioning %q", in, fragment)
			continue
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("ParseMarker(%q) error = %q, want it to mention %q", in, err, fragment)
		}
	}
}

func TestPathStringAndBoard(t *testing.T) {
	for _, p := range []Address{Project("A"), Board("A-B", "api")} {
		got, err := ParseMarker(p.String())
		if err != nil || got != p {
			t.Errorf("ParseMarker(%q) = %+v, %v; want %+v", p.String(), got, err, p)
		}
	}
	if got := Board("MONO", "api").String(); got != "/MONO/boards/api" {
		t.Errorf("String() = %q", got)
	}
	if got := Board("MONO", "api").Board(); got != "api" {
		t.Errorf("Board() = %q", got)
	}
	if got := Project("MONO").Board(); got != "" {
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
		want Address
	}{
		{"/TRELLIS/cards/TRELLIS-12", Address{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/trellis/cards/trellis-12", Address{"TRELLIS", CollectionCards, "TRELLIS-12"}},
		{"/TRELLIS/vault/concurrency-model", Address{"TRELLIS", CollectionVault, "concurrency-model"}},
		{"/GLOBAL/vault/pain-point-analysis", Address{"GLOBAL", CollectionVault, "pain-point-analysis"}},
		{"/TRELLIS/vault/ops/deploy/rollback", Address{"TRELLIS", CollectionVault, "ops/deploy/rollback"}},
		{"/global/vault/pain-point-analysis", Address{"GLOBAL", CollectionVault, "pain-point-analysis"}},
		{"/TRELLIS/artifacts/photo.PNG", Address{"TRELLIS", CollectionArtifacts, "photo.PNG"}},
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
		"/GLOBAL/cards/GLOBAL-1",
		"/TRELLIS/artifacts/..",
		"/TRELLIS/vault/Not_A_Slug",
		"not/absolute/at/all",
		"/TRELLIS/vault/",
		"/TRELLIS/vault",
		"/TRELLIS/vault/ops/",
		"/TRELLIS/vault/ops//rollback",
		"/TRELLIS/cards/TRELLIS-1/extra",
		"/TRELLIS/artifacts/dir/photo.png",
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want an error", s)
		}
	}
}
