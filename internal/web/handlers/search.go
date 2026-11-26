package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/search"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// searchLimit is the most results a search shows.
const searchLimit = 50

// helpLimit is the most help articles a search shows.
const helpLimit = 5

// maxQuery is the most characters of a search query that count.
const maxQuery = 500

// Search is GET /search?q=: help articles whose title, summary or headings
// match, then tickets whose title, description or comments match. Any text
// is accepted; it is never parsed as query syntax.
func Search(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := units.Clip(r.URL.Query().Get("q"), maxQuery)
		res, err := search.Run(r.Context(), d.RDB, q, searchLimit)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.SearchPage(q, help.Search(q, helpLimit), res))
	}
}
