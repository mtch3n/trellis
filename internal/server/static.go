package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/mtch3n/trellis/internal/platform"
	"github.com/mtch3n/trellis/web"
)

func registerApp(mux *http.ServeMux) {
	dist, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(dist)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, platform.ErrNotFound("no_route", "no API route "+r.Method+" "+r.URL.Path, "see the routes in specs/web/web"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFileFS(w, r, dist, "index.html")
	})
}
