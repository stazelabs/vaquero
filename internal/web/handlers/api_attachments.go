package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// ticketAttachments is a ticket's files, oldest first, never nil.
func ticketAttachments(r *http.Request, d Deps, ticketID string) ([]api.Attachment, error) {
	rows, err := d.R.ListTicketAttachments(r.Context(), ticketID)
	if err != nil {
		return nil, err
	}
	out := make([]api.Attachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, attachmentDTO(gen.Attachment{
			ID: row.ID, TicketID: row.TicketID, UploadedBy: row.UploadedBy, Name: row.Name,
			ContentType: row.ContentType, Size: row.Size, BlobKey: row.BlobKey, CreatedAt: row.CreatedAt,
		}, row.UploaderName))
	}
	return out, nil
}

// APIAttachmentList is GET /api/v1/tickets/{ticket}/attachments.
func APIAttachmentList(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		out, err := ticketAttachments(r, d, t.ID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIAttachmentUpload is POST /api/v1/tickets/{ticket}/attachments: the
// file is the multipart part "file".
func APIAttachmentUpload(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, _, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		part, f := uploadedPart(r)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		by := apiActor(r)
		a, err := storeUpload(r.Context(), d, by.ID, t.ID, part, part.FileName())
		if err != nil {
			api.WriteFailure(w, uploadFailure(r, d, err))
			return
		}
		out := attachmentDTO(a, core.ActorName(by))
		if _, _, out.Warning, err = ticketClaim(r, d, t); err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, out)
	}
}

// apiAttachment loads the {id} attachment, any case.
func apiAttachment(w http.ResponseWriter, r *http.Request, d Deps) (gen.Attachment, bool) {
	a, err := d.R.GetAttachment(r.Context(), strings.ToUpper(chi.URLParam(r, "id")))
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteFailure(w, api.NotFound("No such attachment."))
		return a, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return a, false
	}
	return a, true
}

// APIAttachmentGet is GET /api/v1/attachments/{id}: the bytes, answered
// like the page's /attachments/{id}/{name} (same headers, ETag and 304).
func APIAttachmentGet(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox")
		a, ok := apiAttachment(w, r, d)
		if !ok {
			return
		}
		switch err := serveBytes(w, r, d, a); {
		case errors.Is(err, blobs.ErrNotFound):
			d.Logger.ErrorContext(r.Context(), "attachment bytes missing", "attachment", a.ID, "key", a.BlobKey)
			api.WriteFailure(w, api.NotFound("The bytes of that attachment are missing."))
		case err != nil:
			apiFail(w, r, d, api.Internal(), err)
		}
	}
}

// APIAttachmentDelete is DELETE /api/v1/attachments/{id}: the uploader or
// the owner removes it.
func APIAttachmentDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, ok := apiAttachment(w, r, d)
		if !ok {
			return
		}
		key, err := d.Core.DeleteAttachment(r.Context(), apiActor(r).ID, a.ID)
		if err != nil {
			apiCoreFail(w, r, d, gen.Project{}, err)
			return
		}
		deleteBytes(r, d, key, a.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}
