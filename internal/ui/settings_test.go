package ui

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/config"
	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

// settingsTestServer builds a server whose root and database live in the
// same directory, so config.yaml and the database this test controls agree
// with each other.
func settingsTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", root)
	return NewServer(c, db, "127.0.0.1:0", filepath.Join(root, "trellis.db"))
}

func request(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestGetSettingsListsEveryKeyWithDefaultsAndSource(t *testing.T) {
	s := settingsTestServer(t)

	rec := request(t, s, http.MethodGet, "/api/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out settingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.File == "" || !strings.HasSuffix(out.File, "config.yaml") {
		t.Errorf("file = %q, want a path ending in config.yaml", out.File)
	}
	if len(out.Settings) != len(config.AllKeys()) {
		t.Fatalf("settings has %d entries, want %d", len(out.Settings), len(config.AllKeys()))
	}
	byKey := map[string]SettingInfo{}
	for _, setting := range out.Settings {
		byKey[setting.Key] = setting
	}
	leaseTTL, ok := byKey["claim.ttl"]
	if !ok {
		t.Fatal("claim.ttl missing from settings")
	}
	if leaseTTL.Source != "default" || leaseTTL.Value != "30m" || leaseTTL.Default != "30m" {
		t.Errorf("claim.ttl = %+v, want value/default 30m, source default", leaseTTL)
	}
	if !leaseTTL.Editable || leaseTTL.Restart {
		t.Errorf("claim.ttl editable=%v restart=%v, want true/false", leaseTTL.Editable, leaseTTL.Restart)
	}
	uiPort, ok := byKey["ui.port"]
	if !ok {
		t.Fatal("ui.port missing from settings")
	}
	if uiPort.Editable {
		t.Error("ui.port must not be editable")
	}
	searchMethod, ok := byKey["search.method"]
	if !ok {
		t.Fatal("search.method missing from settings")
	}
	if len(searchMethod.Choices) != 3 {
		t.Errorf("search.method choices = %v, want 3", searchMethod.Choices)
	}
}

func TestPatchSettingsWritesAndReportsSource(t *testing.T) {
	s := settingsTestServer(t)

	rec := request(t, s, http.MethodPatch, "/api/settings", `{"set":{"claim.ttl":"45m","history.keep":50,"board.default_columns":["todo","done"]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		File     string        `json:"file"`
		Settings []SettingInfo `json:"settings"`
		Restart  []string      `json:"restart"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Restart == nil || len(out.Restart) != 0 {
		t.Errorf("restart = %v, want an empty (but present) list", out.Restart)
	}
	byKey := map[string]SettingInfo{}
	for _, setting := range out.Settings {
		byKey[setting.Key] = setting
	}
	if leaseTTL := byKey["claim.ttl"]; leaseTTL.Value != "45m" || leaseTTL.Source != "config" {
		t.Errorf("claim.ttl = %+v, want value 45m, source config", leaseTTL)
	}
	if keep := byKey["history.keep"]; keep.Value != float64(50) && keep.Value != 50 {
		t.Errorf("history.keep = %+v, want 50", keep)
	}

	// Reading it back through a fresh GET must agree.
	getRec := request(t, s, http.MethodGet, "/api/settings", "")
	var getOut settingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getOut); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, setting := range getOut.Settings {
		if setting.Key == "claim.ttl" && setting.Value != "45m" {
			t.Errorf("GET after PATCH: claim.ttl = %v, want 45m", setting.Value)
		}
	}
}

func TestPatchSettingsReportsRestartForKeysThatNeedIt(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPatch, "/api/settings", `{"set":{"search.method":"vector"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Restart []string `json:"restart"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(out.Restart) != 1 || out.Restart[0] != "search.method" {
		t.Errorf("restart = %v, want [search.method]", out.Restart)
	}
}

func TestPatchSettingsInvalidBatchWritesNothingAndReportsProblems(t *testing.T) {
	s := settingsTestServer(t)
	rec := request(t, s, http.MethodPatch, "/api/settings",
		`{"set":{"claim.ttl":"45m","history.keep":-1,"ui.port":9999}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out struct {
		Error    string   `json:"error"`
		Code     string   `json:"code"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v, body = %s", err, rec.Body)
	}
	if out.Code != "invalid_settings" {
		t.Errorf("code = %q, want invalid_settings", out.Code)
	}
	if len(out.Problems) != 2 {
		t.Errorf("problems = %v, want 2 (history.keep and ui.port)", out.Problems)
	}

	// Nothing must have been written: claim.ttl (valid on its own) must not
	// have landed either.
	getRec := request(t, s, http.MethodGet, "/api/settings", "")
	var getOut settingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getOut); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, setting := range getOut.Settings {
		if setting.Key == "claim.ttl" && setting.Source != "default" {
			t.Errorf("claim.ttl source = %q, want default: the whole batch should have been refused", setting.Source)
		}
	}
}

func TestPatchSettingsCallsTheLiveHook(t *testing.T) {
	s := settingsTestServer(t)
	p, err := s.core.CreateProject(context.Background(), "TEST", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.core.CreateBoard(context.Background(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	created := request(t, s, http.MethodPost, "/api/p/TEST/b/default/cards", `{"title":"card"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create card status = %d, body = %s", created.Code, created.Body)
	}
	var card core.Card
	if err := json.Unmarshal(created.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	_ = b

	patchRec := request(t, s, http.MethodPatch, "/api/settings", `{"set":{"claim.ttl":"2h"}}`)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", patchRec.Code, patchRec.Body)
	}

	claimRec := request(t, s, http.MethodPost, "/api/p/TEST/b/default/cards/"+card.Ref+"/claim", `{}`)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body = %s", claimRec.Code, claimRec.Body)
	}
	var claimed core.Card
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.LeaseUntil == nil {
		t.Fatal("claimed card has no claim_until")
	}
	want := int64(1_000_000) + int64(2*60*60*1000)
	if *claimed.LeaseUntil != want {
		t.Errorf("claim_until = %d, want %d (2h lease TTL applied live)", *claimed.LeaseUntil, want)
	}
}

func TestSetGlobalValuesUnsetRoute(t *testing.T) {
	s := settingsTestServer(t)
	if rec := request(t, s, http.MethodPatch, "/api/settings", `{"set":{"claim.ttl":"45m"}}`); rec.Code != http.StatusOK {
		t.Fatalf("set status = %d, body = %s", rec.Code, rec.Body)
	}
	rec := request(t, s, http.MethodPatch, "/api/settings", `{"unset":["claim.ttl"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unset status = %d, body = %s", rec.Code, rec.Body)
	}
	var out settingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, setting := range out.Settings {
		if setting.Key == "claim.ttl" && (setting.Source != "default" || setting.Value != "30m") {
			t.Errorf("claim.ttl = %+v, want back to the default after unset", setting)
		}
	}
}
