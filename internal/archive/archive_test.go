package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/archive"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/core/coreseed"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

var bg = context.Background()

const instance = "01jx00000000000000000test00"

var header = archive.Header{
	Format: archive.Format, FormatVersion: archive.FormatVersion, Schema: 7,
	ExportedAt: "2026-11-20T12:00:00.000Z", VaqueroVersion: "1.2.3",
}

// seeded is a database filled by coreseed with a fixed clock, and its store.
func seeded(t *testing.T) (dbtest.DB, blobs.Store) {
	t.Helper()
	d := dbtest.New(t)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	c.BlobStore = "sqlite"
	store := blobs.NewSQLite(d.W, d.R, clock)
	if err := coreseed.Seed(bg, c, d.Q, store, instance, now); err != nil {
		t.Fatal(err)
	}
	return d, store
}

func export(t *testing.T, d dbtest.DB, store blobs.Store) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := archive.Write(bg, &buf, d.R, store, header); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type entry struct {
	hdr  *tar.Header
	data []byte
}

// unpack checks the gzip header and returns the tar entries in order.
func unpack(t *testing.T, b []byte) []entry {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if zr.Name != "" || !zr.ModTime.IsZero() || zr.Comment != "" {
		t.Fatalf("gzip header: %q %v %q", zr.Name, zr.ModTime, zr.Comment)
	}
	tr := tar.NewReader(zr)
	var out []entry
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, entry{h, data})
	}
	return out
}

// tables decodes export.json's tables.
func tables(t *testing.T, raw []byte) map[string][]map[string]any {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string][]map[string]any{}
	for _, name := range archive.Tables {
		var rows []map[string]any
		if err := json.Unmarshal(doc[name], &rows); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = rows
	}
	return out
}

// topKeys is export.json's top-level keys in the order written.
func topKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("%v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func TestExportDeterministic(t *testing.T) {
	d, store := seeded(t)
	a, b := export(t, d, store), export(t, d, store)
	if !bytes.Equal(a, b) {
		t.Fatal("two exports of one database differ")
	}

	entries := unpack(t, a)
	var atts []gen.Attachment
	rows, err := d.R.QueryContext(bg, "SELECT id, blob_key FROM attachments ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a gen.Attachment
		if err := rows.Scan(&a.ID, &a.BlobKey); err != nil {
			t.Fatal(err)
		}
		atts = append(atts, a)
	}
	rows.Close()
	if len(atts) != 2 || len(entries) != 3 {
		t.Fatalf("%d attachments, %d entries", len(atts), len(entries))
	}
	if entries[0].hdr.Name != "export.json" {
		t.Fatalf("first entry %q", entries[0].hdr.Name)
	}
	for i, a := range atts {
		e := entries[i+1]
		if e.hdr.Name != "attachments/"+a.ID {
			t.Fatalf("entry %d is %q, want attachments/%s", i+1, e.hdr.Name, a.ID)
		}
		rc, _, err := store.Get(bg, a.BlobKey)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(e.data, want) {
			t.Fatalf("%s: bytes differ", e.hdr.Name)
		}
	}
	for _, e := range entries {
		h := e.hdr
		if h.Mode != 0o644 || h.ModTime.Unix() != 0 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || h.Typeflag != tar.TypeReg {
			t.Errorf("%s header: %+v", h.Name, h)
		}
	}

	raw := entries[0].data
	want := append([]string{"format", "format_version", "schema", "exported_at", "vaquero_version"}, archive.Tables...)
	if got := topKeys(t, raw); !slices.Equal(got, want) {
		t.Fatalf("top-level keys %v, want %v", got, want)
	}
	var h struct {
		Format         string `json:"format"`
		FormatVersion  int    `json:"format_version"`
		Schema         int64  `json:"schema"`
		ExportedAt     string `json:"exported_at"`
		VaqueroVersion string `json:"vaquero_version"`
	}
	if err := json.Unmarshal(raw, &h); err != nil || h.Format != "vaquero-export" || h.FormatVersion != 1 ||
		h.Schema != 7 || h.ExportedAt != header.ExportedAt || h.VaqueroVersion != "1.2.3" {
		t.Fatalf("%v %+v", err, h)
	}
	// Row keys are sorted, REAL values always carry a point or an
	// exponent, and integers never do.
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(raw, &doc)
	if tk := doc["tickets"]; !regexp.MustCompile(`"position":1024\.0[,}]`).Match(tk) ||
		regexp.MustCompile(`"position":-?[0-9]+[,}]`).Match(tk) {
		t.Errorf("position not encoded as a float: %s", tk)
	}
	if !regexp.MustCompile(`"version":[0-9]+[,}]`).Match(doc["tickets"]) {
		t.Errorf("version not an integer")
	}
	// The stored blob key names this instance; the export names the entry.
	for _, a := range tables(t, raw)["attachments"] {
		if a["blob_key"] != "attachments/"+a["id"].(string) {
			t.Errorf("blob_key %v", a["blob_key"])
		}
	}
	for _, name := range archive.Tables {
		dec := json.NewDecoder(bytes.NewReader(doc[name]))
		if _, err := dec.Token(); err != nil {
			t.Fatal(err)
		}
		for dec.More() {
			var obj json.RawMessage
			if err := dec.Decode(&obj); err != nil {
				t.Fatal(err)
			}
			keys := topKeys(t, obj)
			if !slices.IsSorted(keys) {
				t.Fatalf("%s row keys not sorted: %v", name, keys)
			}
		}
	}
	if bytes.Contains(raw, []byte("\n  ")) {
		t.Error("export.json is indented")
	}
	// Primary-key order: events by seq, tickets by id.
	tb := tables(t, raw)
	var seqs []float64
	for _, e := range tb["events"] {
		seqs = append(seqs, e["seq"].(float64))
	}
	if len(seqs) < 10 || !slices.IsSorted(seqs) {
		t.Errorf("events not in seq order: %v", seqs)
	}
	var ids []string
	for _, tk := range tb["tickets"] {
		ids = append(ids, tk["id"].(string))
	}
	if !slices.IsSorted(ids) {
		t.Errorf("tickets not in id order")
	}
}

func TestExportLeavesOutSecrets(t *testing.T) {
	d, store := seeded(t)
	var hashes []string
	rows, err := d.R.QueryContext(bg, "SELECT token_hash FROM api_tokens")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}
	rows.Close()
	if len(hashes) == 0 {
		t.Fatal("seed made no tokens")
	}
	var claimed int
	if err := d.R.QueryRowContext(bg, "SELECT count(*) FROM tickets WHERE claimed_by IS NOT NULL").Scan(&claimed); err != nil || claimed == 0 {
		t.Fatalf("seed made no claim: %v %d", err, claimed)
	}
	if _, err := d.W.ExecContext(bg, "INSERT INTO settings (key, value, updated_at) VALUES ('instance_id', ?, 'x')", instance); err != nil {
		t.Fatal(err)
	}

	b := export(t, d, store)
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	// vq_abcdef is the MCP log row's token prefix.
	for _, bad := range append([]string{"api_tokens", "mcp_calls", "vq_abcdef", "blob_store", "instance_id", instance}, hashes...) {
		if bytes.Contains(all, []byte(bad)) {
			t.Errorf("archive holds %q", bad)
		}
	}
	tb := tables(t, unpack(t, b)[0].data)
	for _, tk := range tb["tickets"] {
		for _, col := range []string{"claimed_by", "claim_expires_at", "claim_prev_status_id"} {
			if v, ok := tk[col]; !ok || v != nil {
				t.Errorf("%s %s = %v (present %v)", tk["id"], col, v, ok)
			}
		}
	}
}

func TestExportEveryColumn(t *testing.T) {
	d, store := seeded(t)
	if _, err := d.W.ExecContext(bg, "INSERT INTO settings (key, value, updated_at) VALUES ('example', 'yes', 'x')"); err != nil {
		t.Fatal(err)
	}
	tb := tables(t, unpack(t, export(t, d, store))[0].data)
	for _, name := range archive.Tables {
		var cols []string
		rows, err := d.R.QueryContext(bg, "SELECT name FROM pragma_table_info(?)", name)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, c)
		}
		rows.Close()
		if len(cols) == 0 {
			t.Fatalf("no table %s", name)
		}
		slices.Sort(cols)
		if len(tb[name]) == 0 {
			t.Errorf("%s: no rows exported; seed one so its columns are checked", name)
		}
		for _, row := range tb[name] {
			var keys []string
			for k := range row {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, cols) {
				t.Fatalf("%s row has %v, want %v", name, keys, cols)
			}
		}
	}

	// Every table is exported or deliberately left out, so a new
	// migration's table can't be forgotten.
	left := []string{"api_tokens", "mcp_calls", "blobs", "goose_db_version", "sqlite_sequence"}
	rows, err := d.R.QueryContext(bg, "SELECT name FROM sqlite_schema WHERE type = 'table'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		fts := strings.Contains(n, "_fts")
		if !slices.Contains(archive.Tables, n) && !slices.Contains(left, n) && !fts {
			t.Errorf("table %s is neither exported nor left out", n)
		}
		if fts && slices.Contains(archive.Tables, n) {
			t.Errorf("search index %s exported", n)
		}
	}
}

func TestExportMissingBlobFails(t *testing.T) {
	d, store := seeded(t)
	var id, key, ticket string
	if err := d.R.QueryRowContext(bg, `SELECT a.id, a.blob_key, p.key || '-' || t.number
		FROM attachments a JOIN tickets t ON t.id = a.ticket_id JOIN projects p ON p.id = t.project_id
		ORDER BY a.id DESC LIMIT 1`).Scan(&id, &key, &ticket); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(bg, key); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := archive.Write(bg, &buf, d.R, store, header)
	var missing *archive.MissingFileError
	if !errors.As(err, &missing) || missing.AttachmentID != id || missing.TicketKey != ticket {
		t.Fatalf("%v %+v", err, missing)
	}
	if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), ticket) {
		t.Fatalf("message %q names neither %s nor %s", err, id, ticket)
	}
	if buf.Len() != 0 {
		t.Fatalf("%d bytes written before the failure", buf.Len())
	}
}

// vanishing is a store whose objects disappear after their first Get: an
// attachment deleted between the check and the copy.
type vanishing struct {
	blobs.Store
	seen map[string]bool
}

func (v *vanishing) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if v.seen[key] {
		return nil, 0, blobs.ErrNotFound
	}
	v.seen[key] = true
	return v.Store.Get(ctx, key)
}

// A file that vanishes once the archive has started is still an error, so
// the caller knows the bytes it sent are broken.
func TestExportFileVanishesWhileStreaming(t *testing.T) {
	d, store := seeded(t)
	var buf bytes.Buffer
	err := archive.Write(bg, &buf, d.R, &vanishing{store, map[string]bool{}}, header)
	var missing *archive.MissingFileError
	if !errors.As(err, &missing) || buf.Len() == 0 {
		t.Fatalf("%v, %d bytes", err, buf.Len())
	}
}
