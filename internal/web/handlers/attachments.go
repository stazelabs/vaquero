package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// errTooLarge is an upload over core.MaxAttachment.
var errTooLarge = errors.New("upload over the attachment limit")

// attachKey is the blob key of attachment id: attachments/{instance}/{id},
// lower case.
func attachKey(instance, id string) string { return blobs.AttachmentKey(instance, id) }

// attachmentURL is the canonical address of a: /attachments/{id}/{name},
// the name escaped as one path segment.
func attachmentURL(a gen.Attachment) string {
	return "/attachments/" + a.ID + "/" + url.PathEscape(a.Name)
}

// attachmentDTO is the API's view of a: uploader is the uploader's name.
func attachmentDTO(a gen.Attachment, uploader string) api.Attachment {
	return api.Attachment{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: a.Size,
		UploadedBy: uploader, CreatedAt: a.CreatedAt, URL: attachmentURL(a)}
}

// attachmentMarkdown is the reference a ticket body or comment uses.
func attachmentMarkdown(a gen.Attachment) string { return attachmentDTO(a, "").Markdown() }

// blobDeadline bounds deleting an attachment's bytes after its row is gone.
const blobDeadline = 30 * time.Second

// deleteBytes removes an attachment's bytes after core removed its row. It
// outlives the request (a client that hangs up still gets the bytes
// removed) but not forever; a failure is logged and left to the sweep.
func deleteBytes(r *http.Request, d Deps, key string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), blobDeadline)
	defer cancel()
	if err := d.Blobs.Delete(ctx, key); err != nil {
		d.Logger.ErrorContext(r.Context(), "delete attachment bytes", "err", err, "key", key)
	}
}

// uploadedPart finds the multipart part "file" of an upload request, or
// says why there is none: not multipart (400), no such part (400), a body
// over the limit (413 with the file-limit message).
func uploadedPart(r *http.Request) (*multipart.Part, *api.Failure) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, api.Fail(http.StatusBadRequest, "bad_request", "Send the file as multipart form data, in a part named file.")
	}
	for {
		part, err := mr.NextPart()
		_, tooBig := errors.AsType[*http.MaxBytesError](err)
		switch {
		case errors.Is(err, io.EOF):
			return nil, api.Fail(http.StatusBadRequest, "bad_request", "Send the file in a part named file.")
		case tooBig:
			return nil, middleware.TooLargeUpload()
		case err != nil:
			return nil, api.Fail(http.StatusBadRequest, "bad_request", "That upload couldn't be read.")
		}
		if part.FormName() == "file" {
			return part, nil
		}
	}
}

// storeUpload stores one uploaded file on a ticket, for the web and API
// uploads alike. It reads at most core.MaxAttachment+1 bytes (errTooLarge
// past the limit), takes the type from the bytes (never the client), puts
// them under a fresh id's key, then has core write the row. When core
// refuses or fails, the bytes are deleted again; a crash in between leaves
// an orphan for the sweep.
func storeUpload(ctx context.Context, d Deps, by, ticketID string, part io.Reader, filename string) (gen.Attachment, error) {
	b, err := io.ReadAll(io.LimitReader(part, core.MaxAttachment+1))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return gen.Attachment{}, errTooLarge
		}
		return gen.Attachment{}, fmt.Errorf("read upload: %w", err)
	}
	if len(b) > core.MaxAttachment {
		return gen.Attachment{}, errTooLarge
	}
	ctype := http.DetectContentType(b)
	id := idgen.New()
	key := attachKey(d.Instance, id)
	pctx, cancel := context.WithTimeout(ctx, blobs.PutDeadline)
	defer cancel()
	if err := d.Blobs.Put(pctx, key, bytes.NewReader(b), int64(len(b)), ctype); err != nil {
		return gen.Attachment{}, fmt.Errorf("store upload: %w", err)
	}
	a, err := d.Core.AddAttachment(ctx, by, core.AttachmentInput{
		ID: id, TicketID: ticketID, Name: filename, ContentType: ctype, BlobKey: key, Size: int64(len(b)),
	})
	if err != nil {
		if derr := d.Blobs.Delete(context.WithoutCancel(ctx), key); derr != nil {
			d.Logger.ErrorContext(ctx, "delete refused upload", "err", derr, "key", key)
		}
		return gen.Attachment{}, err
	}
	return a, nil
}

// uploadFailure is the answer to a failed storeUpload, logging what the
// person isn't told.
func uploadFailure(r *http.Request, d Deps, err error) *api.Failure {
	if errors.Is(err, errTooLarge) {
		return middleware.TooLargeUpload()
	}
	f := api.FromCore(err, nil)
	if f.Status == http.StatusInternalServerError {
		d.Logger.ErrorContext(r.Context(), "upload attachment", "err", err)
	}
	return f
}

// uploadedFile is the upload's answer, for app.js to insert into the text.
type uploadedFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
	Image    bool   `json:"image"`
}

// AttachmentUpload stores the multipart part "file" on the ticket and
// answers JSON (app.js calls it with fetch), or the API failure envelope.
func AttachmentUpload(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := chi.URLParam(r, "ticket")
		k, parsed := core.ParseTicketKey(raw)
		if !parsed {
			api.WriteFailure(w, api.NotFound("That isn't a ticket key."))
			return
		}
		t, _, err := d.Core.ResolveTicket(r.Context(), d.R, k)
		if errors.Is(err, core.ErrNotFound) {
			api.WriteFailure(w, api.NotFound("No ticket "+k.String()+"."))
			return
		}
		if err != nil {
			d.Logger.ErrorContext(r.Context(), "resolve ticket", "err", err)
			api.WriteFailure(w, api.Internal())
			return
		}
		part, f := uploadedPart(r)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		a, err := storeUpload(r.Context(), d, d.Actor.ID, t.ID, part, part.FileName())
		if err != nil {
			api.WriteFailure(w, uploadFailure(r, d, err))
			return
		}
		api.WriteJSON(w, http.StatusCreated, uploadedFile{
			ID: a.ID, Name: a.Name, URL: attachmentURL(a), Markdown: attachmentMarkdown(a), Image: api.IsInlineType(a.ContentType),
		})
	}
}

// AttachmentDelete removes an attachment of this ticket, then its bytes. A
// failed byte delete is logged and left to the sweep.
func AttachmentDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		to := "/t/" + ticketKey(p, t.Number)
		a, err := d.R.GetAttachment(r.Context(), chi.URLParam(r, "id"))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			serverError(w, r, d, err)
			return
		}
		if err != nil || a.TicketID != t.ID {
			redirectWithFlash(w, r, flash.Error, "That attachment no longer exists.", to)
			return
		}
		key, err := d.Core.DeleteAttachment(r.Context(), d.Actor.ID, a.ID)
		inv, invalid := errors.AsType[*core.Invalid](err)
		switch {
		case invalid:
			redirectWithFlash(w, r, flash.Error, inv.Error(), to)
			return
		case errors.Is(err, core.ErrNotFound):
			redirectWithFlash(w, r, flash.Error, "That attachment no longer exists.", to)
			return
		case err != nil:
			serverError(w, r, d, err)
			return
		}
		deleteBytes(r, d, key)
		redirectWithFlash(w, r, flash.Success, "Attachment removed.", to)
	}
}

// contentDisposition names the file for a save: an ASCII fallback, then
// the real name as UTF-8 (RFC 6266 and 5987), every byte but the unreserved
// ones percent-encoded.
func contentDisposition(kind, name string) string {
	var ascii, star strings.Builder
	for _, c := range name {
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			ascii.WriteRune(c)
		} else {
			ascii.WriteByte('_')
		}
	}
	const hex = "0123456789ABCDEF"
	for i := range len(name) {
		c := name[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '.', c == '_', c == '~':
			star.WriteByte(c)
		default:
			star.WriteByte('%')
			star.WriteByte(hex[c>>4])
			star.WriteByte(hex[c&15])
		}
	}
	return kind + `; filename="` + ascii.String() + `"; filename*=UTF-8''` + star.String()
}

// AttachmentServe streams /attachments/{id}/{name} from the blob store. A
// name (or id case) other than the canonical one redirects to it. Only
// PNG, JPEG, GIF and WebP, by the sniffed type, are inline; the rest is
// application/octet-stream as a download. Every answer, redirects and
// misses included, is nosniff with a sandbox CSP in place of the page's.
func AttachmentServe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox")
		// r.URL.Path is decoded, so the canonical (escaped) URL compares
		// equal to the stored name and never redirects to itself.
		id, name, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/attachments/"), "/")
		a, err := d.R.GetAttachment(r.Context(), strings.ToUpper(id))
		if errors.Is(err, sql.ErrNoRows) {
			NotFound(d)(w, r)
			return
		}
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if id != a.ID || name != a.Name {
			h.Set("Location", attachmentURL(a))
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		switch err := serveBytes(w, r, d, a); {
		case errors.Is(err, blobs.ErrNotFound):
			d.Logger.ErrorContext(r.Context(), "attachment bytes missing", "attachment", a.ID, "key", a.BlobKey)
			NotFound(d)(w, r)
		case err != nil:
			serverError(w, r, d, err)
		}
	}
}

// serveBytes answers a request for a's bytes, the web and the API alike:
// ETag (an id's bytes never change, so it is the validator) with a bodiless
// 304 on a match, inline only for the sniffed image types, otherwise an
// octet-stream download named by Content-Disposition. It sets no
// nosniff or sandbox header (the caller has, before anything can fail).
// An error means nothing was written: blobs.ErrNotFound when the bytes are
// gone, anything else when the store failed.
func serveBytes(w http.ResponseWriter, r *http.Request, d Deps, a gen.Attachment) error {
	h := w.Header()
	etag := `"` + a.ID + `"`
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		h.Set("ETag", etag)
		h.Set("Cache-Control", "private, no-cache")
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	rc, size, err := d.Blobs.Get(r.Context(), a.BlobKey)
	if err != nil {
		return err
	}
	defer rc.Close()
	// A browser revalidates on every use (a deleted file stops showing) and
	// gets a bodiless 304 while it still exists.
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	ctype, kind := "application/octet-stream", "attachment"
	if api.IsInlineType(a.ContentType) {
		ctype, kind = a.ContentType, "inline"
	}
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", contentDisposition(kind, a.Name))
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}
	if _, err := io.Copy(w, rc); err != nil {
		d.Logger.WarnContext(r.Context(), "stream attachment", "err", err, "attachment", a.ID)
	}
	return nil
}

// etagMatches reports whether an If-None-Match header names etag (weak
// comparison, as RFC 9110 asks for If-None-Match) or is "*".
func etagMatches(header, etag string) bool {
	for t := range strings.SplitSeq(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}
