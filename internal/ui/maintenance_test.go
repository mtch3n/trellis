package ui

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/home"
)

func TestGetMaintenanceReportsSizesAndOrphanCount(t *testing.T) {
	s := settingsTestServer(t)

	rec := request(t, s, http.MethodGet, "/api/maintenance", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		DatabaseBytes int64 `json:"database_bytes"`
		WALBytes      int64 `json:"wal_bytes"`
		OrphanHistory int   `json:"orphan_history"`
		HistoryKeep   int   `json:"history_keep"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.DatabaseBytes <= 0 {
		t.Errorf("database_bytes = %d, want > 0: trellis.db already exists", out.DatabaseBytes)
	}
	if out.OrphanHistory != 0 {
		t.Errorf("orphan_history = %d, want 0 on a fresh vault", out.OrphanHistory)
	}
	if out.HistoryKeep != 100 {
		t.Errorf("history_keep = %d, want 100 (the default)", out.HistoryKeep)
	}
}

func TestMaintenancePruneNothingSelectedIs400(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPost, "/api/maintenance/prune", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if out.Code != "nothing_to_prune" {
		t.Errorf("code = %q, want nothing_to_prune", out.Code)
	}
}

func TestMaintenancePruneEventsWithBeforeDeletesOldEvents(t *testing.T) {
	s := settingsTestServer(t)
	// A project creation records an event; that gives PruneHistory something
	// to delete once "before" is far enough in the future.
	if _, err := s.core.CreateProject(t.Context(), "TEST", false); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := s.db.Get(&before, `SELECT COUNT(*) FROM event`); err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("expected at least one event from CreateProject")
	}

	rec := request(t, s, http.MethodPost, "/api/maintenance/prune", `{"events":true,"before":"1ms"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Deleted == 0 {
		t.Error("deleted = 0, want at least one event pruned")
	}
}

func TestMaintenancePruneEventsWithoutBeforeIsInvalidRetention(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPost, "/api/maintenance/prune", `{"events":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if out.Code != "invalid_retention" {
		t.Errorf("code = %q, want invalid_retention", out.Code)
	}
}

func TestMaintenanceCompactReturnsStatus(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPost, "/api/maintenance/compact", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if out.Status != "compacted" {
		t.Errorf("status = %q, want compacted", out.Status)
	}
}

func TestLogsTailsTheLastNLines(t *testing.T) {
	s := settingsTestServer(t)
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(home.DaemonLogPath(root), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := request(t, s, http.MethodGet, "/api/logs?lines=3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Path   string   `json:"path"`
		Exists bool     `json:"exists"`
		Lines  []string `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if !out.Exists {
		t.Fatal("exists = false, want true")
	}
	if filepath.Base(out.Path) != "daemon.log" {
		t.Errorf("path = %q, want it to name daemon.log", out.Path)
	}
	if want := []string{"line 8", "line 9", "line 10"}; !equalStrings(out.Lines, want) {
		t.Errorf("lines = %v, want %v", out.Lines, want)
	}
}

func TestLogsMissingFileReportsNotExists(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodGet, "/api/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Exists bool     `json:"exists"`
		Lines  []string `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Exists {
		t.Error("exists = true, want false: no daemon.log was ever written")
	}
	if len(out.Lines) != 0 {
		t.Errorf("lines = %v, want empty", out.Lines)
	}
}

func TestLogsLinesAboveMaxIsClamped(t *testing.T) {
	s := settingsTestServer(t)
	root, err := home.Root()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := 1; i <= 5001; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	if err := os.WriteFile(home.DaemonLogPath(root), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := request(t, s, http.MethodGet, "/api/logs?lines=999999", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if len(out.Lines) != 5000 {
		t.Errorf("len(lines) = %d, want clamped to 5000", len(out.Lines))
	}
	if out.Lines[len(out.Lines)-1] != "line 5001" {
		t.Errorf("last line = %q, want line 5001", out.Lines[len(out.Lines)-1])
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
