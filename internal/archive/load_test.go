package archive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

// read reads b, spooling into a fresh directory; a refused read must leave
// that directory empty.
func read(t *testing.T, b []byte) (*archive.Reader, error) {
	t.Helper()
	dir := t.TempDir()
	rd, err := archive.Read(bytes.NewReader(b), dir)
	if err == nil {
		t.Cleanup(func() { rd.Close() })
	} else if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("a refused read left %v behind", left)
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
		r.Close()
		if !bytes.Equal(got, entries[i+1].data) || size != int64(len(got)) {
			t.Fatalf("blob %d differs", i)
		}
	}
	if _, _, ok := rd.Blob("nope"); ok {
		t.Fatal("a blob for an unknown id")
	}
	// The spool is a directory of its own under the one Read was given (the
	// data directory, for an import), and Close removes it.
	dir := rd.SpoolDir()
	if !strings.HasPrefix(filepath.Base(dir), "import-spool-") {
		t.Fatalf("spool dir %s", dir)
	}
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
			// read fails the test if a refused read leaves its spool behind.
			_, err := read(t, tc.file)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// The free space where the spool goes is checked against the attachments'
// total size before a byte is spooled.
func TestReadChecksFreeSpace(t *testing.T) {
	d, store := seeded(t)
	file := export(t, d, store)
	var total int64
	if err := d.R.QueryRowContext(bg, "SELECT sum(size) FROM attachments").Scan(&total); err != nil {
		t.Fatal(err)
	}
	var asked string
	archive.SetFreeBytes(t, func(path string) (int64, error) { asked = path; return total - 1, nil })
	dir := t.TempDir()
	_, err := archive.Read(bytes.NewReader(file), dir)
	var refused *archive.Refused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "needs 1 KB free in "+dir+", and 1 KB is free") || asked != dir {
		t.Fatalf("%v (asked about %q)", err, asked)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
	archive.SetFreeBytes(t, func(string) (int64, error) { return total, nil })
	rd, err := archive.Read(bytes.NewReader(file), dir)
	if err != nil {
		t.Fatal(err)
	}
	rd.Close()
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

// attachmentField rewrites the value of field in the first attachment row.
func attachmentField(s, field, value string) string {
	i := strings.Index(s, `"attachments":[`)
	j := i + strings.Index(s[i:], `"`+field+`":`) + len(field) + 3
	k := j
	if s[k] == '"' {
		k = j + 1 + strings.Index(s[j+1:], `"`) + 1
	} else {
		for s[k] >= '0' && s[k] <= '9' {
			k++
		}
	}
	return s[:j] + value + s[k:]
}

func TestReadRefusesHardening(t *testing.T) {
	d, store := seeded(t)
	good := unpack(t, export(t, d, store))
	withDoc := func(edit func(string) string) []byte {
		es := make([]entry, len(good))
		copy(es, good)
		es[0] = entry{good[0].hdr, []byte(edit(string(good[0].data)))}
		return repack(t, es)
	}
	firstID := good[1].hdr.Name[len("attachments/"):]

	// export.json claiming more than the cap: only its header is written.
	var huge bytes.Buffer
	zw := gzip.NewWriter(&huge)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "export.json", Mode: 0o644, Size: archive.MaxDocument + 1}); err != nil {
		t.Fatal(err)
	}
	zw.Close()

	// A flipped byte inside an attachment, with stored (uncompressed)
	// blocks so the byte can be found: sizes still match, the CRC doesn't.
	var stored bytes.Buffer
	zw, _ = gzip.NewWriterLevel(&stored, gzip.NoCompression)
	tw = tar.NewWriter(zw)
	for _, e := range good {
		tw.WriteHeader(e.hdr)
		tw.Write(e.data)
	}
	tw.Close()
	zw.Close()
	flipped := stored.Bytes()
	at := bytes.Index(flipped, []byte("Lasso notes."))
	if at < 0 {
		t.Fatal("attachment bytes not found")
	}
	flipped[at] ^= 0x20

	// Data past the tar's end inside the gzip stream: a little (a tar
	// program's padding) is read through to the checksum, more is refused
	// rather than decompressed without end.
	trailing := func(n int) []byte {
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		tw := tar.NewWriter(zw)
		for _, e := range good {
			tw.WriteHeader(e.hdr)
			tw.Write(e.data)
		}
		tw.Close()
		zw.Write(make([]byte, n))
		zw.Close()
		return b.Bytes()
	}
	if _, err := read(t, trailing(64<<10)); err != nil {
		t.Fatalf("64 KiB of padding: %v", err)
	}

	for _, tc := range []struct {
		name string
		file []byte
		want string
	}{
		{"trailing data", trailing(archive.MaxTrailing + 1), "The export has more than 1 MB of data after its last file."},
		{"attachment over the limit", withDoc(func(s string) string { return attachmentField(s, "size", "10485761") }),
			"The export's attachment " + firstID + " is 10485761 bytes; attachments can be at most 10 MB."},
		{"export.json over the cap", huge.Bytes(), "The export's export.json is larger than"},
		{"corrupted stream", flipped, "The export is damaged"},
		{"id not a ULID", withDoc(func(s string) string { return attachmentField(s, "id", `"../../x"`) }),
			"The export's attachment record 1 has an id that isn't one Vaquero makes."},
		{"ids equal once lower-cased", withDoc(func(s string) string {
			i := strings.Index(s, `"attachments":[`)
			second := good[2].hdr.Name[len("attachments/"):]
			return s[:i] + strings.Replace(s[i:], `"id":"`+second+`"`, `"id":"`+strings.ToLower(firstID)+`"`, 1)
		}), "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := read(t, tc.file)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// fdCount is the number of file descriptors this process holds.
func fdCount(t *testing.T) int {
	t.Helper()
	es, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("no /dev/fd")
	}
	return len(es)
}

func TestBlobClosesWhenCallerDoes(t *testing.T) {
	d, store := seeded(t)
	rd, err := read(t, repack(t, unpack(t, export(t, d, store))))
	if err != nil {
		t.Fatal(err)
	}
	id := rd.Tables["attachments"][0]["id"].(string)
	before := fdCount(t)
	for range 300 {
		r, _, ok := rd.Blob(id)
		if !ok {
			t.Fatal("no blob")
		}
		r.Close()
	}
	if after := fdCount(t); after > before+2 {
		t.Fatalf("descriptors rose from %d to %d: opened blobs stay open", before, after)
	}
}

// An export from v0.1.x has link events without to_id; loading it fills
// them in as migration 0008 did, so the target ticket's history lists them.
func TestLoadBackfillsLinkTargets(t *testing.T) {
	strip := regexp.MustCompile(`,\\"to_id\\":\\"[0-9A-Z]+\\"`)
	b := edited(t, func(s string) string {
		if !strip.MatchString(s) {
			t.Fatal("the seeded export has no link event with to_id")
		}
		return strip.ReplaceAllString(s, "")
	})
	d, err := load(t, b)
	if err != nil {
		t.Fatal(err)
	}
	var total, wrong int
	if err := d.R.QueryRow(`SELECT count(*), count(*) FILTER (WHERE coalesce(json_extract(e.payload, '$.to_id'), '') <> l.to_id)
		FROM events e JOIN ticket_links l ON l.id = json_extract(e.payload, '$.link_id')
		WHERE e.kind IN ('link.created', 'link.deleted')`).Scan(&total, &wrong); err != nil {
		t.Fatal(err)
	}
	if total == 0 || wrong != 0 {
		t.Fatalf("%d of %d link events lack their target's id", wrong, total)
	}
}
