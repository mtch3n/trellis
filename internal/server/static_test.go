package server

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/web"
)

func TestWeb_WEB_C10_ServesTheAppAtRootAndDeepLinks(t *testing.T) {
	e := webSetup(t)
	index, err := fs.ReadFile(web.DistFS, "dist/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/p/APP/vault"} {
		resp, err := http.Get(e.url + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("GET %s Content-Type = %q, want text/html", path, resp.Header.Get("Content-Type"))
		}
		if string(body) != string(index) {
			t.Fatalf("GET %s did not answer the embedded index.html", path)
		}
	}

	resp, err := http.Get(e.url + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/me without a session = %d, want 401", resp.StatusCode)
	}
}
