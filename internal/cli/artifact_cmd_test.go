package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// runCmdErr runs a command and returns its error instead of failing the test.
func runCmdErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func cliErrCode(err error) string {
	if e, ok := errors.AsType[*core.Error](err); ok {
		return e.Code
	}
	return ""
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestArtifactAddAttachesToAnEntry(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Standup")

	runCmd(t, "artifact", "add", writeFile(t, "standup.mp3", "ID3 audio"), "--entry", "standup")

	out := runCmd(t, "vault", "show", "standup", "--json")
	if !strings.Contains(out, `"standup.mp3"`) {
		t.Errorf("entry does not list the artifact:\n%s", out)
	}
}

func TestArtifactLinkAndUnlinkByName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "vault", "new", "--title", "Research")
	runCmd(t, "artifact", "add", writeFile(t, "paper.pdf", "%PDF-1.7\n"))

	runCmd(t, "artifact", "link", "paper.pdf", "--entry", "research")
	listed := runCmd(t, "artifact", "ls", "--entry", "research", "--json")
	if !strings.Contains(listed, "paper.pdf") {
		t.Fatalf("ls --entry does not list the linked artifact:\n%s", listed)
	}

	runCmd(t, "artifact", "unlink", "paper.pdf", "--entry", "research")
	after := runCmd(t, "artifact", "ls", "--entry", "research", "--json")
	if strings.Contains(after, "paper.pdf") {
		t.Errorf("artifact still listed after unlink:\n%s", after)
	}
}

func TestArtifactLinkNeedsExactlyOneTarget(t *testing.T) {
	projectEnv(t)
	runCmd(t, "artifact", "add", writeFile(t, "x.png", "\x89PNG\r\n\x1a\nx"))

	if _, err := runCmdErr(t, "artifact", "link", "x.png"); cliErrCode(err) != "missing_target" {
		t.Errorf("no target: err = %v, want missing_target", err)
	}
	if _, err := runCmdErr(t, "artifact", "link", "x.png", "--card", "TEST-1", "--entry", "x"); cliErrCode(err) != "target_conflict" {
		t.Errorf("both targets: err = %v, want target_conflict", err)
	}
	if _, err := runCmdErr(t, "artifact", "unlink", "x.png"); cliErrCode(err) != "missing_target" {
		t.Errorf("unlink with no target: err = %v, want missing_target", err)
	}
}

func TestArtifactRmAcceptsAName(t *testing.T) {
	projectEnv(t)
	runCmd(t, "artifact", "add", writeFile(t, "old.png", "\x89PNG\r\n\x1a\nx"))

	runCmd(t, "artifact", "rm", "old.png")

	if strings.Contains(runCmd(t, "artifact", "ls", "--json"), "old.png") {
		t.Error("artifact still listed after rm by name")
	}
}

func TestArtifactAddCleansUpOnBadEntryLink(t *testing.T) {
	projectEnv(t)

	// Try to add artifact with bad entry reference
	_, err := runCmdErr(t, "artifact", "add", writeFile(t, "test.txt", "content"), "--entry", "no-such-entry")
	if err == nil {
		t.Fatal("expected error when linking to non-existent entry")
	}

	// Verify no artifacts are listed
	listed := runCmd(t, "artifact", "ls", "--json")
	if strings.Contains(listed, "test.txt") {
		t.Error("artifact should not be listed after failed link")
	}
}

func TestArtifactAddCleansUpOnBadCardLink(t *testing.T) {
	projectEnv(t)

	// Try to add artifact with bad card reference
	_, err := runCmdErr(t, "artifact", "add", writeFile(t, "test.txt", "content"), "--card", "TEST-9999")
	if err == nil {
		t.Fatal("expected error when linking to non-existent card")
	}

	// Verify no artifacts are listed
	listed := runCmd(t, "artifact", "ls", "--json")
	if strings.Contains(listed, "test.txt") {
		t.Error("artifact should not be listed after failed link")
	}
}
