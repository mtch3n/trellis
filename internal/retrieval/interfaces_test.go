package retrieval

import "testing"

func TestReciprocalRankFusionKeepsAgreementFirst(t *testing.T) {
	got := ReciprocalRankFusion([]Candidate{{ID: "exact"}, {ID: "shared"}}, []Candidate{{ID: "shared"}, {ID: "semantic"}})
	if len(got) != 3 || got[0].ID != "shared" {
		t.Fatalf("unexpected fusion: %#v", got)
	}
}
