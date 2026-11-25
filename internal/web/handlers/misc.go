package handlers

import (
	"context"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/web/templates"
)

// Healthz pings the writer pool with a 2 s timeout: "ok", or 503 "db
// unavailable". It sits outside the session group.
func Healthz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.PingContext(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	}
}

// MethodNotAllowed sends a GET or HEAD for a POST-only path (a typed
// URL) to the page above it; other methods get a plain 405.
func MethodNotAllowed(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		up := path.Dir(strings.TrimSuffix(r.URL.Path, "/"))
		if up == "." || up == "" {
			up = "/"
		}
		http.Redirect(w, r, up, http.StatusSeeOther)
	}
}

// NotFound renders the themed 404.
// TooLarge is the themed 413 page middleware.BodyLimit shows for a web
// form body declared over the limit.
func TooLarge(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		if err := templates.TooLargePage().Render(r.Context(), w); err != nil {
			d.Logger.ErrorContext(r.Context(), "render too large", "err", err)
		}
	}
}

func NotFound(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if err := templates.NotFoundPage().Render(r.Context(), w); err != nil {
			d.Logger.ErrorContext(r.Context(), "render not found", "err", err)
		}
	}
}
