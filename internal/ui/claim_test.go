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

	if claimedCard.Owner == nil {
		t.Fatal("expected card to have an owner after claiming")
	}

	firstOwner := *claimedCard.Owner

	// Test claiming an already claimed card with the same actor succeeds (extends lease)
	claimAgainResp := request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/claim", `{"ttl_minutes":30}`)
	if claimAgainResp.Code != http.StatusOK {
		t.Fatalf("claim already claimed card (same actor) status = %d, expected 200, body = %s", claimAgainResp.Code, claimAgainResp.Body)
	}

	var claimedAgain core.Card
	if err := json.Unmarshal(claimAgainResp.Body.Bytes(), &claimedAgain); err != nil {
		t.Fatal(err)
	}
	if claimedAgain.Owner == nil || *claimedAgain.Owner != firstOwner {
		t.Fatalf("expected same owner after re-claiming")
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

	if releasedCard.Owner != nil {
		t.Fatalf("expected card owner to be nil after release, got %v", releasedCard.Owner)
	}

	// Test that claiming again extends the lease
	claimResp = request(http.MethodPost, "/api/p/TEST/b/board1/cards/"+cardRef+"/claim", `{"ttl_minutes":60}`)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim card status = %d", claimResp.Code)
	}
}
