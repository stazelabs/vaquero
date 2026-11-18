package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/search"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// searchLimit is the most results a search shows.
const searchLimit = 50

// Search is GET /search?q=: tickets whose title, description or comments
// match. Any text is accepted; it is never parsed as query syntax.
func Search(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if len(q) > 500 {
			q = q[:500]
		}
		hits, err := search.Run(r.Context(), d.RDB, q, searchLimit)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.SearchPage(q, hits))
	}
}
