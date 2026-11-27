package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/units"
)

// Refused is an archive Read or Load won't take, with a sentence saying
// why, for the person importing it.
type Refused struct{ msg string }

func (e *Refused) Error() string { return e.msg }

func refuse(format string, a ...any) error { return &Refused{fmt.Sprintf(format, a...)} }

// MaxTrailing is the most data Read reads past the tar's end, inside the
// gzip stream, on its way to the checksum: room for a tar program's padding,
// and a bound on the work a crafted file can make.
const MaxTrailing = 1 << 20

// MaxDocument is the largest export.json Read takes, and so the largest
// Write and Check let an export hold (VAQ-132): an archive this version
// writes, it can import. Room for every real instance, and a bound on the
// memory a crafted file can claim. A variable only so tests can lower it:
// set only in tests, never in a t.Parallel test; nothing else writes it.
var MaxDocument int64 = 256 << 20

// TooLarge is an export whose export.json would pass MaxDocument: Write
// and Check refuse it before anything is written.
type TooLarge struct{}

func (*TooLarge) Error() string {
	return fmt.Sprintf("This instance's export.json would be larger than %d MB, more than vaquero import reads.", MaxDocument>>20)
}

var errNotExport = &Refused{"This file isn't a Vaquero export."}

// SpoolPrefix starts the name of the directory Read spools attachment bytes
// into. An importer holding its data directory's lock may remove any such
// directory there: one an interrupted import left.
const SpoolPrefix = "import-spool-"

// freeBytes is the free-space reading, replaceable by a test.
var freeBytes = appdb.FreeBytes

// Reader is an export read back: its header, its tables (each row a map of
// column to value: nil, int64, float64 or string), and its attachment bytes,
// spooled to a directory of its own that Close removes.
type Reader struct {
	Header Header
	Tables map[string][]map[string]any
	dir    string
	files  map[string]spooled // attachment id → its bytes on disk
}

type spooled struct {
	path string
	size int64
}

// Blob opens attachment id's bytes; the caller closes them.
func (rd *Reader) Blob(id string) (io.ReadCloser, int64, bool) {
	s, ok := rd.files[id]
	if !ok {
		return nil, 0, false
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, 0, false
	}
	return f, s.size, true
}

// SpoolDir is the directory holding the attachment bytes.
func (rd *Reader) SpoolDir() string { return rd.dir }

// Close removes the spool directory.
func (rd *Reader) Close() error {
	if rd.dir == "" {
		return nil
	}
	err := os.RemoveAll(rd.dir)
	rd.dir = ""
	return err
}

// Read reads an export written by Write, spooling the attachment bytes into a
// new directory under dir (SpoolPrefix and a random suffix), removed again
// when Read fails or the Reader closes. Before spooling it checks that dir's
// filesystem has room for copies copies of every attachment (the spool, and
// the store when it shares the volume) and for export.json's size again,
// for the rows it becomes. It refuses, with a *Refused, a file
// that isn't an export, an export.json over MaxDocument, a format version
// other than FormatVersion, a schema newer than this build's, a table this
// build doesn't know, an attachment whose id isn't a ULID (or equals
// another's in lower case, as blob keys are) or whose size is over
// core.MaxAttachment, an attachment record without its file, a file without
// its record or of another size than its record says, any other entry, more
// than MaxTrailing bytes after the tar's end, and a stream whose gzip
// checksum fails (read to the end to check it).
func Read(r io.Reader, dir string, copies int) (_ *Reader, err error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, errNotExport
	}
	tr := tar.NewReader(zr)
	h, err := tr.Next()
	if err != nil || h.Name != "export.json" || h.Typeflag != tar.TypeReg {
		return nil, errNotExport
	}
	if h.Size > MaxDocument {
		return nil, refuse("The export's export.json is larger than %d MB, more than this vaquero reads.", MaxDocument>>20)
	}
	rd := &Reader{files: map[string]spooled{}}
	defer func() {
		if err != nil {
			rd.Close()
		}
	}()
	if err := rd.readDoc(io.LimitReader(tr, MaxDocument)); err != nil {
		return nil, err
	}
	want := map[string]int64{} // attachment id → size
	var total int64
	lower := map[string]bool{} // blob keys are lower case, so ids must differ in it
	for i, row := range rd.Tables["attachments"] {
		id, ok1 := row["id"].(string)
		size, ok2 := row["size"].(int64)
		if !ok1 || id == "" || !ok2 || size < 0 {
			return nil, refuse("The export's attachment record %d has no id or size.", i+1)
		}
		if !idgen.Valid(id) {
			return nil, refuse("The export's attachment record %d has an id that isn't one Vaquero makes.", i+1)
		}
		if lower[strings.ToLower(id)] {
			return nil, refuse("The export holds attachment %s twice.", id)
		}
		lower[strings.ToLower(id)] = true
		if size > core.MaxAttachment {
			return nil, refuse("The export's attachment %s is %d bytes; attachments can be at most %d MB.", id, size, core.MaxAttachment>>20)
		}
		want[id] = size
		total += size
	}
	free, err := freeBytes(dir)
	if err != nil {
		return nil, fmt.Errorf("free space in %s: %w", dir, err)
	}
	if need := total*int64(max(copies, 1)) + h.Size; free < need {
		return nil, refuse("Importing the export needs %s free in %s, and %s is free.", units.SizeWords(need), dir, units.SizeWords(free))
	}
	if rd.dir, err = os.MkdirTemp(dir, SpoolPrefix); err != nil {
		return nil, err
	}
	for n := 0; ; n++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, refuse("The export is damaged (%v).", err)
		}
		id, ok := strings.CutPrefix(h.Name, "attachments/")
		if !ok || h.Typeflag != tar.TypeReg || id == "" {
			return nil, refuse("The export holds %s, which isn't part of an export.", h.Name)
		}
		if _, seen := rd.files[id]; seen {
			return nil, refuse("The export holds %s twice.", h.Name)
		}
		size, ok := want[id]
		if !ok {
			return nil, refuse("The export holds %s, but no attachment record names it.", h.Name)
		}
		if h.Size != size {
			return nil, refuse("The export's file for attachment %s is %d bytes, but its record says %d.", id, h.Size, size)
		}
		p := filepath.Join(rd.dir, strconv.Itoa(n))
		if err := spool(p, tr, size); err != nil {
			return nil, refuse("The export is damaged (%v).", err)
		}
		rd.files[id] = spooled{p, size}
	}
	// The rest of the stream holds gzip's checksum, which only reading to
	// the end verifies.
	if n, err := io.Copy(io.Discard, io.LimitReader(zr, MaxTrailing+1)); err != nil {
		return nil, refuse("The export is damaged (%v).", err)
	} else if n > MaxTrailing {
		return nil, refuse("The export has more than %d MB of data after its last file.", MaxTrailing>>20)
	}
	if err := zr.Close(); err != nil {
		return nil, refuse("The export is damaged (%v).", err)
	}
	for _, row := range rd.Tables["attachments"] {
		if id := row["id"].(string); rd.files[id].path == "" {
			return nil, refuse("The export has no file for attachment %s.", id)
		}
	}
	return rd, nil
}

// spool copies size bytes from r to a new file at path.
func spool(path string, r io.Reader, size int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, r, size); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readDoc decodes export.json into rd.Header and rd.Tables.
func (rd *Reader) readDoc(r io.Reader) error {
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return errNotExport
	}
	var h struct {
		Format         string `json:"format"`
		FormatVersion  int    `json:"format_version"`
		Schema         int64  `json:"schema"`
		ExportedAt     string `json:"exported_at"`
		VaqueroVersion string `json:"vaquero_version"`
	}
	fields := map[string]any{
		"format": &h.Format, "format_version": &h.FormatVersion, "schema": &h.Schema,
		"exported_at": &h.ExportedAt, "vaquero_version": &h.VaqueroVersion,
	}
	for k, dst := range fields {
		raw, ok := doc[k]
		if !ok || json.Unmarshal(raw, dst) != nil {
			return errNotExport
		}
		delete(doc, k)
	}
	if h.Format != Format || h.Schema < 1 {
		return errNotExport
	}
	if h.FormatVersion != FormatVersion {
		return refuse("This export is format version %d, which this vaquero can't read. Upgrade vaquero and import again.", h.FormatVersion)
	}
	newest, err := appdb.Newest()
	if err != nil {
		return err
	}
	if h.Schema > newest {
		return refuse("This export is from a newer vaquero (schema %d). Upgrade vaquero and import again.", h.Schema)
	}
	rd.Header = Header(h)
	names := make([]string, 0, len(doc))
	for k := range doc {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		if !slices.Contains(Tables, k) {
			return refuse("The export holds a table called %s, which this vaquero doesn't know.", k)
		}
	}
	rd.Tables = map[string][]map[string]any{}
	for _, t := range Tables {
		raw, ok := doc[t]
		if !ok {
			continue // an older export, from before the table existed
		}
		rows, err := decodeRows(raw)
		if err != nil {
			return refuse("The export's %s table can't be read (%v).", t, err)
		}
		rd.Tables[t] = rows
	}
	return nil
}

// decodeRows decodes a table's rows, typing each number as Write wrote it:
// with '.', 'e' or 'E' a float64, otherwise an int64.
func decodeRows(raw json.RawMessage) ([]map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		return nil, err
	}
	for i, row := range rows {
		for k, v := range row {
			switch v := v.(type) {
			case nil, string:
			case json.Number:
				s := v.String()
				var err error
				if strings.ContainsAny(s, ".eE") {
					row[k], err = strconv.ParseFloat(s, 64)
				} else {
					row[k], err = strconv.ParseInt(s, 10, 64)
				}
				if err != nil {
					return nil, fmt.Errorf("row %d, %s: %v", i+1, k, err)
				}
			default:
				return nil, fmt.Errorf("row %d, %s: a value an export never holds", i+1, k)
			}
		}
	}
	return rows, nil
}

// Load inserts rd's rows through tx, table by table in Tables order. Every
// column named must exist in this build's schema (an unknown one is
// refused, naming its table); a column the export lacks takes its default.
// Foreign keys are checked at commit (tickets refer to their parents in id
// order). Attachments keep the blob_key the export gives (the entry name);
// the caller rewrites it to its own instance's keys. Link events without
// to_id get it as on upgrade (appdb.BackfillLinkTargets), and event_tickets
// is built from the events (appdb.BackfillEventTickets), and approvals
// asked before their ticket's latest move are cleared
// (appdb.ClearMovedApprovals). Last, the search
// indexes are rebuilt, leaving deleted comments out as the triggers do.
func Load(ctx context.Context, tx *sql.Tx, rd *Reader) error {
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return err
	}
	for _, t := range Tables {
		rows := rd.Tables[t]
		if len(rows) == 0 {
			continue
		}
		cols, err := columns(ctx, tx, t)
		if err != nil {
			return fmt.Errorf("import %s: %w", t, err)
		}
		known := map[string]bool{}
		for _, c := range cols {
			known[c.name] = true
		}
		stmts := map[string]*sql.Stmt{}
		for _, row := range rows {
			names := make([]string, 0, len(row))
			for k := range row {
				if !known[k] {
					closeAll(stmts)
					return refuse("The export's %s table has a column called %s, which this vaquero doesn't know.", t, k)
				}
				names = append(names, k)
			}
			slices.Sort(names)
			if t == "settings" {
				if k, _ := row["key"].(string); !slices.Contains(exportedSettings, k) {
					closeAll(stmts)
					return refuse("The export holds the setting %q, which an export never carries.", k)
				}
			}
			sig := strings.Join(names, ",")
			st, ok := stmts[sig]
			if !ok {
				quoted := make([]string, len(names))
				for i, n := range names {
					quoted[i] = quote(n)
				}
				q := "INSERT INTO " + quote(t) + " (" + strings.Join(quoted, ", ") + ") VALUES (" +
					strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ") + ")"
				if st, err = tx.PrepareContext(ctx, q); err != nil {
					closeAll(stmts)
					return fmt.Errorf("import %s: %w", t, err)
				}
				stmts[sig] = st
			}
			args := make([]any, len(names))
			for i, n := range names {
				args[i] = row[n]
			}
			if _, err := st.ExecContext(ctx, args...); err != nil {
				closeAll(stmts)
				return fmt.Errorf("import %s: %w", t, err)
			}
		}
		closeAll(stmts)
	}
	// An export from v0.1.x has link events without to_id; fill them in as
	// migration 0008 did on upgrade.
	if err := appdb.BackfillLinkTargets(ctx, tx); err != nil {
		return err
	}
	// event_tickets isn't exported: build it from the events' payloads, as
	// migration 0013 did on upgrade.
	if err := appdb.BackfillEventTickets(ctx, tx); err != nil {
		return err
	}
	// An export from v0.1.7 or before may hold approvals asked before a
	// move; clear them as migration 0014 did on upgrade.
	if err := appdb.ClearMovedApprovals(ctx, tx); err != nil {
		return err
	}
	return appdb.RebuildSearch(ctx, tx)
}

func closeAll(stmts map[string]*sql.Stmt) {
	for _, st := range stmts {
		st.Close()
	}
}
