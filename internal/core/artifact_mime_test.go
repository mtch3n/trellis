package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A genuine PDF always sniffs as application/pdf on its own; the extension
// fallback only ever runs for bytes that sniffed as octet-stream, which a
// real PDF never does. Letting *.pdf claim the kind through the extension
// alone would hand arbitrary bytes the one MIME type the web server serves
// without a sandbox.
func TestExtensionAloneNeverMakesAPDF(t *testing.T) {
	c, p, _ := kbCore(t)
	src := filepath.Join(t.TempDir(), "fake.pdf")
	if err := os.WriteFile(src, []byte("\x01\x02\x03\x04\x05\x06\x07\x08"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := c.CreateArtifact(t.Context(), p.ID, src)
	if artifactErrCode(err) != "artifact_type_not_allowed" {
		t.Fatalf("err = %v, want artifact_type_not_allowed", err)
	}
}

// The same bytes with a real PDF header still sniff, and resolve, as a PDF.
func TestARealPDFIsStillAcceptedAsDocument(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "doc.pdf", "%PDF-1.7\nreal pdf content\n")
	if a.Kind != "document" || a.MIME != "application/pdf" {
		t.Errorf("kind = %q, mime = %q, want document / application/pdf", a.Kind, a.MIME)
	}
}

// The fallback still has to work for every other type: bytes that sniff to
// octet-stream but whose extension names a permitted, non-PDF kind must still
// resolve through the extension.
func TestExtensionFallbackStillResolvesNonPDFTypes(t *testing.T) {
	c, p, _ := kbCore(t)
	a := addArtifact(t, c, p.ID, "song.mp3", "\x00\x01\x02\x03\x04\x05\x06\x07\x08")
	if a.Kind != "audio" || a.MIME != "audio/mpeg" {
		t.Errorf("kind = %q, mime = %q, want audio / audio/mpeg", a.Kind, a.MIME)
	}
}
