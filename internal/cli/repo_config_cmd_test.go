package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoEnv is a pinned project whose pin directory holds a .trellis.yaml with
// content. It returns the pin directory, which is also the working directory.
func repoEnv(t *testing.T, content string) string {
	t.Helper()
	dir := pinEnv(t, "repo")
	seedProject(t, "REPO", "main")
	writePin(t, dir, "/REPO\n")
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, ".trellis.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestConfigGetReadsTheFileBesideThePin(t *testing.T) {
	dir := repoEnv(t, "config:\n  card.ls_limit: 7\n")

	out := runCmd(t, "config", "get", "card.ls_limit", "--json")
	if !strings.Contains(out, `"value":"7"`) || !strings.Contains(out, `"source":"repo"`) {
		t.Fatalf("config get = %s, want value 7 from repo", out)
	}

	// From a subdirectory, the file beside the pin still answers.
	sub := filepath.Join(dir, "deep", "inside")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	out = runCmd(t, "config", "get", "card.ls_limit", "--json")
	if !strings.Contains(out, `"source":"repo"`) {
		t.Fatalf("from a subdirectory, config get = %s, want source repo", out)
	}
}

func TestANamedProjectReadsNoRepositoryFile(t *testing.T) {
	repoEnv(t, "config:\n  card.ls_limit: 7\n")
	t.Setenv("TRELLIS_PROJECT", "REPO")

	out := runCmd(t, "config", "get", "card.ls_limit", "--json")
	if strings.Contains(out, `"source":"repo"`) {
		t.Fatalf("config get = %s: a project named by TRELLIS_PROJECT has no pin, so no repo file", out)
	}
}

func TestABrokenRepositoryFileFailsOrdinaryCommands(t *testing.T) {
	repoEnv(t, "config:\n  ui.port: 1\n")

	_, err := runCmdErr(t, "card", "ls")
	if err == nil {
		t.Fatal("card ls ran with a .trellis.yaml that sets a machine-level key")
	}
	if ce := coreErr(t, err); ce.Code != "bad_repo_config" || !strings.Contains(ce.Msg, "ui.port") {
		t.Fatalf("error = %+v, want bad_repo_config naming ui.port", ce)
	}
}

func TestConfigSetRepoWritesBesideThePin(t *testing.T) {
	dir := repoEnv(t, "config:\n  lease.ttl: 45m\nextensions:\n  actions:\n    - on: knowledge.created\n      run: ./review.sh\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	runCmd(t, "config", "set", "--repo", "card.ls_limit", "9")

	if _, err := os.Stat(filepath.Join(sub, ".trellis.yaml")); !os.IsNotExist(err) {
		t.Fatalf("--repo wrote a file in the working directory, where nothing reads it: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".trellis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"card.ls_limit: 9", "lease.ttl: 45m", "run: ./review.sh"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf(".trellis.yaml lost or lacks %q:\n%s", want, raw)
		}
	}

	runCmd(t, "config", "unset", "--repo", "card.ls_limit")
	raw, err = os.ReadFile(filepath.Join(dir, ".trellis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "card.ls_limit") || !strings.Contains(string(raw), "lease.ttl: 45m") {
		t.Errorf("unset --repo removed the wrong thing:\n%s", raw)
	}
}

// review-cli #6: config set --repo must refuse a value its own loader would
// reject, before writing it -- otherwise it breaks every command in the
// repository, including this one, the next time it reads the file.
func TestConfigSetRepoRejectsABadValue(t *testing.T) {
	repoEnv(t, "")

	_, err := runCmdErr(t, "config", "set", "--repo", "card.ls_limit", "abc")
	if err == nil {
		t.Fatal("config set --repo card.ls_limit abc was accepted")
	}
	if ce := coreErr(t, err); ce.Code != "invalid_value" {
		t.Fatalf("code = %s, want invalid_value", ce.Code)
	}

	// The command that follows must still work: nothing was written.
	runCmd(t, "card", "ls")
}

func TestConfigSetRepoRefusesAMachineLevelKey(t *testing.T) {
	repoEnv(t, "")
	for _, key := range []string{"ui.port", "search.vector.provider", "search.vector.embed_command"} {
		_, err := runCmdErr(t, "config", "set", "--repo", key, "1")
		if err == nil {
			t.Errorf("config set --repo %s was accepted", key)
			continue
		}
		if ce := coreErr(t, err); ce.Code != "not_repo_safe" {
			t.Errorf("%s: code = %s, want not_repo_safe", key, ce.Code)
		}
	}
}

func TestConfigSetRepoNeedsAPin(t *testing.T) {
	repoEnv(t, "")
	t.Setenv("TRELLIS_PROJECT", "REPO")
	_, err := runCmdErr(t, "config", "set", "--repo", "card.ls_limit", "9")
	if err == nil {
		t.Fatal("--repo with TRELLIS_PROJECT has no pin to write beside")
	}
	if ce := coreErr(t, err); ce.Code != "no_pin" {
		t.Fatalf("code = %s, want no_pin", ce.Code)
	}
}

func TestExtensionConfigPrintsItsSubtree(t *testing.T) {
	dir := repoEnv(t, "extensions:\n  actions:\n    - on: knowledge.created\n      type: finding\n      run: ./review.sh\n  other:\n    x: 1\n")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	out := runCmd(t, "extension", "config", "actions", "--json")
	if !strings.Contains(out, `"run":"./review.sh"`) || !strings.Contains(out, `"on":"knowledge.created"`) {
		t.Fatalf("extension config actions = %s", out)
	}
	if strings.Contains(out, `"x"`) {
		t.Fatalf("extension config actions leaked another extension's section: %s", out)
	}
}
