//lint:file-ignore U1000 Selected by manager_{linux,darwin,other}.go; a single-GOOS check cannot see the other platforms use it.

package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const launchdLabel = "dev.trellis.daemon"

// renderLaunchdPlist builds the LaunchAgent. Every interpolated value goes
// through XML escaping: a storage root is user-controlled text, and an
// unescaped '&' makes the whole plist unparseable to launchd.
func renderLaunchdPlist(spec Spec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", xmlText(launchdLabel))
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range []string{spec.Exec, "daemon", "--bind", spec.Bind, "--port", strconv.Itoa(spec.Port)} {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlText(arg))
	}
	b.WriteString("\t</array>\n")
	if spec.Home != "" {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		fmt.Fprintf(&b, "\t\t<key>TRELLIS_HOME</key>\n\t\t<string>%s</string>\n", xmlText(spec.Home))
		b.WriteString("\t</dict>\n")
	}
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	b.WriteString("\t<key>ProcessType</key>\n\t<string>Background</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func xmlText(v string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}

// launchctlPID reads the PID out of `launchctl list <label>`, whose output is
// an old-style property list rather than anything machine-readable.
// A dash means the job is loaded but not currently running.
var launchctlPIDRe = regexp.MustCompile(`"PID"\s*=\s*(\d+)`)

func parseLaunchctlList(out string) (pid int, running bool) {
	m := launchctlPIDRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// execFromPlist recovers the first ProgramArguments entry so doctor can spot a
// unit still pointing at a binary that has since moved.
func execFromPlist(plist string) string {
	_, rest, found := strings.Cut(plist, "<key>ProgramArguments</key>")
	if !found {
		return ""
	}
	_, rest, found = strings.Cut(rest, "<string>")
	if !found {
		return ""
	}
	value, _, found := strings.Cut(rest, "</string>")
	if !found {
		return ""
	}
	return xmlUnescape(value)
}

func xmlUnescape(v string) string {
	return strings.NewReplacer(
		"&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&#x9;", "\t",
		"&#xA;", "\n", "&#xD;", "\r", "&amp;", "&").Replace(v)
}

type launchdManager struct{}

func (launchdManager) Name() string { return "launchd" }

func (launchdManager) UnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

// domain is the per-user launchd domain. Modern launchctl verbs address a job
// as <domain>/<label>; the legacy load/unload verbs are deprecated.
func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func target() string { return domain() + "/" + launchdLabel }

func (m launchdManager) Install(spec Spec) error {
	path, err := m.UnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Re-installing over a bootstrapped job fails, so tear the old one down
	// first. A missing job makes bootout fail harmlessly.
	_ = exec.Command("launchctl", "bootout", target()).Run()
	if err := os.WriteFile(path, []byte(renderLaunchdPlist(spec)), 0o600); err != nil {
		return err
	}
	return launchctl("bootstrap", domain(), path)
}

func (m launchdManager) Uninstall() error {
	path, err := m.UnitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	_ = exec.Command("launchctl", "bootout", target()).Run()
	return os.Remove(path)
}

func (m launchdManager) Start() error {
	path, err := m.installedPath()
	if err != nil {
		return err
	}
	// kickstart starts a bootstrapped job; a job that was booted out needs
	// bootstrapping again first, which is a no-op error when already loaded.
	_ = exec.Command("launchctl", "bootstrap", domain(), path).Run()
	return launchctl("kickstart", target())
}

func (m launchdManager) Stop() error {
	if _, err := m.installedPath(); err != nil {
		return err
	}
	return launchctl("bootout", target())
}

func (m launchdManager) Restart() error {
	path, err := m.installedPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootstrap", domain(), path).Run()
	return launchctl("kickstart", "-k", target())
}

func (m launchdManager) installedPath() (string, error) {
	path, err := m.UnitPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", ErrNotInstalled
	} else if err != nil {
		return "", err
	}
	return path, nil
}

func (m launchdManager) Status() (State, error) {
	path, err := m.UnitPath()
	if err != nil {
		return State{}, err
	}
	state := State{ManagedBy: m.Name(), UnitPath: path}
	plist, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Installed = true
	state.Exec = execFromPlist(string(plist))

	out, err := exec.Command("launchctl", "list", launchdLabel).Output()
	if err != nil {
		// Not loaded into the domain: installed but not enabled.
		return state, nil
	}
	state.Enabled = true
	state.PID, state.Running = parseLaunchctlList(string(out))
	return state, nil
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
