// Package doctor holds the checks that describe this Trellis installation:
// the binary, the storage root, the database, the configuration and the
// search backend. They belong to the installation rather than to a caller, so
// the CLI's `trellis doctor` and the web UI's diagnostics run the same ones.
//
// Whatever depends on the caller stays with it: the CLI adds the daemon, the
// service manager and the project the working directory resolves to; the web
// UI adds that it is the daemon answering.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mtch3n/trellis/internal/address"
	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/home"
	"github.com/mtch3n/trellis/internal/store"
	"github.com/mtch3n/trellis/internal/version"
)

// Check outcomes. warn means "works, but something will surprise you later";
// fail means a command is broken right now. Only fail sets a non-zero exit.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusFail = "fail"
)

// Check is one diagnostic. Fix is the command that resolves it, so an agent
// reading --json output can act without parsing prose.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func OK(name, detail string) Check { return Check{Name: name, Status: StatusOK, Detail: detail} }
func Fail(name, detail, fix string) Check {
	return Check{Name: name, Status: StatusFail, Detail: detail, Fix: fix}
}
func Warn(name, detail, fix string) Check {
	return Check{Name: name, Status: StatusWarn, Detail: detail, Fix: fix}
}

// Config reports what the configuration says, and warns when it could not be
// read at all: everything after it is then running on defaults.
func Config(cfg config.Config, err error) Check {
	if err != nil {
		return Warn("config", "unreadable, using defaults: "+err.Error(), "trellis config ls")
	}
	return OK("config", fmt.Sprintf("ui %s:%d, search %s", cfg.UI.Bind, cfg.UI.Port, cfg.Search.Method))
}

// Machine runs every check that describes the installation rather than the
// caller, in the order `trellis doctor` reports them: a broken storage root
// explains most of what follows.
func Machine(root string, cfg config.Config, cfgErr error) []Check {
	return []Check{Binary(), StorageRoot(root), Database(), Config(cfg, cfgErr), ProjectKeys(), VectorSearch(cfg)}
}

// Failed counts the checks that say something is broken right now. Warnings
// are not failures: they describe what will surprise someone later.
func Failed(checks []Check) int {
	n := 0
	for _, c := range checks {
		if c.Status == StatusFail {
			n++
		}
	}
	return n
}

func Binary() Check {
	exe, err := os.Executable()
	if err != nil {
		return Warn("binary", "cannot locate the running binary: "+err.Error(), "")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	return OK("binary", fmt.Sprintf("%s (%s, %s/%s)", exe, version.Version, runtime.GOOS, runtime.GOARCH))
}

func StorageRoot(root string) Check {
	info, err := os.Stat(root)
	if err != nil {
		return Fail("storage root", root+": "+err.Error(), "set TRELLIS_HOME to a writable directory")
	}
	if !info.IsDir() {
		return Fail("storage root", root+" is not a directory", "remove it or set TRELLIS_HOME elsewhere")
	}
	// Stat cannot tell us about write permission portably; a probe file can.
	probe := filepath.Join(root, ".doctor-write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return Fail("storage root", root+" is not writable: "+err.Error(), "fix the directory permissions")
	}
	_ = os.Remove(probe)
	return OK("storage root", root)
}

func Database() Check {
	path, err := home.DBPath()
	if err != nil {
		return Fail("database", err.Error(), "")
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return Warn("database", "no database yet at "+path, "trellis init")
	}
	// Opening runs any pending migrations, so a clean open is also a clean
	// schema. Counting projects proves the file is readable, not just present.
	db, err := store.Open(path)
	if err != nil {
		return Fail("database", "cannot open "+path+": "+err.Error(), "trellis backup, then restore or re-init")
	}
	defer db.Close()
	var projects int
	if err := db.Get(&projects, "SELECT count(*) FROM project"); err != nil {
		return Fail("database", "cannot read "+path+": "+err.Error(), "trellis maintenance")
	}
	return OK("database", fmt.Sprintf("%s (%d projects)", path, projects))
}

// ProjectKeys lists projects whose key predates the key grammar. They
// stay reachable with --project, but no marker can name them.
func ProjectKeys() Check {
	db, err := openExistingDB()
	if err != nil {
		return OK("project keys", "no database yet")
	}
	defer db.Close()
	var keys []string
	if err := db.Select(&keys, `SELECT key FROM project ORDER BY key`); err != nil {
		return Warn("project keys", "cannot read project keys: "+err.Error(), "trellis maintenance")
	}
	bad := slices.DeleteFunc(keys, address.ValidKey)
	if len(bad) == 0 {
		return OK("project keys", "a marker can name every key")
	}
	return Warn("project keys",
		fmt.Sprintf("no marker can name %s: %s", plural(len(bad), "this project", "these projects"), strings.Join(bad, ", ")),
		"trellis project merge <KEY> --into <VALID-KEY>")
}

// openExistingDB opens the database only when it already exists, so a check
// never creates one.
func openExistingDB() (*sqlx.DB, error) {
	path, err := home.DBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return store.Open(path)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// VectorSearch verifies the one part of search that depends on something
// outside the binary: a user-supplied embedding command or endpoint.
func VectorSearch(cfg config.Config) Check {
	vector := cfg.Search.Vector
	if !vector.Enabled {
		if cfg.Search.Method == "vector" || cfg.Search.Method == "hybrid" {
			return Warn("vector search", "search.method is "+cfg.Search.Method+" but search.vector.enabled is false",
				"trellis config set search.vector.enabled true")
		}
		return OK("vector search", "disabled")
	}
	switch vector.Provider {
	case "command":
		if vector.EmbedCommand == "" {
			return Fail("vector search", "provider is command but search.vector.embed_command is empty",
				"trellis config set search.vector.embed_command <path>")
		}
		program := strings.Fields(vector.EmbedCommand)[0]
		if _, err := exec.LookPath(program); err != nil {
			return Fail("vector search", "embed command not executable: "+program, "install it or fix search.vector.embed_command")
		}
		return OK("vector search", "command "+vector.EmbedCommand)
	case "http":
		if vector.Endpoint == "" {
			return Fail("vector search", "provider is http but search.vector.endpoint is empty",
				"trellis config set search.vector.endpoint <url>")
		}
		return OK("vector search", "http "+vector.Endpoint)
	case "":
		return Fail("vector search", "enabled but search.vector.provider is unset",
			"trellis config set search.vector.provider command")
	default:
		return OK("vector search", vector.Provider)
	}
}
