package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// MaxAttachment is the largest file a ticket can carry, in bytes.
const MaxAttachment = 10 << 20

// maxNameRunes is the longest stored file name, in characters.
const maxNameRunes = 200

// settingBlobStore records which backend holds the attachment bytes.
const settingBlobStore = "blob_store"

// AttachmentInput describes a file whose bytes are already in the blob
// store under BlobKey. ID is the id the caller minted for the key (the key
// names the attachment); empty mints one.
type AttachmentInput struct {
	ID, TicketID, Name, ContentType, BlobKey string
	Size                                     int64
}

// bidiOverride reports the invisible characters that reorder or hide text:
// directional marks, embeddings, overrides and isolates.
func bidiOverride(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x200E || r == 0x200F || r == 0x061C
}

// CleanName turns an uploaded file name into one safe to store and show: the
// base name after any / or \, without control or bidi-override characters,
// trimmed of spaces and dots, and at most 200 characters (cut on a character
// boundary, keeping the extension). An empty result is "file".
func CleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || bidiOverride(r) {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if utf8.RuneCountInString(name) > maxNameRunes {
		rs := []rune(name)
		ext := []rune{}
		if i := strings.LastIndex(name, "."); i > 0 {
			ext = []rune(name[i:])
			if len(ext) > 20 {
				ext = nil
			}
		}
		stem := strings.TrimRight(string(rs[:maxNameRunes-len(ext)]), " .")
		if len(ext) > 0 {
			// The stem is cut where it ends; re-trim before the extension.
			name = stem + string(ext)
		} else {
			name = stem
		}
	}
	if name == "" {
		return "file"
	}
	return name
}

// AddAttachment records a file on a ticket. The caller has already stored
// the bytes under in.BlobKey. The first attachment also records the backend
// (Core.BlobStore) in settings, so a later change of backend is caught.
func (c *Core) AddAttachment(ctx context.Context, by string, in AttachmentInput) (gen.Attachment, error) {
	var a gen.Attachment
	err := c.tx(ctx, func(q *gen.Queries) error {
		t, err := q.GetTicket(ctx, in.TicketID)
		if err != nil {
			return notFound(err, "get ticket")
		}
		f := fields{}
		name := CleanName(in.Name)
		switch {
		case in.Size < 1:
			f.add("file", "That file is empty.")
		case in.Size > MaxAttachment:
			f.add("file", "Files can be at most 10 MB.")
		}
		if in.BlobKey == "" {
			f.add("file", "That file was not stored.")
		}
		if err := f.err(); err != nil {
			return err
		}
		ctype := strings.TrimSpace(in.ContentType)
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		now := timefmt.Format(c.Now())
		id := in.ID
		if id == "" {
			id = idgen.New()
		}
		a, err = q.CreateAttachment(ctx, gen.CreateAttachmentParams{
			ID: id, TicketID: t.ID, UploadedBy: by, Name: name,
			ContentType: ctype, Size: in.Size, BlobKey: in.BlobKey, CreatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("create attachment: %w", err)
		}
		if c.BlobStore != "" {
			if _, err := q.GetSetting(ctx, settingBlobStore); errors.Is(err, sql.ErrNoRows) {
				if err := q.PutSetting(ctx, gen.PutSettingParams{Key: settingBlobStore, Value: c.BlobStore, UpdatedAt: now}); err != nil {
					return fmt.Errorf("record blob store: %w", err)
				}
			} else if err != nil {
				return fmt.Errorf("get blob store: %w", err)
			}
		}
		return c.emit(ctx, q, by, EvAttachmentAdded, t.ProjectID, t.ID, map[string]string{"attachment_id": a.ID, "name": a.Name})
	})
	return a, err
}

// CanDeleteAttachment reports whether actor may remove a: its uploader or
// the owner.
func CanDeleteAttachment(actor gen.Actor, a gen.Attachment) bool {
	return a.UploadedBy == actor.ID || IsOwner(actor)
}

// DeleteAttachment removes an attachment's row and returns its blob key, so
// the caller can delete the bytes. Only the uploader or the owner may.
func (c *Core) DeleteAttachment(ctx context.Context, by, id string) (string, error) {
	var key string
	err := c.tx(ctx, func(q *gen.Queries) error {
		a, err := q.GetAttachment(ctx, id)
		if err != nil {
			return notFound(err, "get attachment")
		}
		actor, err := q.GetActor(ctx, by)
		if err != nil {
			return notFound(err, "get actor")
		}
		if !CanDeleteAttachment(actor, a) {
			return invalid("attachment", "You can only remove your own attachments.")
		}
		t, err := q.GetTicket(ctx, a.TicketID)
		if err != nil {
			return fmt.Errorf("get ticket: %w", err)
		}
		if err := q.DeleteAttachment(ctx, a.ID); err != nil {
			return fmt.Errorf("delete attachment: %w", err)
		}
		key = a.BlobKey
		return c.emit(ctx, q, by, EvAttachmentDeleted, t.ProjectID, t.ID, map[string]string{"attachment_id": a.ID, "name": a.Name})
	})
	if err != nil {
		return "", err
	}
	return key, nil
}
