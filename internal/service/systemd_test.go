package service

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitQuotesPathsWithSpaces(t *testing.T) {
	unit := renderSystemdUnit(Spec{
		Exec: "/home/a b/bin/trellis",
		Bind: "127.0.0.1",
		Port: 7788,
		Home: "/home/a b/.trellis",
	})
	want := `ExecStart="/home/a b/bin/trellis" daemon --bind "127.0.0.1" --port 7788`
	if !strings.Contains(unit, want) {
		t.Fatalf("unit missing quoted ExecStart\nwant line: %s\ngot:\n%s", want, unit)
	}
	if !strings.Contains(unit, `Environment=TRELLIS_HOME="/home/a b/.trellis"`) {
		t.Fatalf("unit missing quoted TRELLIS_HOME:\n%s", unit)
	}
	for _, section := range []string{"[Unit]", "[Service]", "[Install]", "WantedBy=default.target"} {
		if !strings.Contains(unit, section) {
			t.Errorf("unit missing %s", section)
		}
	}
}

func TestRenderSystemdUnitOmitsHomeWhenUnset(t *testing.T) {
	unit := renderSystemdUnit(Spec{Exec: "/usr/bin/trellis", Bind: "127.0.0.1", Port: 7788})
	if strings.Contains(unit, "TRELLIS_HOME") {
		t.Fatalf("unset Home must not appear in the unit:\n%s", unit)
	}
}

func TestExecFromUnitRoundTrips(t *testing.T) {
	for _, exec := range []string{
		"/usr/bin/trellis",
		"/home/a b/bin/trellis",
		`/home/od"d/trellis`,
		`/home/back\slash/trellis`,
	} {
		unit := renderSystemdUnit(Spec{Exec: exec, Bind: "127.0.0.1", Port: 7788})
		if got := execFromUnit(unit); got != exec {
			t.Errorf("execFromUnit round trip: want %q, got %q", exec, got)
		}
	}
}

func TestExecFromUnitMissingExecStart(t *testing.T) {
	if got := execFromUnit("[Unit]\nDescription=x\n"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestParseSystemctlShow(t *testing.T) {
	props := parseSystemctlShow("ActiveState=active\nUnitFileState=enabled\nMainPID=4821\n")
	for key, want := range map[string]string{"ActiveState": "active", "UnitFileState": "enabled", "MainPID": "4821"} {
		if props[key] != want {
			t.Errorf("%s: want %q, got %q", key, want, props[key])
		}
	}
}

func TestParseSystemctlShowKeepsEqualsInValue(t *testing.T) {
	props := parseSystemctlShow("ExecStart={ path=/usr/bin/trellis ; argv[]=/usr/bin/trellis daemon }\n\nMainPID=0\n")
	if want := "{ path=/usr/bin/trellis ; argv[]=/usr/bin/trellis daemon }"; props["ExecStart"] != want {
		t.Errorf("want %q, got %q", want, props["ExecStart"])
	}
	if props["MainPID"] != "0" {
		t.Errorf("blank lines must not break parsing, got %q", props["MainPID"])
	}
}
