package handlers

import (
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// AdminHome is GET /admin: the system details (version, uptime, data
// directory, database and WAL sizes, schema version, whether brew services
// runs it) and the read-only Storage section (VAQ-148), with links to the
// admin pages.
func AdminHome(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		schema, err := appdb.Version(d.RDB)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		totals, err := d.R.AttachmentTotals(r.Context())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		info := templates.AdminInfo{
			Version: version.Line(), DataDir: d.Cfg.DataDir,
			DBSize: units.SizeWords(fileSize(d.Cfg.DBPath)), WALSize: units.SizeWords(fileSize(d.Cfg.DBPath + "-wal")),
			Schema: schema, Brew: d.BrewService != nil && d.BrewService(),
			Storage: storageInfo(d.Cfg, totals),
		}
		if !d.Started.IsZero() {
			info.Uptime = timefmt.Duration(d.Now().Sub(d.Started))
			info.Since = timefmt.DisplayUTC(d.Started)
		}
		render(w, r, d, http.StatusOK, templates.AdminHomePage(info))
	}
}

// fileSize is a file's size, 0 when it isn't there.
func fileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

// storageInfo is the Storage section: where attachments are kept, how many
// there are, and each storage setting's value (through config.Redact, so a
// key is never shown whole) and source.
func storageInfo(cfg config.Config, totals gen.AttachmentTotalsRow) templates.StorageInfo {
	s := templates.StorageInfo{Backend: cfg.BlobBackend, Count: totals.Count, Size: units.SizeWords(totals.Size),
		EnvFile: config.EnvFilePath(cfg.DataDir)}
	if _, err := os.Stat(s.EnvFile); err == nil {
		s.EnvFileExists = true
	}
	values := map[string]string{"VQ_BLOB_STORE": cfg.BlobBackend}
	keys := []string{"VQ_BLOB_STORE"}
	switch cfg.BlobBackend {
	case "dir":
		s.Location = cfg.BlobDir
		keys = append(keys, "VQ_BLOB_DIR")
		values["VQ_BLOB_DIR"] = cfg.BlobDir
	case "s3":
		s.Location = blobs.Location(cfg)
		keys = append(keys, "VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_REGION", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY")
		values["VQ_S3_ENDPOINT"], values["VQ_S3_BUCKET"], values["VQ_S3_REGION"] = cfg.S3Endpoint, cfg.S3Bucket, cfg.S3Region
		values["VQ_S3_ACCESS_KEY_ID"] = cfg.S3AccessKeyID
		values["VQ_S3_SECRET_ACCESS_KEY"] = string(cfg.S3SecretAccessKey)
	}
	for _, k := range keys {
		s.Settings = append(s.Settings, templates.SettingRow{Key: k, Value: config.Redact(k, values[k]), Source: cfg.Source(k)})
	}
	return s
}

func tokensPage(w http.ResponseWriter, r *http.Request, d Deps, status int, v templates.TokensView) {
	templates.LoadEventSeq(r.Context()) // before the page data is read
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
