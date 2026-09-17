package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
	"github.com/mtch3n/trellis/internal/store"
)

func TestClaimAndReleaseCard(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	c := core.New(db, core.FixedClock{MS: 1_000_000}, "ui-test", dir)
	p, err := c.CreateProject(context.Background(), "TEST", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateBoard(context.Background(), p.ID, "board1", true)
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	request := func(method, path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		return rec
	}

	// Create a card
	createResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards", `{"title":"Test Card"}`)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create card status = %d, body = %s", createResp.Code, createResp.Body)
	}

	var card core.Card
	if err := json.Unmarshal(createResp.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}

	cardRef := card.Ref

	// Test claiming a card
	claimResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/claim", `{"ttl_minutes":30}`)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim card status = %d, body = %s", claimResp.Code, claimResp.Body)
	}

	var claimedCard core.Card
	if err := json.Unmarshal(claimResp.Body.Bytes(), &claimedCard); err != nil {
		t.Fatal(err)
	}

	if claimedCard.ClaimedBy == nil {
		t.Fatal("expected card to have a claimant after claiming")
	}

	firstClaimant := *claimedCard.ClaimedBy

	// Test claiming an already claimed card with the same actor succeeds (extends claim)
	claimAgainResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/claim", `{"ttl_minutes":30}`)
	if claimAgainResp.Code != http.StatusOK {
		t.Fatalf("claim already claimed card (same actor) status = %d, expected 200, body = %s", claimAgainResp.Code, claimAgainResp.Body)
	}

	var claimedAgain core.Card
	if err := json.Unmarshal(claimAgainResp.Body.Bytes(), &claimedAgain); err != nil {
		t.Fatal(err)
	}
	if claimedAgain.ClaimedBy == nil || *claimedAgain.ClaimedBy != firstClaimant {
		t.Fatalf("expected same claimant after re-claiming")
	}

	// Test releasing a card
	releaseResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/release", "")
	if releaseResp.Code != http.StatusOK {
		t.Fatalf("release card status = %d, body = %s", releaseResp.Code, releaseResp.Body)
	}

	var releasedCard core.Card
	if err := json.Unmarshal(releaseResp.Body.Bytes(), &releasedCard); err != nil {
		t.Fatal(err)
	}

	if releasedCard.ClaimedBy != nil {
		t.Fatalf("expected card claimant to be nil after release, got %v", releasedCard.ClaimedBy)
	}

	// Test that claiming again extends the claim
	claimResp = request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/claim", `{"ttl_minutes":60}`)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim card status = %d", claimResp.Code)
	}
}

// A 409 names who holds the card, so the browser can say so without a second
// request.
func TestClaimContentionNamesTheClaimant(t *testing.T) {
	t.Setenv("TRELLIS_AGENT", "")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "trellis.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	c := core.New(db, core.FixedClock{MS: 1_000_000}, "agent:claimant", dir)
	p, err := c.CreateProject(ctx, "CLAIMED", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CreateBoard(ctx, p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := c.CreateCard(ctx, p.ID, b.ID, core.NewCard{Title: "claimed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ClaimCard(ctx, card.ID, 0, false, ""); err != nil {
		t.Fatal(err)
	}

	s := NewServer(c, db, "127.0.0.1:0", filepath.Join(dir, "trellis.db"))
	req := httptest.NewRequest(http.MethodPost, "/api/p/CLAIMED/b/default/cards/"+card.Ref+"/claim", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("claim = %d, want 409: %s", rec.Code, rec.Body)
	}
	var body struct {
		Code   string `json:"code"`
		Detail struct {
			ClaimedBy struct {
				ID string `json:"id"`
			} `json:"claimed_by"`
			RecommendedAction string `json:"recommended_action"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "contention" || body.Detail.ClaimedBy.ID != "agent:claimant" || body.Detail.RecommendedAction == "" {
		t.Errorf("409 does not name the claimant: %s", rec.Body)
	}
}
