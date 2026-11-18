package handlers

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// render writes a page with status, attaching any queued toast. An htmx
// request leaves the toast for the next full page.
func render(w http.ResponseWriter, r *http.Request, d Deps, status int, c templ.Component) {
	ctx := r.Context()
	if r.Header.Get("HX-Request") != "true" {
		if f, ok := flash.Pop(w, r); ok {
			ctx = flash.WithCurrent(ctx, f)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(ctx, w); err != nil {
		d.Logger.ErrorContext(ctx, "render", "err", err, "path", middleware.RedactPath(r.URL.Path))
	}
}
