package handlers

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/templates"
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

// renderPart renders a fragment of a page (one htmx or app.js reads into
// a page already shown). Unlike render it never pops the toast: a queued
// toast belongs to the next full page.
func renderPart(w http.ResponseWriter, r *http.Request, d Deps, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		d.Logger.ErrorContext(r.Context(), "render", "err", err, "path", middleware.RedactPath(r.URL.Path))
	}
}

// serverError logs err and answers with the themed 500 page.
func serverError(w http.ResponseWriter, r *http.Request, d Deps, err error) {
	d.Logger.ErrorContext(r.Context(), "handler error", "err", err, "path", middleware.RedactPath(r.URL.Path),
		"request_id", middleware.GetRequestID(r.Context()))
	render(w, r, d, http.StatusInternalServerError, templates.ErrorPage())
}

// redirectWithFlash queues a toast and answers 303 to to.
func redirectWithFlash(w http.ResponseWriter, r *http.Request, kind flash.Kind, msg, to string) {
	flash.Put(w, kind, msg)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// invalidOrError answers a failed core call. A refused input calls
// rerender with its field messages (the caller renders the form with 422);
// a missing thing goes home with a toast; anything else is a 500.
func invalidOrError(w http.ResponseWriter, r *http.Request, d Deps, err error, rerender func(fields map[string]string)) {
	var inv *core.Invalid
	switch {
	case errors.As(err, &inv):
		templates.LoadEventSeq(r.Context()) // before rerender reads the page again
		rerender(inv.Fields)
	case errors.Is(err, core.ErrNotFound):
		redirectWithFlash(w, r, flash.Error, "That no longer exists.", "/")
	default:
		serverError(w, r, d, err)
	}
}
