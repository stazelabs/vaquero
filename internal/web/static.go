package web

import (
	"net/http"
	"strings"

	"github.com/stazelabs/vaquero/internal/web/static"
)

// staticHandler serves the embedded static tree without directory listings
// or Go source. Pages link files with a content hash (static.URL), so an
// hour's caching never serves a stale copy after a change.
func staticHandler() http.Handler {
	files := http.FileServer(http.FS(static.FS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, ".go") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
