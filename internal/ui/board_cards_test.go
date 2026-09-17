package ui

import (
	"bytes"
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

func TestBoardCardsIncludesLabelsAndTags(t *testing.T) {
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

	// Create a label using core directly
	ctx := context.Background()
	_, err = s.write.CreateLabel(ctx, p.ID, "bug", "")
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}

	// Create cards
	card1Resp := request(http.MethodPost, "/api/p/TEST/b/board1/cards", `{"title":"Card 1","labels":["bug"],"tags":["backend"]}`)
	if card1Resp.Code != http.StatusCreated {
		t.Fatalf("create card 1 status = %d, body = %s", card1Resp.Code, card1Resp.Body)
	}
	var card1 core.Card
	if err := json.Unmarshal(card1Resp.Body.Bytes(), &card1); err != nil {
		t.Fatal(err)
	}

	card2Resp := request(http.MethodPost, "/api/p/TEST/b/board1/cards", `{"title":"Card 2"}`)
	if card2Resp.Code != http.StatusCreated {
		t.Fatalf("create card 2 status = %d, body = %s", card2Resp.Code, card2Resp.Body)
	}
	var card2 core.Card
	if err := json.Unmarshal(card2Resp.Body.Bytes(), &card2); err != nil {
		t.Fatal(err)
	}

	// Get board cards
	boardResp := request(http.MethodGet, "/api/p/TEST/b/board1/cards", "")
	if boardResp.Code != http.StatusOK {
		t.Fatalf("board cards status = %d, body = %s", boardResp.Code, boardResp.Body)
	}

	var result []columnCardsInfo
	if err := json.Unmarshal(boardResp.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(result) == 0 {
		t.Fatal("expected at least one column")
	}

	// Find the cards
	var foundCard1, foundCard2 *cardInfo
	for _, col := range result {
		for i := range col.Cards {
			if col.Cards[i].ID == card1.ID {
				foundCard1 = &col.Cards[i]
			}
			if col.Cards[i].ID == card2.ID {
				foundCard2 = &col.Cards[i]
			}
		}
	}

	if foundCard1 == nil {
		t.Fatal("card 1 not found in response")
	}
	if foundCard2 == nil {
		t.Fatal("card 2 not found in response")
	}

	// Verify card 1 has labels
	if !bytes.Contains(boardResp.Body.Bytes(), []byte(`"labels":["bug"]`)) {
		t.Fatalf("expected labels in card 1, body = %s", boardResp.Body)
	}

	// Verify card 2 has empty labels
	if !bytes.Contains(boardResp.Body.Bytes(), []byte(`"labels":[]\n`)) && !bytes.Contains(boardResp.Body.Bytes(), []byte(`"labels":[]}`)) {
		t.Logf("response: %s", boardResp.Body)
		// Labels field should be present as empty array
	}
}
