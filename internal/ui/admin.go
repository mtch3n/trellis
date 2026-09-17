package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/doctor"
	"github.com/mtch3n/trellis/internal/version"
)

// agentInfo is one agent and the work it claims. `agent ls` answers "who is
// active, where, holding what"; the table it prints leaves the cards out, and
// the cards are the reason anyone asks.
type agentInfo struct {
	core.Agent
	Claimed []claimedCard `json:"claimed"`
}

type claimedCard struct {
	Ref        string `db:"ref" json:"ref"`
	Title      string `db:"title" json:"title"`
	ProjectKey string `db:"project_key" json:"project"`
	ClaimUntil *int64 `db:"claim_until" json:"claim_until,omitempty"`
	// Expired says the claim has run out, so the card is claimable again
	// whatever the agent thinks.
	Expired bool `json:"expired"`
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	agents, err := s.core.ListAgents(ctx)
	if err != nil {
		s.coreError(w, err)
		return
	}
	var claims []struct {
		claimedCard
		Actor string `db:"claimed_by"`
	}
	if err := s.db.SelectContext(ctx, &claims, `
		SELECT c.ref, c.title, c.claimed_by, c.claim_until, p.key AS project_key
		FROM card c JOIN project p ON p.id = c.project_id
		WHERE c.claimed_by IS NOT NULL AND c.archived_at IS NULL
		ORDER BY c.ref`); err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UnixMilli()
	byActor := map[string][]claimedCard{}
	for _, claim := range claims {
		card := claim.claimedCard
		card.Expired = card.ClaimUntil == nil || *card.ClaimUntil < now
		byActor[claim.Actor] = append(byActor[claim.Actor], card)
	}
	out := make([]agentInfo, 0, len(agents))
	for _, agent := range agents {
		out = append(out, agentInfo{Agent: agent, Claimed: byActor[agent.ID]})
		delete(byActor, agent.ID)
	}
	// A claim is what holds a card, not an agent row, so an actor that never
	// registered still has to appear: otherwise a card would be claimed by
	// nobody the page can name.
	for actor, cards := range byActor {
		out = append(out, agentInfo{
			Agent:   core.Agent{ID: actor, Handle: actor, Kind: "unregistered"},
			Claimed: cards,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDoctor runs the checks that describe this installation, the ones
// `trellis doctor` runs. The daemon's own state is not among them — this
// answer is proof the daemon is up — so it is added here instead.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	cfg, cfgErr := config.Load(s.root)
	if cfgErr != nil {
		cfg = config.Defaults()
	}
	checks := append([]doctor.Check{doctor.OK("daemon", "running; it is serving this page")},
		doctor.Machine(s.root, cfg, cfgErr)...)
	writeJSON(w, http.StatusOK, struct {
		Checks []doctor.Check `json:"checks"`
		Failed int            `json:"failed"`
	}{checks, doctor.Failed(checks)})
}

// versionInfo is what this binary is, and what to run to replace it. The
// browser never installs anything: a web page must not be able to swap the
// binary serving it.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// Latest is the newest release on GitHub, when it was asked for.
	Latest string `json:"latest,omitempty"`
	// Update is the command that installs it.
	Update string `json:"update,omitempty"`
}

// handleVersion reports this build, and with ?check=1 asks GitHub for the
// latest release. The check is a request to another machine, so it happens
// only when someone asks for it.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	info := versionInfo{
		Version: version.Version, Commit: version.Commit, Date: version.Date,
		OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
	if truthy(r.URL.Query().Get("check")) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		latest, err := latestRelease(ctx)
		if err != nil {
			s.error(w, http.StatusBadGateway, "could not reach GitHub: "+err.Error())
			return
		}
		info.Latest = latest
		if latest != "" && latest != version.Version {
			info.Update = "trellis update"
		}
	}
	writeJSON(w, http.StatusOK, info)
}

// latestRelease reads the newest release tag from GitHub, the same endpoint
// `trellis update --check` reads.
func latestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+version.Repository+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", core.ErrUsage("release_unavailable", "GitHub answered "+resp.Status, "trellis update --check")
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.UnmarshalRead(resp.Body, &release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

func (s *Server) handleVectorStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	p, err := s.projectByKey(ctx, r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	status, err := s.search.VectorStatus(ctx, p.ID)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleVectorRun does one of the four things that can be done to a vector
// index: rebuild it, prune what has left the vault, make the extension
// rebuild its cache, or compact the file. Embedding can be slow, so these
// take longer than a read; the request timeout is the caller's own.
func (s *Server) handleVectorRun(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectByKey(r.Context(), r.PathValue("key"))
	if err != nil {
		s.coreError(w, err)
		return
	}
	switch r.PathValue("action") {
	case "rebuild":
		n, err := s.search.VectorRebuild(r.Context(), p.ID)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"rebuilt": n})
	case "prune":
		n, err := s.search.VectorPrune(r.Context(), p.ID)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"pruned": n})
	case "reindex":
		result, err := s.search.VectorReindex(r.Context(), p.ID)
		if err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"result": result})
	case "compact":
		if err := s.search.VectorCompact(r.Context(), p.ID); err != nil {
			s.error(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "compacted"})
	default:
		s.error(w, http.StatusNotFound, "no such vector action")
	}
}

type backupRequest struct {
	// Directory holds the backups; empty means <root>/backups, which is
	// beside the database it copies.
	Directory string `json:"directory"`
	// Keep is how many newest backups a prune leaves.
	Keep int `json:"keep"`
}

// backupDir resolves where backups go. A relative directory is refused: the
// daemon's working directory is not something the person at the browser can
// see, so a relative path would land somewhere they did not choose.
func (s *Server) backupDir(in backupRequest) (string, error) {
	dir := strings.TrimSpace(in.Directory)
	if dir == "" {
		return filepath.Join(s.root, "backups"), nil
	}
	if strings.HasPrefix(dir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), string(filepath.Separator)))
	}
	if !filepath.IsAbs(dir) {
		return "", core.ErrUsage("relative_directory", "a backup directory has to be an absolute path",
			"use a path starting at the root, or leave it empty for the default")
	}
	return dir, nil
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var in backupRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing was written")
		return
	}
	dir, err := s.backupDir(in)
	if err != nil {
		s.coreError(w, err)
		return
	}
	path, err := s.core.BackupInto(ctx, dir)
	if err != nil {
		s.coreError(w, err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"path": path, "bytes": info.Size()})
}

func (s *Server) handleBackupPrune(w http.ResponseWriter, r *http.Request) {
	var in backupRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON; nothing was deleted")
		return
	}
	dir, err := s.backupDir(in)
	if err != nil {
		s.coreError(w, err)
		return
	}
	deleted, err := s.core.BackupsPrune(dir, in.Keep)
	if err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": deleted})
}

// handleBackups lists the backups Trellis wrote, newest first, so a person
// can see what pruning would leave.
func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	dir, err := s.backupDir(backupRequest{Directory: r.URL.Query().Get("directory")})
	if err != nil {
		s.coreError(w, err)
		return
	}
	type backup struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
		When  int64  `json:"when"`
	}
	out := []backup{}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "trellis-backup-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, backup{Name: entry.Name(), Bytes: info.Size(), When: info.ModTime().UnixMilli()})
	}
	// Newest first, the order a person reads them in.
	slices.SortFunc(out, func(a, b backup) int { return int(b.When - a.When) })
	writeJSON(w, http.StatusOK, map[string]any{"directory": dir, "backups": out})
}
