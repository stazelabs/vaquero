// Package archive writes an instance's export: a reproducible .tar.gz of
// its tracker data and attachment bytes, which vaquero import reads back.
//
// The file is a gzip stream with no name, no time and no comment, holding a
// tar. Its entries are export.json first, then attachments/{id} (the
// attachment's id as stored, upper case) for every attachment, in id order.
// Every tar header is a regular file with mode 0644, mtime 0 (the Unix
// epoch), uid and gid 0 and no user or group name (USTAR format).
//
// export.json is one JSON object, followed by a newline, with no
// indentation. Its top-level keys are, in this order: "format"
// ("vaquero-export"), "format_version" (1), "schema" (the goose migration
// number), "exported_at" (timefmt), "vaquero_version", then one key per
// table in Tables order. Each table is an array of row objects in
// primary-key order (the declared primary-key columns in key order; rowid
// for a table without one). A row object holds every column of the table,
// keys sorted by byte order. Values are written as SQLite holds them:
// NULL as null, TEXT as a JSON string (HTML characters not escaped; invalid
// UTF-8 is refused rather than altered), INTEGER as a JSON integer with no
// point or exponent, and REAL as Go's shortest round-trip form
// (strconv 'g', -1) with ".0" added when that form has neither a point nor
// an exponent. So a reader decoding with json.Decoder.UseNumber knows every
// value's type: a number with '.', 'e' or 'E' is a float64, any other is an
// int64. The only REAL column today is tickets.position. Exported tables
// have no BLOB columns, and Write refuses one. events.meta (an event's
// provenance, a JSON object as text) is exported like any TEXT column; an
// export from before it loads with {}. plans and plan_revisions (0009)
// are exported whole, every revision's text included; an export from
// before them has neither key and loads with no plans. decisions,
// decision_tickets and approvals (0009) are exported whole too, the owner's
// relayed words included; an export from before them loads with none.
// project_icons (0010) is exported whole, each image as the base64 text it
// is stored as (icons never live in the blob store, so they add no entry);
// an export from before it loads with every project on its monogram.
// ticket_old_keys (0011) is exported whole, so a moved ticket's old keys
// keep leading to it after an import; an export from before it loads with
// none. project_links (0011, VAQ-119) is exported whole; an export from
// before it loads with no project links.
//
// Left out: api_tokens, mcp_calls, blobs (its bytes are the attachments/
// entries, whatever the backend), the FTS tables, goose's table, and every
// settings row but those in ExportedSettings (an allow-list; a key in
// neither ExportedSettings nor InstanceSettings makes Write fail). Tickets' claim columns (claimed_by,
// claim_expires_at, claim_prev_status_id) are written as null. An
// attachment's blob_key is written as its entry's name, attachments/{id},
// because the stored key names this instance: a loader stores the bytes
// under its own instance's key (blobs.AttachmentKey) and records that.
//
// Loading order: Tables lists parents before children, so a loader can
// insert in that order with foreign keys on, except that tickets.parent_id
// refers to tickets and is not deferrable, while tickets come in id order:
// a loader must set PRAGMA defer_foreign_keys = ON inside its transaction
// (or insert each parent before its children).
package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// Format and FormatVersion are the header's "format" and "format_version".
const (
	Format        = "vaquero-export"
	FormatVersion = 1
)

// Tables are the exported tables, parents before children.
var Tables = []string{
	"settings", "actors", "projects", "project_links", "statuses", "milestones", "tags",
	"tickets", "ticket_old_keys", "ticket_tags", "comments", "ticket_links", "attachments",
	"plans", "plan_revisions", "decisions", "decision_tickets", "approvals", "project_icons", "events",
}

// Header is export.json's first fields.
type Header struct {
	Format         string
	FormatVersion  int
	Schema         int64
	ExportedAt     string
	VaqueroVersion string
}

// NewHeader is the header of an export of schema taken at now by version.
func NewHeader(schema int64, now time.Time, version string) Header {
	return Header{Format: Format, FormatVersion: FormatVersion, Schema: schema, ExportedAt: timefmt.Format(now), VaqueroVersion: version}
}

// FileError is an attachment whose bytes can't be exported: missing from
// the store, or a different size there from the one its record says.
type FileError struct {
	AttachmentID, TicketKey string
	Missing                 bool  // absent from the store
	Size, Stored            int64 // the record's size and the store's, when present
}

func (e *FileError) Error() string {
	what := "The file for attachment " + e.AttachmentID + " on " + e.TicketKey
	if e.Missing {
		return what + " is missing from storage."
	}
	return fmt.Sprintf("%s is %d bytes in storage, but its record says %d.", what, e.Stored, e.Size)
}

// check refuses a missing or wrongly sized object, asking the store for its
// size only.
func (a attachment) check(ctx context.Context, store blobs.Store) error {
	size, err := store.Stat(ctx, a.key)
	if errors.Is(err, blobs.ErrNotFound) {
		return &FileError{AttachmentID: a.id, TicketKey: a.ticket, Missing: true, Size: a.size}
	}
	if err != nil {
		return fmt.Errorf("check attachment %s: %w", a.id, err)
	}
	if size != a.size {
		return &FileError{AttachmentID: a.id, TicketKey: a.ticket, Size: a.size, Stored: size}
	}
	return nil
}

// open gets a's bytes, refusing a missing or wrongly sized object.
func (a attachment) open(ctx context.Context, store blobs.Store) (io.ReadCloser, error) {
	rc, size, err := store.Get(ctx, a.key)
	if errors.Is(err, blobs.ErrNotFound) {
		return nil, &FileError{AttachmentID: a.id, TicketKey: a.ticket, Missing: true, Size: a.size}
	}
	if err != nil {
		return nil, fmt.Errorf("read attachment %s: %w", a.id, err)
	}
	if size != a.size {
		rc.Close()
		return nil, &FileError{AttachmentID: a.id, TicketKey: a.ticket, Size: a.size, Stored: size}
	}
	return rc, nil
}

// attachment is one attachments/ entry to write.
type attachment struct {
	id, key, ticket string
	size            int64
}

// Check makes Write's checks without keeping export.json or reading any
// attachment: the settings keys and table columns Write would refuse,
// export.json's size (counted row by row as Write would write it, *TooLarge
// past MaxDocument), and every attachment's size in the store (a Stat
// each). It also refuses what Write can't encode (text that isn't UTF-8, a
// REAL with no JSON form), which core never stores.
// h is the header Write would be given, so the count is the one Write
// would make.
func Check(ctx context.Context, db *sql.DB, store blobs.Store, h Header) error {
	_, _, err := prepare(ctx, db, store, h, false)
	return err
}

// prepare reads the attachment list and export.json (kept only when keep
// is set), then checks every attachment's bytes are in store at their
// recorded size.
func prepare(ctx context.Context, db *sql.DB, store blobs.Store, h Header, keep bool) ([]byte, []attachment, error) {
	doc, atts, err := snapshot(ctx, db, h, keep)
	if err != nil {
		return nil, nil, err
	}
	for _, a := range atts {
		if err := a.check(ctx, store); err != nil {
			return nil, nil, err
		}
	}
	return doc, atts, nil
}

// Write writes the export of db, with attachment bytes from store, to w.
// Every table is read in one read transaction, so all see one snapshot.
// export.json is built whole first: past MaxDocument it is *TooLarge (the
// building stops there) and w gets nothing. Every attachment's size is
// checked (Stat) before the first byte reaches w: a missing or wrongly
// sized one is *FileError and w gets nothing. Each
// is then read once, as it is copied; one that vanishes or changes by then
// is an error too, and the caller must treat what reached w as broken.
func Write(ctx context.Context, w io.Writer, db *sql.DB, store blobs.Store, h Header) error {
	doc, atts, err := prepare(ctx, db, store, h, true)
	if err != nil {
		return err
	}

	zw, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(fileHeader("export.json", int64(len(doc)))); err != nil {
		return err
	}
	if _, err := tw.Write(doc); err != nil {
		return err
	}
	for _, a := range atts {
		if err := writeAttachment(ctx, tw, store, a); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

// writeAttachment copies one attachment's bytes into the archive.
func writeAttachment(ctx context.Context, tw *tar.Writer, store blobs.Store, a attachment) error {
	rc, err := a.open(ctx, store)
	if err != nil {
		return err
	}
	defer rc.Close()
	size := a.size
	if err := tw.WriteHeader(fileHeader("attachments/"+a.id, size)); err != nil {
		return err
	}
	if n, err := io.Copy(tw, rc); err != nil {
		return fmt.Errorf("read attachment %s: %w", a.id, err)
	} else if n != size {
		return fmt.Errorf("attachment %s on %s: read %d of %d bytes", a.id, a.ticket, n, size)
	}
	return nil
}

// fileHeader is a tar header with nothing in it that varies.
func fileHeader(name string, size int64) *tar.Header {
	return &tar.Header{
		Typeflag: tar.TypeReg, Name: name, Size: size, Mode: 0o644,
		ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR,
	}
}

// snapshot reads export.json (returned only with keep; otherwise only
// measured) and the attachment list in one read transaction.
func snapshot(ctx context.Context, db *sql.DB, h Header, keep bool) ([]byte, []attachment, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var buf bytes.Buffer
	if err := dump(ctx, tx, &buf, h, &docSize{keep: keep}); err != nil {
		return nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.blob_key, a.size, p.key || '-' || t.number
		FROM attachments a
		JOIN tickets t ON t.id = a.ticket_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY a.id`)
	if err != nil {
		return nil, nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	var atts []attachment
	for rows.Next() {
		var a attachment
		if err := rows.Scan(&a.id, &a.key, &a.size, &a.ticket); err != nil {
			return nil, nil, err
		}
		atts = append(atts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if !keep {
		buf = bytes.Buffer{}
	}
	return buf.Bytes(), atts, tx.Commit()
}
