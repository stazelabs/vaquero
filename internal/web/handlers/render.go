package handlers

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// render writes a page with status.
func render(w http.ResponseWriter, r *http.Request, d Deps, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		d.Logger.ErrorContext(r.Context(), "render", "err", err, "path", middleware.RedactPath(r.URL.Path))
	}
}
