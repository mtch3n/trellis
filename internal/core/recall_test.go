package core

import (
	"slices"
	"testing"
)

// The premise of the command: Search quotes its caller's text into one phrase,
// which finds nothing when the text is a sentence rather than a query. If this
// test ever fails at the Search step, recall has lost its reason to exist.
func TestRecallFindsWhatAPhraseSearchCannot(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title:   "Lease renewal on claim",
		Summary: "A claim starts the lease and a note renews it.",
	}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	sentence := "why does the lease expire when an agent goes quiet halfway through"

	phrase, err := c.Search(ctx, p.ID, sentence, SearchOpts{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(phrase) != 0 {
		t.Fatalf("Search matched a whole sentence (%d hits); recall's premise no longer holds", len(phrase))
	}

	hits, err := c.Recall(ctx, p.ID, sentence, RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Recall found nothing in a sentence naming the entry's subject")
	}
	if hits[0].Ref != "/XPSCTL/vault/lease-renewal-on-claim" {
		t.Errorf("first hit = %q, want /XPSCTL/vault/lease-renewal-on-claim", hits[0].Ref)
	}
}

func TestRecallCarriesTheLineThatDecidesWhetherToOpen(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title:   "Vector index rebuild",
		Summary: "Rebuilds on every write above ten thousand rows",
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	hits, err := c.Recall(ctx, p.ID, "vector rebuild cost", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) == 0 || hits[0].Recap != "Rebuilds on every write above ten thousand rows" {
		t.Fatalf("recap = %+v, want the summary as fallback", hits)
	}

	// A pinned recap is written for exactly this job, so it outranks summary.
	if _, err := c.db.Exec(`UPDATE entry SET recap = ? WHERE id = ?`,
		"Rebuild is O(n) and blocks writes", entry.ID); err != nil {
		t.Fatalf("setting recap: %v", err)
	}
	hits, err = c.Recall(ctx, p.ID, "vector rebuild cost", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) == 0 || hits[0].Recap != "Rebuild is O(n) and blocks writes" {
		t.Errorf("recap = %+v, want the pinned recap to win", hits)
	}
}

func TestRecallOmitsRefsTheCallerAlreadyHolds(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	entry, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Staleness and leases", Summary: "When a quiet lease may be taken over",
	})
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	hits, err := c.Recall(ctx, p.ID, "staleness", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Recall returned %d hits, want 1", len(hits))
	}

	hits, err = c.Recall(ctx, p.ID, "staleness", RecallOpts{Exclude: []string{entry.Ref}})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Recall resent a ref the caller already holds: %+v", hits)
	}
}

func TestRecallPutsEntriesBeforeCards(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()

	if _, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Fix telemetry pipeline"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Telemetry pipeline decision", Summary: "Why batching beat streaming",
	}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	hits, err := c.Recall(ctx, p.ID, "the telemetry pipeline keeps dropping spans", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Recall returned %d hits, want 2", len(hits))
	}
	if hits[0].Kind != "knowledge" || hits[1].Kind != "card" {
		t.Errorf("order = %s then %s, want knowledge then card", hits[0].Kind, hits[1].Kind)
	}
}

func TestRecallHonoursLimit(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	for _, title := range []string{"Retry budget", "Retry jitter", "Retry ceiling"} {
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{Title: title, Summary: "retry"}); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
	}

	hits, err := c.Recall(ctx, p.ID, "retry", RecallOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("Recall returned %d hits, want 2", len(hits))
	}
}

func TestRecallWithoutUsableTermsIsEmptyNotAnError(t *testing.T) {
	c, p, _ := vaultCore(t)

	hits, err := c.Recall(t.Context(), p.ID, "ok can you do this for me", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Recall = %+v, want nothing from a prompt with no content words", hits)
	}
}

func TestRecallTermsRankRareTokensAboveLongProse(t *testing.T) {
	// internationalization is far longer, but fts5 carries a digit and so
	// narrows the search much further; it must not be crowded out.
	got := recallTerms("the fts5 tokenizer mishandles internationalization", 2)
	if !slices.Contains(got, "fts5") {
		t.Errorf("recallTerms = %v, want fts5 kept", got)
	}
}

func TestRecallTermsDropFunctionWordsAndStubs(t *testing.T) {
	if got := recallTerms("can you do this for me ok", 4); len(got) != 0 {
		t.Errorf("recallTerms = %v, want none", got)
	}
}

func TestRecallTermsAreStableForTheSameText(t *testing.T) {
	text := "the daemon restart loses the vector index and the lease"
	first := recallTerms(text, 3)
	for range 5 {
		if got := recallTerms(text, 3); !slices.Equal(got, first) {
			t.Fatalf("recallTerms = %v then %v; map order reached the query", first, got)
		}
	}
}

func TestRecallLiftsHitsConnectedToOtherHits(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	// Equal textual match; only the link graph tells them apart.
	mk := func(title, body string) {
		t.Helper()
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title: title, Summary: "retry budget", Body: body,
		}); err != nil {
			t.Fatalf("CreateEntry %s: %v", title, err)
		}
	}
	mk("Retry alpha", "retry budget notes")
	mk("Retry beta", "retry budget notes, see [[retry-alpha]]")
	mk("Retry gamma", "retry budget notes, unconnected")

	hits, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{Limit: 3})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("Recall returned %d hits, want 3", len(hits))
	}
	if hits[2].Ref != "/XPSCTL/vault/retry-gamma" {
		t.Errorf("order = %s %s %s; want the unconnected entry last",
			hits[0].Ref, hits[1].Ref, hits[2].Ref)
	}
}

func TestRecallCountsALinkToAHubForLessThanALinkToARarity(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	mk := func(title, body string) {
		t.Helper()
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title: title, Summary: "retry budget", Body: body,
		}); err != nil {
			t.Fatalf("CreateEntry %s: %v", title, err)
		}
	}
	mk("Retry hub", "retry budget, the index everything points at")
	mk("Retry rarity", "retry budget, cited by almost nothing")
	// Five entries that do not match the query but do make the hub a hub.
	for n := range 5 {
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title:   "Unrelated " + string(rune('a'+n)),
			Summary: "nothing to do with the query",
			Body:    "points at [[retry-hub]]",
		}); err != nil {
			t.Fatalf("CreateEntry unrelated: %v", err)
		}
	}
	mk("Retry alpha", "retry budget, see [[retry-hub]]")
	mk("Retry beta", "retry budget, see [[retry-rarity]]")

	hits, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	rank := map[string]int{}
	for i, h := range hits {
		rank[h.Ref] = i
	}
	alpha, okA := rank["/XPSCTL/vault/retry-alpha"]
	beta, okB := rank["/XPSCTL/vault/retry-beta"]
	if !okA || !okB {
		t.Fatalf("both linkers should be recalled; got %v", rank)
	}
	if beta > alpha {
		t.Errorf("beta (links a rarity) ranked %d, alpha (links the hub) ranked %d; "+
			"a link to a hub should be worth less", beta, alpha)
	}
}

func TestRecallWithoutLinksKeepsFTSOrder(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	for _, title := range []string{"Retry budget", "Retry jitter", "Retry ceiling"} {
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title: title, Summary: "retry", Body: "no links here",
		}); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
	}
	first, err := c.Recall(ctx, p.ID, "retry", RecallOpts{Limit: 3})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	for range 3 {
		again, err := c.Recall(ctx, p.ID, "retry", RecallOpts{Limit: 3})
		if err != nil {
			t.Fatalf("Recall: %v", err)
		}
		for i := range first {
			if again[i].Ref != first[i].Ref {
				t.Fatalf("unlinked order is unstable: %s then %s at %d",
					first[i].Ref, again[i].Ref, i)
			}
		}
	}
}

func TestRecallNarrowedToAnEntryDimensionDropsCards(t *testing.T) {
	c, p, b := vaultCore(t)
	ctx := t.Context()

	if _, err := c.CreateCard(ctx, p.ID, b.ID, NewCard{Title: "Fix retry budget"}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
		Title: "Retry budget decision", Summary: "retry budget", Provenance: "extracted",
	}); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	wide, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(wide) != 2 {
		t.Fatalf("unfiltered recall returned %d, want the card and the entry", len(wide))
	}

	// A card carries neither template nor provenance, so keeping it in a
	// narrowed result would answer a question nobody asked.
	narrow, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{Provenances: []string{"extracted"}})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(narrow) != 1 || narrow[0].Kind != "knowledge" {
		t.Errorf("narrowed recall = %+v, want the entry alone", narrow)
	}
}

func TestRecallHoldsOutAnIngestionPath(t *testing.T) {
	c, p, _ := vaultCore(t)
	ctx := t.Context()

	for _, prov := range []string{"authored", "extracted"} {
		if _, err := c.CreateEntry(ctx, p.ID, NewEntry{
			Title: "Retry budget " + prov, Summary: "retry budget", Provenance: prov,
		}); err != nil {
			t.Fatalf("CreateEntry: %v", err)
		}
	}
	// Holding one path out is what makes a comparison possible at all.
	hits, err := c.Recall(ctx, p.ID, "retry budget", RecallOpts{Provenances: []string{"authored"}})
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 1 || hits[0].Ref != "/XPSCTL/vault/retry-budget-authored" {
		t.Errorf("hits = %+v, want only the authored entry", hits)
	}
}
