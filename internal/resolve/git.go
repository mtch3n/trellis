package resolve

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RepoInfo describes the git repository containing a directory.
type RepoInfo struct {
	CommonDir string // the MAIN repository's .git, so worktrees agree
	TopLevel  string
	Origin    string // may be empty: many repositories have no remote
}

// gitLocationVars redirect where git believes the repository is. Inheriting
// them makes `rev-parse` answer about a DIFFERENT repository while exiting 0,
// so a directory that is not a repository at all reports someone else's paths
// and Repo() returns a confident wrong answer. Scrub them.
var gitLocationVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM",
}

// scrubbedEnv returns the parent environment without the variables that move
// git's idea of the repository.
func scrubbedEnv() []string {
	drop := make(map[string]bool, len(gitLocationVars))
	for _, k := range gitLocationVars {
		drop[k] = true
	}
	env := os.Environ()
	kept := env[:0]
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); !ok || !drop[k] {
			kept = append(kept, kv)
		}
	}
	return kept
}

func git(dir string, args ...string) (string, bool) {
	// Bounded: a directory on a stalled network mount would otherwise hang the
	// caller forever, and this runs on every board resolution.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = scrubbedEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// Repo interrogates the git repository containing dir. --path-format=absolute
// is mandatory: without it --git-common-dir returns a relative path from inside
// the main repository and an absolute one from a worktree.
func Repo(dir string) (RepoInfo, bool) {
	out, ok := git(dir, "rev-parse", "--path-format=absolute",
		"--git-common-dir", "--show-toplevel")
	if !ok {
		return RepoInfo{}, false
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return RepoInfo{}, false // bare repository: no --show-toplevel
	}

	info := RepoInfo{
		CommonDir: strings.TrimSpace(lines[0]),
		TopLevel:  strings.TrimSpace(lines[1]),
	}
	// A missing origin is normal, not an error.
	if origin, ok := git(dir, "remote", "get-url", "origin"); ok {
		info.Origin = origin
	}
	return info, true
}
