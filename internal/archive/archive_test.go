package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	// No settings key is exported yet; allow one here so the settings
	// columns are checked too.
	archive.SetExportedSettings(t, "example")
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
	// event_tickets is derived from the events and rebuilt on import;
	// actor_avatars is exported from the profile task on (VAQ-149).
	left := []string{"api_tokens", "mcp_calls", "blobs", "goose_db_version", "sqlite_sequence", "event_tickets", "actor_avatars"}
	// The search indexes: each FTS5 virtual table and its shadow tables.
	vt, err := d.R.QueryContext(bg, "SELECT name FROM sqlite_schema WHERE type = 'table' AND sql LIKE 'CREATE VIRTUAL TABLE%'")
	if err != nil {
		t.Fatal(err)
	}
	var fts []string
	for vt.Next() {
		var n string
		if err := vt.Scan(&n); err != nil {
			t.Fatal(err)
		}
		fts = append(fts, n)
		for _, suffix := range []string{"_data", "_idx", "_content", "_docsize", "_config"} {
			fts = append(fts, n+suffix)
		}
	}
	vt.Close()
	if !slices.Contains(fts, "tickets_fts") || !slices.Contains(fts, "comments_fts") {
		t.Fatalf("search indexes %v", fts)
	}
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
		index := slices.Contains(fts, n)
		if !slices.Contains(archive.Tables, n) && !slices.Contains(left, n) && !index {
			t.Errorf("table %s is neither exported nor left out", n)
		}
		if index && slices.Contains(archive.Tables, n) {
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
	var missing *archive.FileError
	if !errors.As(err, &missing) || !missing.Missing || missing.AttachmentID != id || missing.TicketKey != ticket {
		t.Fatalf("%v %+v", err, missing)
	}
	if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), ticket) {
		t.Fatalf("message %q names neither %s nor %s", err, id, ticket)
	}
	if buf.Len() != 0 {
		t.Fatalf("%d bytes written before the failure", buf.Len())
	}
}

// vanishing is a store whose objects pass the size check (Stat) and then
// are gone when read: an attachment deleted between the check and the copy.
type vanishing struct {
	blobs.Store
}

func (v *vanishing) Get(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, blobs.ErrNotFound
}

// A file that vanishes once the archive has started is still an error, so
// the caller knows the bytes it sent are broken.
func TestExportFileVanishesWhileStreaming(t *testing.T) {
	d, store := seeded(t)
	var buf bytes.Buffer
	err := archive.Write(bg, &buf, d.R, &vanishing{store}, header)
	var missing *archive.FileError
	if !errors.As(err, &missing) || !missing.Missing || buf.Len() == 0 {
		t.Fatalf("%v, %d bytes", err, buf.Len())
	}
}

// An object shorter than its record (truncated out of band) is refused
// before any byte, naming the attachment and its ticket.
func TestExportWrongSizeFails(t *testing.T) {
	d, store := seeded(t)
	var id, key, ticket string
	var size int64
	if err := d.R.QueryRowContext(bg, `SELECT a.id, a.blob_key, a.size, p.key || '-' || t.number
		FROM attachments a JOIN tickets t ON t.id = a.ticket_id JOIN projects p ON p.id = t.project_id
		ORDER BY a.id LIMIT 1`).Scan(&id, &key, &size, &ticket); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(bg, key, strings.NewReader("short"), 5, "image/png"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := archive.Write(bg, &buf, d.R, store, header)
	var fe *archive.FileError
	if !errors.As(err, &fe) || fe.Missing || fe.AttachmentID != id || fe.TicketKey != ticket || fe.Size != size || fe.Stored != 5 {
		t.Fatalf("%v %+v", err, fe)
	}
	if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), ticket) || buf.Len() != 0 {
		t.Fatalf("%q, %d bytes", err, buf.Len())
	}
	if err := archive.Check(bg, d.R, store, header); !errors.As(err, &fe) {
		t.Fatalf("Check: %v", err)
	}
}

// A settings key in neither known set stops the export: it might hold a
// secret.
func TestExportUnknownSettingRefused(t *testing.T) {
	d, store := seeded(t)
	if err := archive.Check(bg, d.R, store, header); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.ExecContext(bg, "INSERT INTO settings (key, value, updated_at) VALUES ('smtp_password', 'hunter2', 'x')"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := archive.Write(bg, &buf, d.R, store, header)
	if err == nil || !strings.Contains(err.Error(), "smtp_password") || buf.Len() != 0 {
		t.Fatalf("%v, %d bytes", err, buf.Len())
	}
	if err := archive.Check(bg, d.R, store, header); err == nil || !strings.Contains(err.Error(), "smtp_password") {
		t.Fatalf("Check: %v", err)
	}
}

// Every settings key the code writes is in a known set. Keys are written
// with PutSetting, whose Key is a string literal or a constant declared in
// the same file.
func TestSettingsKeysKnown(t *testing.T) {
	put := regexp.MustCompile(`PutSettingParams\{Key:\s*([A-Za-z0-9_]+|"[^"]*")`)
	found := 0
	err := filepath.WalkDir("../..", func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() && (de.Name() == "gen" || de.Name() == "testdata" || strings.HasPrefix(de.Name(), ".") && path != "../..") {
			return filepath.SkipDir
		}
		if de.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		if strings.Contains(strings.ToUpper(src), "INTO SETTINGS") {
			t.Errorf("%s writes settings without PutSetting", path)
		}
		for _, m := range put.FindAllStringSubmatch(src, -1) {
			key := m[1]
			if strings.HasPrefix(key, `"`) {
				key = strings.Trim(key, `"`)
			} else {
				c := regexp.MustCompile(`\b` + key + `\s*=\s*"([^"]+)"`).FindStringSubmatch(src)
				if c == nil {
					t.Errorf("%s: can't resolve settings key %s; declare it as a constant in that file", path, key)
					continue
				}
				key = c[1]
			}
			found++
			if !slices.Contains(archive.ExportedSettings(), key) && !slices.Contains(archive.InstanceSettings(), key) {
				t.Errorf("%s writes settings key %q, in neither archive.ExportedSettings nor archive.InstanceSettings", path, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found < 3 {
		t.Fatalf("found %d PutSetting calls; the scan is broken", found)
	}
}

// A fractional position (a board drop between two cards) comes back exactly.
func TestExportFractionalPosition(t *testing.T) {
	d, store := seeded(t)
	want := map[string]float64{}
	for i, v := range []float64{0.1 + 0.2, 1536.123456789012, 3e-9} {
		var id string
		if err := d.R.QueryRowContext(bg, "SELECT id FROM tickets ORDER BY id LIMIT 1 OFFSET ?", i).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := d.W.ExecContext(bg, "UPDATE tickets SET position = ? WHERE id = ?", v, id); err != nil {
			t.Fatal(err)
		}
		want[id] = v
	}
	raw := unpack(t, export(t, d, store))[0].data
	var doc struct {
		Tickets []map[string]any `json:"tickets"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tk := range doc.Tickets {
		v, ok := want[tk["id"].(string)]
		if !ok {
			continue
		}
		n := tk["position"].(json.Number)
		got, err := strconv.ParseFloat(string(n), 64)
		if err != nil || got != v || !strings.ContainsAny(string(n), ".eE") {
			t.Errorf("position %s, want %v (%v)", n, v, err)
		}
		seen++
	}
	if seen != len(want) {
		t.Fatalf("saw %d of %d", seen, len(want))
	}
}

// The allow-lists are read through accessors that hand out copies, so no
// caller (or parallel test) can change what an export carries.
func TestSettingsListsAreReadOnly(t *testing.T) {
	ins := archive.InstanceSettings()
	if !slices.Contains(ins, "instance_id") || !slices.Contains(ins, "blob_store") {
		t.Fatalf("%v", ins)
	}
	ins[0] = "tampered"
	if slices.Contains(archive.InstanceSettings(), "tampered") {
		t.Fatal("InstanceSettings returned its own slice")
	}
	archive.SetExportedSettings(t, "example")
	got := archive.ExportedSettings()
	got[0] = "tampered"
	if !slices.Equal(archive.ExportedSettings(), []string{"example"}) {
		t.Fatalf("%v", archive.ExportedSettings())
	}
}
