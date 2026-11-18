package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// AdminHome is GET /admin: the instance's version, data directory,
// database size and schema version, and links to the admin pages.
func AdminHome(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		schema, err := appdb.Version(d.RDB)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		var size int64
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(d.Cfg.DBPath + suffix); err == nil {
				size += fi.Size()
			}
		}
		render(w, r, d, http.StatusOK, templates.AdminHomePage(templates.AdminInfo{
			Version: version.Line(), DataDir: d.Cfg.DataDir, DBSize: humanBytes(size), Schema: schema,
		}))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func tokensPage(w http.ResponseWriter, r *http.Request, d Deps, status int, v templates.TokensView) {
	toks, err := d.R.ListTokens(r.Context())
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	v.Tokens, v.Now = toks, d.Now()
	render(w, r, d, status, templates.TokensPage(v))
}

// AdminTokens is GET /admin/tokens.
func AdminTokens(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokensPage(w, r, d, http.StatusOK, templates.TokensView{})
	}
}

// AdminTokenCreate is POST /admin/tokens: the new token is shown on this
// response only (no redirect, so it never sits in a URL, cookie or log).
func AdminTokenCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PostFormValue("name")
		raw, _, err := d.Core.CreateToken(r.Context(), d.Actor.ID, d.Actor.ID, name)
		var inv *core.Invalid
		switch {
		case errors.As(err, &inv):
			tokensPage(w, r, d, http.StatusUnprocessableEntity, templates.TokensView{Name: name, Error: inv.Fields["name"]})
			return
		case err != nil:
			serverError(w, r, d, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		tokensPage(w, r, d, http.StatusCreated, templates.TokensView{Created: raw})
	}
}

// AdminTokenRevoke is POST /admin/tokens/{id}/revoke.
func AdminTokenRevoke(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := d.Core.RevokeToken(r.Context(), d.Actor.ID, chi.URLParam(r, "id"))
		var inv *core.Invalid
		switch {
		case err == nil:
			renewLocalToken(r, d)
			redirectWithFlash(w, r, flash.Success, "Token revoked.", "/admin/tokens")
		case errors.As(err, &inv):
			redirectWithFlash(w, r, flash.Error, inv.Error(), "/admin/tokens")
		case errors.Is(err, core.ErrNotFound):
			redirectWithFlash(w, r, flash.Error, "That token no longer exists.", "/admin/tokens")
		default:
			serverError(w, r, d, err)
		}
	}
}

// renewLocalToken writes a new local token if the revoked one was it, so
// the CLI keeps working.
func renewLocalToken(r *http.Request, d Deps) {
	if d.RenewLocalToken == nil {
		return
	}
	if err := d.RenewLocalToken(r.Context()); err != nil {
		d.Logger.ErrorContext(r.Context(), "renew local token", "err", err)
	}
}
