package service

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestRenderLaunchdPlistIsWellFormedXML(t *testing.T) {
	plist := renderLaunchdPlist(Spec{
		Exec: "/Applications/Trellis & Co/trellis",
		Bind: "127.0.0.1",
		Port: 7788,
		Home: "/Users/a b/.trellis",
	})
	if err := xml.Unmarshal([]byte(plist), new(any)); err != nil {
		t.Fatalf("plist is not well-formed XML: %v\n%s", err, plist)
	}
	if strings.Contains(plist, "Trellis & Co") {
		t.Errorf("ampersand must be escaped:\n%s", plist)
	}
	for _, want := range []string{"<string>daemon</string>", "<string>7788</string>", "<key>RunAtLoad</key>", "dev.trellis.daemon"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %s", want)
		}
	}
}

func TestRenderLaunchdPlistOmitsHomeWhenUnset(t *testing.T) {
	plist := renderLaunchdPlist(Spec{Exec: "/usr/local/bin/trellis", Bind: "127.0.0.1", Port: 7788})
	if strings.Contains(plist, "TRELLIS_HOME") {
		t.Fatalf("unset Home must not appear in the plist:\n%s", plist)
	}
}

func TestExecFromPlistRoundTrips(t *testing.T) {
	for _, exe := range []string{
		"/usr/local/bin/trellis",
		"/Users/a b/bin/trellis",
		"/Users/x&y/trellis",
		`/Users/<odd>/trellis`,
	} {
		plist := renderLaunchdPlist(Spec{Exec: exe, Bind: "127.0.0.1", Port: 7788})
		if got := execFromPlist(plist); got != exe {
			t.Errorf("execFromPlist round trip: want %q, got %q", exe, got)
		}
	}
}

func TestParseLaunchctlList(t *testing.T) {
	running := `{
	"LimitLoadToSessionType" = "Aqua";
	"Label" = "dev.trellis.daemon";
	"OnDemand" = false;
	"LastExitStatus" = 0;
	"PID" = 5123;
};`
	pid, ok := parseLaunchctlList(running)
	if !ok || pid != 5123 {
		t.Errorf("want pid 5123 running, got %d %v", pid, ok)
	}

	// A loaded but stopped job reports no PID key at all.
	stopped := `{
	"Label" = "dev.trellis.daemon";
	"LastExitStatus" = 0;
};`
	if pid, ok := parseLaunchctlList(stopped); ok || pid != 0 {
		t.Errorf("stopped job must report not running, got %d %v", pid, ok)
	}
}
