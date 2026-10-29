package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)

func TestVault_VAULT_C58_ArtifactRoundTripsAndCardShowListsIt(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	card := w.must(w.p.CreateCard(ctx, a, "APP", NewCard{Title: "draw it"}))

	added := w.must(w.p.AddArtifact(ctx, a, "APP", NewArtifact{Name: "Flow Diagram.png", Content: pngBytes}))
	sum := sha256.Sum256(pngBytes)
	if added.ContentHash != hex.EncodeToString(sum[:]) || added.Size != int64(len(pngBytes)) {
		t.Fatalf("added = %+v", added)
	}
	if added.Kind != "image" || added.MIME != "image/png" || added.Scope != ScopePersonal {
		t.Fatalf("added = %+v", added)
	}
	if err := w.p.LinkArtifact(ctx, a, "APP", added.Name, ArtifactTarget{Card: card.Ref}); err != nil {
		t.Fatal(err)
	}

	shown := w.must(w.p.ShowArtifact(ctx, a, "APP", added.Name))
	if !bytes.Equal(shown.Content, pngBytes) || shown.ContentHash != added.ContentHash {
		t.Fatalf("shown hash %s, %d bytes", shown.ContentHash, len(shown.Content))
	}
	byID := w.must(w.p.ShowArtifact(ctx, a, "APP", added.ID.String()))
	if !bytes.Equal(byID.Content, pngBytes) {
		t.Fatal("show by id returns other bytes")
	}

	seen := w.must(w.p.ShowCard(ctx, a, "APP", card.Ref))
	if !slices.ContainsFunc(seen.Artifacts, func(x Artifact) bool { return x.ID == added.ID }) {
		t.Fatalf("card_show artifacts = %+v", seen.Artifacts)
	}
	if seen.Artifacts[0].Content != nil {
		t.Fatal("card_show carries the artifact bytes")
	}
}

func TestVault_VAULT_C59_TooLargeArtifactIsAUsageErrorAndNothingIsStored(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")

	big := bytes.Repeat([]byte("a"), artifactSizeLimit+1)
	_, err := w.p.AddArtifact(ctx, a, "APP", NewArtifact{Name: "big.txt", Content: big})
	e := wantCode(t, err, "artifact_too_large")
	if e.Kind != KindUsage || !strings.Contains(e.Msg, "64 MiB") {
		t.Fatalf("error = %+v", e)
	}
	if n := w.count("artifact"); n != 0 {
		t.Fatalf("artifacts stored = %d", n)
	}

	w.must(w.p.AddArtifact(ctx, a, "APP", NewArtifact{Name: "edge.txt", Content: big[:artifactSizeLimit]}))
}

func TestVault_VAULT_C60_PersonalArtifactIsHiddenUntilShared(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	added := w.must(w.p.AddArtifact(ctx, a, "APP", NewArtifact{Name: "notes.txt", Content: []byte("ann's notes")}))

	for _, ref := range []string{added.Name, added.ID.String()} {
		_, err := w.p.ShowArtifact(ctx, b, "APP", ref)
		if e := wantCode(t, err, "artifact_not_found"); e.Kind != KindNotFound {
			t.Fatalf("kind = %s", e.Kind)
		}
	}
	if list := w.must(w.p.ListArtifacts(ctx, b, "APP")); len(list) != 0 {
		t.Fatalf("bob lists %+v", list)
	}
	_, err := w.p.ShareArtifact(ctx, b, "APP", added.ID.String())
	wantCode(t, err, "artifact_not_found")

	shared := w.must(w.p.ShareArtifact(ctx, a, "APP", added.Name))
	if shared.Scope != ScopeProject {
		t.Fatalf("scope after share = %q", shared.Scope)
	}
	for _, ref := range []string{added.Name, added.ID.String()} {
		got := w.must(w.p.ShowArtifact(ctx, b, "APP", ref))
		if string(got.Content) != "ann's notes" {
			t.Fatalf("bob reads %q", got.Content)
		}
	}
	if list := w.must(w.p.ListArtifacts(ctx, b, "APP")); len(list) != 1 {
		t.Fatalf("bob lists %+v", list)
	}
}
