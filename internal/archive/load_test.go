package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/archive"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/search"
)

// repack builds a .tar.gz from entries, as Write lays them out.
func repack(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := *e.hdr
		h.Size = int64(len(e.data))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// edited is the seeded export with export.json passed through edit.
func edited(t *testing.T, edit func(string) string) []byte {
	t.Helper()
	d, store := seeded(t)
	entries := unpack(t, export(t, d, store))
	entries[0].data = []byte(edit(string(entries[0].data)))
	return repack(t, entries)
}

func read(t *testing.T, b []byte) (*archive.Reader, error) {
	t.Helper()
	rd, err := archive.Read(bytes.NewReader(b))
	if err == nil {
		t.Cleanup(func() { rd.Close() })
	}
	return rd, err
}

func TestReadExport(t *testing.T) {
	d, store := seeded(t)
	entries := unpack(t, export(t, d, store))
	rd, err := read(t, repack(t, entries))
	if err != nil {
		t.Fatal(err)
	}
	if rd.Header != header {
		t.Fatalf("header %+v", rd.Header)
	}
	if n := len(rd.Tables["tickets"]); n != 8 {
		t.Fatalf("%d tickets", n)
	}
	// Numbers come back typed as export.json wrote them.
	tk := rd.Tables["tickets"][0]
	if _, ok := tk["position"].(float64); !ok {
		t.Fatalf("position is %T", tk["position"])
	}
	if _, ok := tk["version"].(int64); !ok {
		t.Fatalf("version is %T", tk["version"])
	}
	atts := rd.Tables["attachments"]
	if len(atts) != 2 {
		t.Fatalf("%d attachments", len(atts))
	}
	for i, a := range atts {
		r, size, ok := rd.Blob(a["id"].(string))
		if !ok {
			t.Fatalf("no blob for %v", a["id"])
		}
		got, _ := io.ReadAll(r)
		if !bytes.Equal(got, entries[i+1].data) || size != int64(len(got)) {
			t.Fatalf("blob %d differs", i)
		}
	}
	if _, _, ok := rd.Blob("nope"); ok {
		t.Fatal("a blob for an unknown id")
	}
	// Close removes the spooled files.
	dir := rd.SpoolDir()
	if err := rd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("spool dir left: %v", err)
	}
}

func TestReadRefuses(t *testing.T) {
	newest, err := appdb.Newest()
	if err != nil {
		t.Fatal(err)
	}
	d, store := seeded(t)
	good := unpack(t, export(t, d, store))
	clone := func() []entry {
		out := make([]entry, len(good))
		for i, e := range good {
			h := *e.hdr
			out[i] = entry{&h, append([]byte(nil), e.data...)}
		}
		return out
	}
	json := func(edit func(string) string) []byte {
		es := clone()
		es[0].data = []byte(edit(string(es[0].data)))
		return repack(t, es)
	}
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"not gzip", []byte("hello"), "This file isn't a Vaquero export."},
		{"wrong format", json(func(s string) string { return strings.Replace(s, `"vaquero-export"`, `"other"`, 1) }), "This file isn't a Vaquero export."},
		{"wrong format version", json(func(s string) string { return strings.Replace(s, `"format_version":1`, `"format_version":2`, 1) }),
			"This export is format version 2, which this vaquero can't read. Upgrade vaquero and import again."},
		{"newer schema", json(func(s string) string {
			return strings.Replace(s, `"schema":7`, `"schema":`+itoa(newest+1), 1)
		}), "This export is from a newer vaquero (schema " + itoa(newest+1) + "). Upgrade vaquero and import again."},
		{"unknown table", json(func(s string) string { return strings.Replace(s, `"settings":`, `"secrets":[],"settings":`, 1) }),
			"The export holds a table called secrets, which this vaquero doesn't know."},
		{"row without its file", func() []byte { return repack(t, clone()[:2]) }(), "The export has no file for attachment "},
		{"file without its row", func() []byte {
			es := clone()
			extra := entry{&tar.Header{Typeflag: tar.TypeReg, Name: "attachments/01JX0000000000000000000000", Mode: 0o644}, []byte("x")}
			return repack(t, append(es, extra))
		}(), "The export holds attachments/01JX0000000000000000000000, but no attachment record names it."},
		{"file twice", func() []byte { es := clone(); return repack(t, append(es, es[1])) }(), "twice"},
		{"wrong size", func() []byte { es := clone(); es[1].data = es[1].data[:3]; return repack(t, es) }(), "bytes, but its record says"},
		{"stray entry", func() []byte {
			es := clone()
			return repack(t, append(es, entry{&tar.Header{Typeflag: tar.TypeReg, Name: "notes.txt", Mode: 0o644}, []byte("x")}))
		}(), "The export holds notes.txt, which isn't part of an export."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := os.ReadDir(os.TempDir())
			_, err := read(t, tc.file)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			// A refused read leaves no spool directory behind.
			after, _ := os.ReadDir(os.TempDir())
			if spools(after) > spools(before) {
				t.Fatal("a spool directory was left behind")
			}
		})
	}
}

func spools(es []os.DirEntry) int {
	n := 0
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "vaquero-import-") {
			n++
		}
	}
	return n
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// load reads b and loads it into a fresh database in one transaction.
func load(t *testing.T, b []byte) (dbtest.DB, error) {
	t.Helper()
	rd, err := read(t, b)
	if err != nil {
		t.Fatal(err)
	}
	d := dbtest.New(t)
	tx, err := d.W.BeginTx(bg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := archive.Load(bg, tx, rd); err != nil {
		return d, err
	}
	return d, tx.Commit()
}

func TestLoadUnknownColumn(t *testing.T) {
	b := edited(t, func(s string) string { return strings.Replace(s, `"title":`, `"colour":"red","title":`, 1) })
	d, err := load(t, b)
	want := "The export's tickets table has a column called colour, which this vaquero doesn't know."
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	var n int
	if err := d.R.QueryRow("SELECT count(*) FROM actors").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows written: %d %v", n, err)
	}
}

func TestLoadRefusesInstanceSetting(t *testing.T) {
	b := edited(t, func(s string) string {
		return strings.Replace(s, `"settings":[]`, `"settings":[{"key":"instance_id","updated_at":"2026-11-20T12:00:00.000Z","value":"x"}]`, 1)
	})
	if _, err := load(t, b); err == nil || !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("got %v", err)
	}
}

// A missing column takes its default: an older export lacks the newer ones.
func TestLoadMissingColumnDefaults(t *testing.T) {
	b := edited(t, func(s string) string {
		// Drop every tickets.version: the key comes right after "updated_at".
		for {
			i := strings.Index(s, `,"version":`)
			if i < 0 {
				return s
			}
			j := i + len(`,"version":`)
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			s = s[:i] + s[j:]
		}
	})
	d, err := load(t, b)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow("SELECT count(*) FROM tickets WHERE version = 1").Scan(&n); err != nil || n != 8 {
		t.Fatalf("%d tickets at the default version, %v", n, err)
	}
}

// The search indexes are rebuilt, and a deleted comment stays out of them.
func TestLoadRebuildsSearch(t *testing.T) {
	d, store := seeded(t)
	db, err := load(t, export(t, d, store))
	if err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"lasso": 1, "checklist": 1, "taken back": 0, "install guide": 1} {
		hits, err := search.Run(bg, db.R, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != want {
			t.Errorf("%q: %d hits, want %d", q, len(hits), want)
		}
	}
	var n int
	if err := db.R.QueryRow("SELECT count(*) FROM comments_fts WHERE comments_fts MATCH 'taken'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("deleted comment indexed: %d %v", n, err)
	}
}
