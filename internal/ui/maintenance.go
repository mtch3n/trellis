package ui

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
)

type maintenanceStats struct {
	DatabaseBytes     int64 `json:"database_bytes"`
	WALBytes          int64 `json:"wal_bytes"`
	LeftoverRevisions int   `json:"leftover_revisions"`
	HistoryKeep       int   `json:"history_keep"`
}

// fileSize reports path's size, or 0 when it does not exist -- trellis.db-wal
// is absent between checkpoints, which is not an error.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	dbPath := filepath.Join(s.root, "trellis.db")
	leftovers, err := s.core.LeftoverRevisionsCount(ctx)
	if err != nil {
		s.coreError(w, err)
		return
	}
	cfg, err := config.Load(s.root)
	if err != nil {
		cfg = config.Defaults()
	}
	writeJSON(w, http.StatusOK, maintenanceStats{
		DatabaseBytes:     fileSize(dbPath),
		WALBytes:          fileSize(dbPath + "-wal"),
		LeftoverRevisions: leftovers,
		HistoryKeep:       cfg.History.EffectiveKeep(),
	})
}

type pruneRequest struct {
	Events            bool   `json:"events"`
	Invocations       bool   `json:"invocations"`
	Before            string `json:"before"`
	Revisions         bool   `json:"revisions"`
	LeftoverRevisions bool   `json:"leftover_revisions"`
}

// handleMaintenancePrune mirrors `trellis maintenance prune`: at least one
// selector is required, and before is required with events or invocations.
func (s *Server) handleMaintenancePrune(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	var in pruneRequest
	if !decodeJSON(w, r, &in) {
		s.error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !in.Events && !in.Invocations && !in.Revisions && !in.LeftoverRevisions {
		s.coreError(w, core.ErrUsage("nothing_to_prune",
			"select events, invocations, revisions and/or leftover_revisions", ""))
		return
	}

	var total int64
	if in.Events || in.Invocations {
		age, err := core.ParseRetention(in.Before)
		if err != nil {
			s.coreError(w, core.ErrUsage("invalid_retention", err.Error(), ""))
			return
		}
		before := time.Now().Add(-age).UnixMilli()
		n, err := s.writer(r).PruneHistory(ctx, before, in.Events, in.Invocations)
		if err != nil {
			s.coreError(w, err)
			return
		}
		total += n
	}
	if in.Revisions {
		n, err := s.writer(r).PruneRevisions(ctx)
		if err != nil {
			s.coreError(w, err)
			return
		}
		total += n
	}
	if in.LeftoverRevisions {
		n, err := s.writer(r).PruneLeftoverRevisions(ctx)
		if err != nil {
			s.coreError(w, err)
			return
		}
		total += n
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": total})
}

func (s *Server) handleMaintenanceCompact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	if err := s.writer(r).Compact(ctx); err != nil {
		s.coreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "compacted"})
}

const (
	logsDefaultLines = 500
	logsMaxLines     = 5000
	// logsMaxReadBytes bounds how much of daemon.log the logs route ever
	// reads, however many lines are requested: a run left going for months
	// must not make every request to this route read the whole file.
	logsMaxReadBytes = 1 << 20
)

type logsResponse struct {
	Path   string   `json:"path"`
	Exists bool     `json:"exists"`
	Lines  []string `json:"lines"`
}

// handleLogs tails the daemon's log file. A daemon under systemd logs to
// journalctl instead, and one under launchd has no file at all; either way,
// a missing file is reported, not an error.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.root, "daemon.log")

	n := logsDefaultLines
	if raw := r.URL.Query().Get("lines"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > logsMaxLines {
		n = logsMaxLines
	}

	lines, exists, err := tailLines(path, n)
	if err != nil {
		s.error(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, logsResponse{Path: path, Exists: exists, Lines: lines})
}

// tailLines returns path's last n lines, reading no more than the final
// logsMaxReadBytes of the file. When the read was truncated, the first
// recovered line is dropped: it is likely a fragment the seek cut into, not
// a whole line.
func tailLines(path string, n int) (lines []string, exists bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := info.Size()
	var start int64
	truncated := false
	if size > logsMaxReadBytes {
		start = size - logsMaxReadBytes
		truncated = true
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}

	text := strings.TrimRight(string(data), "\n")
	var all []string
	if text != "" {
		all = strings.Split(text, "\n")
	}
	if truncated && len(all) > 0 {
		all = all[1:]
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, true, nil
}
