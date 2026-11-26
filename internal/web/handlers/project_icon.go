package handlers

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// projectIconURL is the address of a project's uploaded icon image.
func projectIconURL(key string) string { return "/p/" + key + "/icon" }

// errIconTooLarge is an icon image over core.MaxIcon, or a body past the
// route's limit.
var errIconTooLarge = errors.New("icon over the limit")

// readIconFile reads an uploaded icon file: at most core.MaxIcon bytes,
// errIconTooLarge past it.
func readIconFile(part io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(part, core.MaxIcon+1))
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return nil, errIconTooLarge
	}
	if err != nil {
		return nil, err
	}
	if len(b) > core.MaxIcon {
		return nil, errIconTooLarge
	}
	return b, nil
}

// iconForm is the settings page's icon form: the chosen radio ("monogram",
// "glyph:NAME" or "image") and the chosen file, if any.
type iconForm struct {
	choice  string
	file    []byte
	hasFile bool
}

// readIconForm reads the multipart icon form part by part, so the file is
// never held past core.MaxIcon+1 bytes.
func readIconForm(r *http.Request) (iconForm, error) {
	var f iconForm
	mr, err := r.MultipartReader()
	if err != nil {
		return f, err
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return f, nil
		}
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return f, errIconTooLarge
		}
		if err != nil {
			return f, err
		}
		switch part.FormName() {
		case "icon":
			b, err := io.ReadAll(io.LimitReader(part, 200))
			if err != nil {
				return f, err
			}
			f.choice = string(b)
		case "file":
			b, err := readIconFile(part)
			if err != nil {
				return f, err
			}
			// A form with no file chosen still sends an empty part.
			if part.FileName() != "" || len(b) > 0 {
				f.file, f.hasFile = b, true
			}
		}
	}
}

// ProjectIconSave is POST /p/{key}/icon, the settings page's icon form: a
// chosen file makes the icon that image, else the chosen radio applies. It
// returns to settings (or with ?return=/ the projects page, for quick
// settings) with a toast either way.
func ProjectIconSave(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f, err := readIconForm(r)
		switch {
		case errors.Is(err, errIconTooLarge):
			redirectWithFlash(w, r, flash.Error, middleware.IconTooLargeMessage, returnPath(r, p))
			return
		case err != nil:
			redirectWithFlash(w, r, flash.Error, "That upload couldn't be read.", returnPath(r, p))
			return
		}
		kind, glyph, ok := strings.Cut(f.choice, ":")
		if !ok {
			glyph = ""
		}
		var img *core.IconImage
		if f.hasFile {
			kind, glyph, img = core.IconKindImage, "", &core.IconImage{Data: f.file}
		}
		settingsAction(w, r, d, p, "Icon saved.", func() error {
			_, err := d.Core.SetProjectIcon(r.Context(), d.Actor.ID, p.ID, kind, glyph, img)
			return err
		})
	}
}

// APIProjectIconUpload is PUT /api/v1/projects/{key}/icon: the multipart
// part "file" becomes the project's icon image. It answers the project.
func APIProjectIconUpload(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		part, fail := uploadedPart(r)
		if fail != nil {
			if fail.Status == http.StatusRequestEntityTooLarge {
				fail = middleware.TooLargeIcon()
			}
			api.WriteFailure(w, fail)
			return
		}
		b, err := readIconFile(part)
		switch {
		case errors.Is(err, errIconTooLarge):
			api.WriteFailure(w, middleware.TooLargeIcon())
			return
		case err != nil:
			api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", "That upload couldn't be read."))
			return
		}
		np, err := d.Core.SetProjectIcon(r.Context(), apiActor(r).ID, p.ID, core.IconKindImage, "", &core.IconImage{Type: part.Header.Get("Content-Type"), Data: b})
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeProject(w, r, d, np, http.StatusOK)
	}
}

// ProjectIconRemove is POST /p/{key}/icon/delete, the settings page's
// Remove image (VAQ-146): the stored image goes, and an icon showing it
// falls back to the monogram.
func ProjectIconRemove(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		settingsAction(w, r, d, p, "Image removed.", func() error {
			_, err := d.Core.RemoveProjectIconImage(r.Context(), d.Actor.ID, p.ID)
			return err
		})
	}
}

// APIProjectIconDelete is DELETE /api/v1/projects/{key}/icon: the stored
// icon image is removed (an icon showing it falls back to the monogram).
// With none stored nothing changes. It answers the project.
func APIProjectIconDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		np, err := d.Core.RemoveProjectIconImage(r.Context(), apiActor(r).ID, p.ID)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeProject(w, r, d, np, http.StatusOK)
	}
}

// ProjectIconChoices is GET /p/{key}/icon/choices: the projects page's
// quick settings icon form alone, fetched when its part first opens.
func ProjectIconChoices(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		hasImage := true
		if _, err := d.R.GetProjectIconEtag(r.Context(), p.ID); errors.Is(err, sql.ErrNoRows) {
			hasImage = false
		} else if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.QuickIconChoices(templates.IconOf(p), hasImage))
	}
}

// ProjectIconServe is GET /p/{key}/icon: the project's stored image, with
// its etag (private, no-cache: a browser revalidates on every use and gets
// a 304 while it is unchanged), nosniff and a sandbox CSP, like an
// attachment. A project with no stored image is a 404.
func ProjectIconServe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		icon, err := d.R.GetProjectIcon(r.Context(), p.ID)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		serveIcon(w, r, d, icon)
	}
}

// serveIcon answers icon's bytes, or a 304 when the browser has them.
// Stored data that doesn't decode is logged and answered 404, like a
// missing image: the page shows a broken image, not an error.
func serveIcon(w http.ResponseWriter, r *http.Request, d Deps, icon gen.ProjectIcon) {
	b, err := base64.StdEncoding.DecodeString(icon.Data)
	if err != nil {
		d.Logger.WarnContext(r.Context(), "undecodable project icon", "project", icon.ProjectID, "err", err)
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	etag := `"` + icon.Etag + `"`
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", icon.Type)
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}
