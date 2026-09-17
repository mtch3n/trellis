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
		// retired is spelled out only where the vocabulary test cannot see
		// it: this word breaks none of its rules.
		retired []string
	}{
		{"/api/projects", []string{`"expired_claims":`}, nil},
		{"/api/p/WIRE/b/default/cards", []string{`"claimed_by":"ui-test"`}, nil},
		{"/api/p/WIRE/cards/" + card.Ref, []string{`"claimed_by":"ui-test"`, `"events":[{`}, []string{`"activity"`}},
		{"/api/p/WIRE/events", []string{`"entity":"card"`}, nil},
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
		for _, retired := range tc.retired {
			if strings.Contains(rec.Body.String(), retired) {
				t.Errorf("%s: retired %s in %s", tc.path, retired, rec.Body)
			}
		}
	}
}
