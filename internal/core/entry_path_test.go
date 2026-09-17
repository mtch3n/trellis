package core

import (
	"errors"
	"testing"
)

func TestSlugifyPathAcceptsAPlainDirectory(t *testing.T) {
	got, err := SlugifyPath("Deployment")
	if err != nil || got != "deployment" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSlugifyPathSlugifiesEachSegment(t *testing.T) {
	got, err := SlugifyPath("Deployment/AWS Runbooks")
	if err != nil || got != "deployment/aws-runbooks" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSlugifyPathRejectsAnAbsoluteInput(t *testing.T) {
	if _, err := SlugifyPath("/etc/passwd"); pathErrCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsBackslash(t *testing.T) {
	if _, err := SlugifyPath(`deployment\aws`); pathErrCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsTraversal(t *testing.T) {
	for _, in := range []string{"..", "../etc", "deployment/..", "deployment/../etc"} {
		if _, err := SlugifyPath(in); pathErrCode(err) != "bad_path" {
			t.Errorf("SlugifyPath(%q) err = %v, want bad_path", in, err)
		}
	}
}

func TestSlugifyPathRejectsAnEmptySegment(t *testing.T) {
	if _, err := SlugifyPath("deployment//rollback"); pathErrCode(err) != "bad_path" {
		t.Fatalf("err = %v, want bad_path", err)
	}
}

func TestSlugifyPathRejectsEveryReservedDeviceName(t *testing.T) {
	names := []string{"con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"}
	for _, n := range names {
		if _, err := SlugifyPath(n); pathErrCode(err) != "reserved_name" {
			t.Errorf("SlugifyPath(%q) err = %v, want reserved_name", n, err)
		}
		if _, err := SlugifyPath("docs/" + n); pathErrCode(err) != "reserved_name" {
			t.Errorf("SlugifyPath(%q) err = %v, want reserved_name", "docs/"+n, err)
		}
	}
}

func TestSlugifyPathRejectsAnOverLongSegment(t *testing.T) {
	seg := ""
	for len(seg) < 97 {
		seg += "a"
	}
	if _, err := SlugifyPath(seg); pathErrCode(err) != "path_too_long" {
		t.Fatalf("a 97-character segment: err = %v, want path_too_long", err)
	}
	seg96 := seg[:96]
	if got, err := SlugifyPath(seg96); err != nil || got != seg96 {
		t.Fatalf("a 96-character segment must be accepted: got %q, %v", got, err)
	}
}

func TestSlugifyPathRejectsAnOverLongFullPath(t *testing.T) {
	seg := ""
	for len(seg) < 90 {
		seg += "a"
	}
	// Two 90-character segments plus one "/" is 181 characters.
	if _, err := SlugifyPath(seg + "/" + seg); pathErrCode(err) != "path_too_long" {
		t.Fatalf("a 181-character path: err = %v, want path_too_long", err)
	}
}

func TestSlugifyPathEmptyInputIsEmptyOutput(t *testing.T) {
	got, err := SlugifyPath("")
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTruncateSegmentLeavesAShortSegmentAlone(t *testing.T) {
	if got := truncateSegment("concurrency-model"); got != "concurrency-model" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateSegmentCutsAtTheCeiling(t *testing.T) {
	long := ""
	for len(long) < 120 {
		long += "a"
	}
	got := truncateSegment(long)
	if len(got) > maxPathSegmentLen {
		t.Fatalf("len(%q) = %d, want <= %d", got, len(got), maxPathSegmentLen)
	}
}

// The longest slug in the live vault today is 65 characters; the ceiling
// must not touch it.
func TestTruncateSegmentRoundTripsTheLongestKnownSlug(t *testing.T) {
	slug := "a-sixty-five-character-slug-that-already-exists-in-the-live-vault"
	if len(slug) != 65 {
		t.Fatalf("test fixture is %d characters, want 65", len(slug))
	}
	if got := truncateSegment(slug); got != slug {
		t.Fatalf("got %q, want it unchanged", got)
	}
}

func TestNormalizeSlugPathNeverErrors(t *testing.T) {
	cases := map[string]string{
		"Rollback":             "rollback",
		"deployment/rollback":  "deployment/rollback",
		"":                     "",
		"../etc":               "etc",
		"deployment//rollback": "deployment/rollback",
		"  Spaced Title  ":     "spaced-title",
	}
	for in, want := range cases {
		if got := normalizeSlugPath(in); got != want {
			t.Errorf("normalizeSlugPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResemblesPrefixRule(t *testing.T) {
	if !resembles("deploy", "deployment") {
		t.Error("deploy should resemble deployment")
	}
	if !resembles("runbook", "runbooks") {
		t.Error("runbook should resemble runbooks")
	}
}

func TestResemblesEditDistanceRule(t *testing.T) {
	if !resembles("deployment", "deplyoment") {
		t.Error("deployment should resemble deplyoment (transposition, edit distance 2)")
	}
}

func TestResemblesFloorsKeepObviousNamesApart(t *testing.T) {
	if resembles("api", "apis-legacy") {
		t.Error("api must not resemble apis-legacy: the shorter name is under the 4-character floor")
	}
	if resembles("docs", "dogs") {
		t.Error("docs must not resemble dogs: both are under the 5-character edit-distance floor")
	}
}

func TestResemblesIsFalseForIdenticalNames(t *testing.T) {
	if resembles("deployment", "deployment") {
		t.Error("a name never resembles itself; that is an exact match, not a diagnostic")
	}
}

func TestReservedLeafTakenChecksOnlyTheLastSegment(t *testing.T) {
	if !reservedLeafTaken("con") {
		t.Error("con")
	}
	if !reservedLeafTaken("deployment/con") {
		t.Error("deployment/con")
	}
	if reservedLeafTaken("con-2") {
		t.Error("con-2 is not itself reserved")
	}
	if reservedLeafTaken("consulting") {
		t.Error("consulting must not match on a prefix")
	}
}

func pathErrCode(err error) string {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}
