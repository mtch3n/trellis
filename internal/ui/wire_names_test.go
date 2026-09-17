package ui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/core"
)

// TestAPIResponsesUseTheGlossary pins the web API's names (spec §5, "JSON").
// The vocabulary test keeps the retired names out of the handlers.
func TestAPIResponsesUseTheGlossary(t *testing.T) {
	s := settingsTestServer(t)
	p, err := s.core.CreateProject(t.Context(), "WIRE", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.core.CreateBoard(t.Context(), p.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.core.CreateCard(t.Context(), p.ID, b.ID, core.NewCard{Title: "Claimed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.core.ClaimCard(t.Context(), card.ID, 60_000, false, ""); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		want []string
	}{
		{"/api/projects", []string{`"expired_claims":`}},
		{"/api/p/WIRE/b/default/cards", []string{`"claimed_by":"ui-test"`}},
		{"/api/p/WIRE/cards/" + card.Ref, []string{`"claimed_by":"ui-test"`, `"events":[{`}},
		{"/api/p/WIRE/events", []string{`"entity":"card"`}},
	} {
		rec := request(t, s, http.MethodGet, tc.path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", tc.path, rec.Code, rec.Body)
		}
		for _, want := range tc.want {
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("%s: no %s in %s", tc.path, want, rec.Body)
			}
		}
	}
}
