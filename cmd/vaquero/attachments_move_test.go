package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/idgen"
)

// movedFrom is an instance whose two seeded attachments (and a preview of
// the first) are in backend, the server stopped, with VQ_BLOB_STORE set
// to nothing so each test names its target.
func movedFrom(t *testing.T, backend string) (dataDir string, want map[string]string) {
	t.Helper()
	dataDir, _ = localEnv(t)
	t.Setenv("VQ_BLOB_STORE", backend)
	if code, _, e := runCLI(t, "import", seededArchive(t, importHeader(t, 0), nil)); code != 0 {
		t.Fatal(e)
	}
	t.Setenv("VQ_BLOB_STORE", "")
	t.Setenv("VQ_BLOB_DIR", "")
	want = attachmentBytes(t, dataDir, backendStore(t, dataDir, backend, ""))
	if len(want) != 2 {
		t.Fatalf("seeded %d attachments", len(want))
	}
	instance := readSetting(t, dataDir, "instance_id")
	first := slices.Sorted(maps.Keys(want))[0]
	thumb := "thumbs/" + instance + "/" + first[strings.LastIndex(first, "/")+1:]
	if err := backendStore(t, dataDir, backend, "").Put(t.Context(), thumb, strings.NewReader("preview"), 7, "image/png"); err != nil {
		t.Fatal(err)
	}
	return dataDir, want
}

// backendStore opens backend for dataDir: the database's blobs table, the
// directory dir (default {data dir}/attachments), or the fake S3 bucket.
func backendStore(t *testing.T, dataDir, backend, dir string) blobs.Store {
	t.Helper()
	switch backend {
	case "sqlite":
		path := filepath.Join(dataDir, "vaquero.db")
		w, err := appdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		r, err := appdb.OpenReader(path, 1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close(); w.Close() })
		return blobs.NewSQLite(w, r, time.Now)
	case "dir":
		if dir == "" {
			dir = filepath.Join(dataDir, "attachments")
		}
		s, err := blobs.NewDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return fakeS3Store(t, dir)
}

// fakeBuckets maps an S3 endpoint/bucket to the directory standing in for
// it: S3 here is a test fake, never a network service.
var fakeBuckets = struct {
	sync.Mutex
	dirs map[string]string
}{dirs: map[string]string{}}

func fakeS3Store(t *testing.T, loc string) blobs.Store {
	t.Helper()
	fakeBuckets.Lock()
	dir, ok := fakeBuckets.dirs[loc]
	if !ok {
		dir = t.TempDir()
		fakeBuckets.dirs[loc] = dir
	}
	fakeBuckets.Unlock()
	s, err := blobs.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// withStores replaces openBlobStore for one test: s3 opens the fake bucket
// its endpoint and bucket name, every store passes through wrap (nil keeps
// it).
func withStores(t *testing.T, wrap func(cfg config.Config, s blobs.Store) blobs.Store) {
	t.Helper()
	saved := openBlobStore
	openBlobStore = func(cfg config.Config, w, r *sql.DB, now func() time.Time) (blobs.Store, error) {
		var s blobs.Store
		if cfg.BlobBackend == "s3" {
			if cfg.S3AccessKeyID == "" || len(cfg.S3SecretAccessKey) == 0 {
				return nil, errors.New("no S3 keys")
			}
			s = fakeS3Store(t, blobs.Location(cfg))
		} else {
			var err error
			if s, err = blobs.Open(cfg, w, r, now); err != nil {
				return nil, err
			}
		}
		if wrap != nil {
			s = wrap(cfg, s)
		}
		return s, nil
	}
	t.Cleanup(func() { openBlobStore = saved })
}

// attachmentBytes reads every attachment's bytes from store, by blob key.
func attachmentBytes(t *testing.T, dataDir string, store blobs.Store) map[string]string {
	t.Helper()
	db, err := appdb.OpenReader(filepath.Join(dataDir, "vaquero.db"), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT blob_key FROM attachments ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		keys = append(keys, k)
	}
	rows.Close()
	got := map[string]string{}
	for _, k := range keys {
		rc, _, err := store.Get(t.Context(), k)
		if errors.Is(err, blobs.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[k] = string(b)
	}
	return got
}

// objectKeys lists every object in store.
func objectKeys(t *testing.T, store blobs.Store) []string {
	t.Helper()
	var keys []string
	if err := store.List(t.Context(), "", func(k string, _ time.Time) error { keys = append(keys, k); return nil }); err != nil {
		t.Fatal(err)
	}
	return keys
}

func movePending(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "move.pending"))
	return err == nil
}

// The hand-off: the next serve boots on the new storage with no refusal,
// serves the moved bytes from there, and refuses the old storage.
func TestMoveSQLiteToDirHandsOffToServe(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	t.Setenv("VQ_BLOB_STORE", "dir")
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if !strings.Contains(out, "Moved 2 attachments to dir") || !strings.Contains(out, "VQ_BLOB_STORE=dir") {
		t.Fatalf("%q", out)
	}
	dir := backendStore(t, dataDir, "dir", "")
	if got := attachmentBytes(t, dataDir, dir); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
	// Previews are made again on demand, never copied.
	for _, k := range objectKeys(t, dir) {
		if strings.HasPrefix(k, "thumbs/") {
			t.Fatalf("copied a preview: %s", k)
		}
	}
	// Without --delete-old the old copies stay.
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "sqlite", "")); !maps.Equal(got, want) {
		t.Fatalf("the old copies went: %v", got)
	}
	if s, l := readSetting(t, dataDir, "blob_store"), readSetting(t, dataDir, "blob_location"); s != "dir" || l != "{data dir}/attachments" {
		t.Fatalf("blob_store %q, blob_location %q", s, l)
	}
	if movePending(dataDir) {
		t.Fatal("move.pending stays")
	}

	err := bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "sqlite"})
	if err == nil || !strings.Contains(err.Error(), "Attachments are stored in dir") {
		t.Fatalf("the old storage boots: %v", err)
	}
	serving(t, dataDir, map[string]string{"VQ_BLOB_STORE": "dir"}, func(base string) {
		tok, _ := os.ReadFile(filepath.Join(dataDir, "token"))
		db, _ := appdb.OpenReader(filepath.Join(dataDir, "vaquero.db"), 1)
		defer db.Close()
		var id, key string
		db.QueryRow(`SELECT id, blob_key FROM attachments ORDER BY id LIMIT 1`).Scan(&id, &key)
		req, _ := http.NewRequest(http.MethodGet, base+"/api/v1/attachments/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 || string(b) != want[key] {
			t.Fatalf("%d %q", resp.StatusCode, b)
		}
	})
}

// Into the database, with --delete-old: the directory ends up empty,
// previews included, and serve boots on sqlite.
func TestMoveDirToSQLiteDeletesOld(t *testing.T) {
	dataDir, want := movedFrom(t, "dir")
	code, out, e := runCLI(t, "attachments", "move", "--to", "sqlite", "--delete-old")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if !strings.Contains(out, "Deleted the old copies from dir") {
		t.Fatalf("%q", out)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "sqlite", "")); !maps.Equal(got, want) {
		t.Fatalf("sqlite holds %v", got)
	}
	if n := dirObjects(t, filepath.Join(dataDir, "attachments")); n != 0 {
		t.Fatalf("%d objects left in the directory", n)
	}
	if s, l := readSetting(t, dataDir, "blob_store"), readSetting(t, dataDir, "blob_location"); s != "sqlite" || l != "" {
		t.Fatalf("blob_store %q, blob_location %q", s, l)
	}
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatalf("serve on sqlite: %v", err)
	}
}

// To S3 and on to a directory: the old bucket is read with the VQ_S3_
// keys in the environment and emptied by --delete-old.
func TestMoveThroughS3(t *testing.T) {
	withStores(t, nil)
	dataDir, want := movedFrom(t, "sqlite")
	t.Setenv("VQ_S3_ENDPOINT", "S3.example.test:9000")
	t.Setenv("VQ_S3_BUCKET", "vq")
	t.Setenv("VQ_S3_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "s3cr3t-key-DO-NOT-LEAK")
	code, out, e := runCLI(t, "attachments", "move", "--to", "s3")
	if code != 0 || strings.Contains(out+e, "DO-NOT-LEAK") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	bucket := fakeS3Store(t, "s3.example.test:9000/vq")
	if got := attachmentBytes(t, dataDir, bucket); !maps.Equal(got, want) {
		t.Fatalf("the bucket holds %v", got)
	}
	if l := readSetting(t, dataDir, "blob_location"); l != "s3.example.test:9000/vq" {
		t.Fatalf("blob_location %q", l)
	}

	// Reading the old bucket needs its keys.
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "")
	code, _, e = runCLI(t, "attachments", "move", "--to", "dir")
	if code != 1 || !strings.Contains(e, "VQ_S3_SECRET_ACCESS_KEY") {
		t.Fatalf("%d %q", code, e)
	}
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "s3cr3t-key-DO-NOT-LEAK")
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old"); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
	if left := objectKeys(t, bucket); len(left) != 0 {
		t.Fatalf("left in the bucket: %v", left)
	}
	if err := bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "dir"}); err != nil {
		t.Fatal(err)
	}
}

// failingPut fails the nth attachment put into the target.
type failingPut struct {
	blobs.Store
	n, puts int
}

func (f *failingPut) Put(ctx context.Context, key string, r io.Reader, size int64, ctype string) error {
	if strings.HasPrefix(key, "attachments/") && !strings.Contains(key, "/move-probe-") {
		f.puts++
		if f.puts == f.n {
			return errors.New("the disk went away")
		}
	}
	return f.Store.Put(ctx, key, r, size, ctype)
}

// Review Focus 3: a move that fails at file 2 leaves the old storage in
// use (serve boots on it), and a rerun skips file 1 and finishes.
func TestMoveResumesAfterAnInterruption(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	before := settingsDump(t, dataDir)
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &failingPut{Store: s, n: 2}
		}
		return s
	})
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 1 || !strings.Contains(e, "the disk went away") || !strings.Contains(e, "Attachments are still stored in sqlite") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if after := settingsDump(t, dataDir); after != before {
		t.Fatalf("settings changed:\n%s\n%s", before, after)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "sqlite", "")); !maps.Equal(got, want) {
		t.Fatal("--delete-old deleted before the switch")
	}
	if !movePending(dataDir) {
		t.Fatal("no move.pending")
	}
	if n := dirObjects(t, filepath.Join(dataDir, "attachments")); n != 1 {
		t.Fatalf("%d objects copied, want 1", n)
	}
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatalf("the old storage must still boot: %v", err)
	}

	withStores(t, nil)
	code, out, e = runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 0 || !strings.Contains(out, "1 copied by an earlier run") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
	if left := objectKeys(t, backendStore(t, dataDir, "sqlite", "")); len(left) != 0 {
		t.Fatalf("left in sqlite: %v", left)
	}
	if movePending(dataDir) {
		t.Fatal("move.pending stays")
	}
}

// A copy recorded as verified but gone from the target by the rerun is
// copied again: the switch never names storage that lacks a file.
func TestMoveRecopiesAVanishedCopy(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &failingPut{Store: s, n: 2}
		}
		return s
	})
	if code, _, _ := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 {
		t.Fatal("the first run passed")
	}
	dir := backendStore(t, dataDir, "dir", "")
	for _, k := range objectKeys(t, dir) {
		dir.Delete(t.Context(), k)
	}
	withStores(t, nil)
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 || strings.Contains(out, "earlier run") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if got := attachmentBytes(t, dataDir, dir); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
}

// corruptGet reads back the nth attachment altered.
type corruptGet struct {
	blobs.Store
	n, gets int
}

func (c *corruptGet) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	rc, size, err := c.Store.Get(ctx, key)
	if err != nil || !strings.HasPrefix(key, "attachments/") {
		return rc, size, err
	}
	c.gets++
	if c.gets != c.n {
		return rc, size, err
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	b[0] ^= 0xff
	return io.NopCloser(bytes.NewReader(b)), size, nil
}

// Review Focus 3: --delete-old deletes nothing when any copy fails its
// check, and nothing switches.
func TestMoveDeleteOldDeletesNothingWhenACheckFails(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	before := settingsDump(t, dataDir)
	old := objectKeys(t, backendStore(t, dataDir, "sqlite", ""))
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &corruptGet{Store: s, n: 2}
		}
		return s
	})
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 1 || !strings.Contains(e, "doesn't match the original") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if now := objectKeys(t, backendStore(t, dataDir, "sqlite", "")); !slices.Equal(now, old) {
		t.Fatalf("old copies deleted: %v, was %v", now, old)
	}
	if after := settingsDump(t, dataDir); after != before {
		t.Fatalf("settings changed:\n%s\n%s", before, after)
	}
	withStores(t, nil)
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old"); code != 0 || !strings.Contains(out, "1 copied by an earlier run") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
}

// A file missing from the old storage stops the move before the switch,
// naming the attachment and its ticket.
func TestMoveStopsOnAMissingFile(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	before := settingsDump(t, dataDir)
	old := backendStore(t, dataDir, "sqlite", "")
	key := slices.Sorted(maps.Keys(attachmentBytes(t, dataDir, old)))[1]
	if err := old.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 1 || !strings.Contains(e, "is missing from sqlite") || !strings.Contains(e, "-") {
		t.Fatalf("%d %q", code, e)
	}
	if after := settingsDump(t, dataDir); after != before {
		t.Fatal("settings changed")
	}
}

func TestMoveRefusals(t *testing.T) {
	dataDir, _ := movedFrom(t, "dir")
	before := settingsDump(t, dataDir)
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		code int
		want string
	}{
		{"same place", nil, []string{"--to", "dir"}, 1, "Attachments are already stored there."},
		{"same place through VQ_BLOB_DIR", map[string]string{"VQ_BLOB_DIR": filepath.Join(dataDir, "attachments")}, []string{"--to", "dir"}, 1, "already stored there"},
		{"no target", nil, nil, 2, "--to"},
		{"unknown target", nil, []string{"--to", "tape"}, 2, "--to"},
		{"stray argument", nil, []string{"--to", "sqlite", "VAQ-1"}, 2, "vaquero attachments move"},
		{"other VQ_BLOB_STORE", map[string]string{"VQ_BLOB_STORE": "s3"}, []string{"--to", "sqlite"}, 1, "VQ_BLOB_STORE is s3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			code, _, e := runCLI(t, append([]string{"attachments", "move"}, tc.args...)...)
			if code != tc.code || !strings.Contains(e, tc.want) {
				t.Fatalf("%d %q", code, e)
			}
		})
	}
	if after := settingsDump(t, dataDir); after != before || movePending(dataDir) {
		t.Fatal("a refusal changed something")
	}
}

func TestMoveRefusesRunningServer(t *testing.T) {
	startBackground(t)
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 1 || !strings.Contains(e, "Stop vaquero first") {
		t.Fatalf("%d %q", code, e)
	}
}

func TestMoveNothingToMove(t *testing.T) {
	dataDir, _ := localEnv(t)
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatal(err)
	}
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 || !strings.Contains(out, "There are no attachments to move") {
		t.Fatalf("%d %q %q", code, out, e)
	}
}

// A database an older Vaquero wrote is upgraded by serve first.
func TestMoveRefusesAnOlderDatabase(t *testing.T) {
	dataDir, _ := localEnv(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "upgrade", "v0.1.0.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "vaquero.db"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 1 || !strings.Contains(e, "older Vaquero") {
		t.Fatalf("%d %q", code, e)
	}
}

// The room for the copies is checked on the target's volume first, and a
// refusal writes nothing. On the data directory's volume it must hold the
// premove snapshot too (the database, grown by the copies when they go
// into it) and appdb.MinBackupMargin.
func TestMoveChecksFreeSpace(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	var total int64
	for _, b := range want {
		total += int64(len(b))
	}
	var asked string
	saved := moveFreeBytes
	t.Cleanup(func() { moveFreeBytes = saved })
	free := func(n int64) { moveFreeBytes = func(p string) (int64, error) { asked = p; return n, nil } }
	savedFS := sameFilesystem
	t.Cleanup(func() { sameFilesystem = savedFS })
	onDataVolume := func(same bool) { sameFilesystem = func(string, string) (bool, error) { return same, nil } }

	// Another volume: the copies alone.
	onDataVolume(false)
	root := filepath.Join(t.TempDir(), "store")
	t.Setenv("VQ_BLOB_DIR", root)
	free(total - 1)
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 1 || !strings.Contains(e, "free in "+root) || asked != root {
		t.Fatalf("%d %q, asked %q", code, e, asked)
	}
	if movePending(dataDir) || dirObjects(t, root) != 0 {
		t.Fatal("the refusal wrote something")
	}

	// The data directory's volume: the copies, the snapshot and the margin.
	onDataVolume(true)
	size := dbSize(t, filepath.Join(dataDir, "vaquero.db"))
	free(total + size + appdb.MinBackupMargin - 1)
	if code, _, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 || !strings.Contains(e, "free in "+root) {
		t.Fatalf("same volume: %d %q", code, e)
	}
	free(total + size + appdb.MinBackupMargin)
	if code, _, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
		t.Fatal(e)
	}

	// Into the database: the snapshot holds the copies as well.
	size = dbSize(t, filepath.Join(dataDir, "vaquero.db"))
	free(total + size + total + appdb.MinBackupMargin - 1)
	if code, _, e := runCLI(t, "attachments", "move", "--to", "sqlite"); code != 1 || !strings.Contains(e, "free in "+dataDir) || asked != dataDir {
		t.Fatalf("sqlite: %d %q, asked %q", code, e, asked)
	}
	free(total + size + total + appdb.MinBackupMargin)
	if code, _, e := runCLI(t, "attachments", "move", "--to", "sqlite"); code != 0 {
		t.Fatal(e)
	}
}

// An interrupted move to one directory is cleaned up by a move to another.
func TestMoveElsewhereCleansAnInterruptedMove(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	first := filepath.Join(t.TempDir(), "first")
	t.Setenv("VQ_BLOB_DIR", first)
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &failingPut{Store: s, n: 2}
		}
		return s
	})
	if code, _, _ := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 {
		t.Fatal("the first run passed")
	}
	withStores(t, nil)
	second := filepath.Join(t.TempDir(), "second")
	t.Setenv("VQ_BLOB_DIR", second)
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 || !strings.Contains(e, "Removed 1 file an interrupted move left in dir "+first) {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if n := dirObjects(t, first); n != 0 {
		t.Fatalf("%d objects left in the first directory", n)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", second)); !maps.Equal(got, want) {
		t.Fatalf("second holds %v", got)
	}
}

// A move killed after the switch, before it deleted the old copies:
// the rerun deletes them; one that didn't ask to delete just finishes.
func TestMoveFinishesAfterTheSwitch(t *testing.T) {
	for _, deleteOld := range []bool{true, false} {
		t.Run(map[bool]string{true: "delete old", false: "keep old"}[deleteOld], func(t *testing.T) {
			dataDir, want := movedFrom(t, "sqlite")
			if code, _, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
				t.Fatal(e)
			}
			// What the killed run's file said.
			p := movePlan{
				Instance:  readSetting(t, dataDir, "instance_id"),
				From:      movePlace{Backend: "sqlite"},
				To:        movePlace{Backend: "dir", Location: "{data dir}/attachments"},
				DeleteOld: deleteOld,
			}
			b, _ := json.Marshal(p)
			if err := os.WriteFile(filepath.Join(dataDir, "move.pending"), append(b, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
			if code != 0 || !strings.Contains(out, "finished") {
				t.Fatalf("%d %q %q", code, out, e)
			}
			left := attachmentBytes(t, dataDir, backendStore(t, dataDir, "sqlite", ""))
			if deleteOld && len(left) != 0 || !deleteOld && !maps.Equal(left, want) {
				t.Fatalf("sqlite holds %v", left)
			}
			if movePending(dataDir) {
				t.Fatal("move.pending stays")
			}
		})
	}
}

// serve says when a move was left half done.
func TestServeWarnsOfAnInterruptedMove(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &failingPut{Store: s, n: 2}
		}
		return s
	})
	if code, _, _ := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 {
		t.Fatal("the move passed")
	}
	cfg := testConfig(t, freeAddr(t), dataDir)
	var log bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.NewTextHandler(&log, nil))) }()
	waitHealthy(t, cfg.Addr)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "interrupted attachments move") {
		t.Fatalf("%s", log.String())
	}
}

func TestMoveJSON(t *testing.T) {
	movedFrom(t, "sqlite")
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--json")
	var res moveResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Moved != 2 || res.Copied != 2 || res.Backend != "dir" {
		t.Fatalf("%d %q %q", code, out, e)
	}
}

// undeletable refuses every delete.
type undeletable struct{ blobs.Store }

func (undeletable) Delete(context.Context, string) error { return errors.New("read-only") }

// A delete refused after the switch: the move stands, says so, and keeps
// its record, so a rerun deletes the old copies.
func TestMoveDeleteFailureAfterTheSwitch(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "sqlite" {
			return undeletable{s}
		}
		return s
	})
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 1 || !strings.Contains(e, "Moved 2 attachments to dir, but the old copies in sqlite couldn't all be deleted (read-only)") {
		t.Fatalf("%d %q", code, e)
	}
	if s := readSetting(t, dataDir, "blob_store"); s != "dir" || !movePending(dataDir) {
		t.Fatalf("blob_store %q, pending %v", s, movePending(dataDir))
	}
	if err := bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "dir"}); err != nil {
		t.Fatal(err)
	}
	withStores(t, nil)
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir")
	if code != 0 || !strings.Contains(out, "finished") || !strings.Contains(out, "Deleted the old copies from sqlite") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if left := objectKeys(t, backendStore(t, dataDir, "sqlite", "")); len(left) != 0 {
		t.Fatalf("left in sqlite: %v", left)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("dir holds %v", got)
	}
}

// premoves lists the premove snapshots in dataDir's backups.
func premoves(t *testing.T, dataDir string) []string {
	t.Helper()
	got, _ := filepath.Glob(filepath.Join(dataDir, "backups", "vaquero-premove-*.sqlite"))
	return got
}

// N-R9: a move saves a premove snapshot after the copies are checked and
// before the switch; the snapshot records the old storage, and backup list
// shows it.
func TestMoveSnapshotsBeforeTheSwitch(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	snaps := premoves(t, dataDir)
	if len(snaps) != 1 || !strings.Contains(out, filepath.Base(snaps[0])) {
		t.Fatalf("snapshots %v, out %q", snaps, out)
	}
	db, err := appdb.OpenReader(snaps[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	var store string
	var blobsIn int
	db.QueryRow(`SELECT value FROM settings WHERE key = 'blob_store'`).Scan(&store)
	db.QueryRow(`SELECT count(*) FROM blobs WHERE key LIKE 'attachments/%'`).Scan(&blobsIn)
	db.Close()
	if store != "sqlite" || blobsIn != 2 {
		t.Fatalf("the snapshot records %q with %d files", store, blobsIn)
	}
	if code, out, _ := runCLI(t, "backup", "list"); code != 0 || !strings.Contains(out, filepath.Base(snaps[0])) {
		t.Fatalf("backup list: %d %q", code, out)
	}
	// Restoring it undoes the move, bytes included (they were in the
	// database), and serve boots on sqlite again.
	if code, _, e := runCLI(t, "backup", "restore", filepath.Base(snaps[0])); code != 0 {
		t.Fatalf("restore: %q", e)
	}
	if s := readSetting(t, dataDir, "blob_store"); s != "sqlite" {
		t.Fatalf("restored blob_store %q", s)
	}
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatal(err)
	}
}

// N-R9: a move whose snapshot fails switches nothing and, with
// --delete-old, deletes nothing.
func TestMoveSnapshotFailureSwitchesNothing(t *testing.T) {
	dataDir, want := movedFrom(t, "sqlite")
	before := settingsDump(t, dataDir)
	saved := moveSnapshot
	moveSnapshot = func(context.Context, *sql.DB, string, appdb.Kind, time.Time) (appdb.BackupInfo, error) {
		return appdb.BackupInfo{}, errors.New("disk full")
	}
	t.Cleanup(func() { moveSnapshot = saved })
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 1 || !strings.Contains(e, "couldn't save a snapshot first") || !strings.Contains(e, "disk full") ||
		!strings.Contains(e, "Attachments are still stored in sqlite") {
		t.Fatalf("%d %q", code, e)
	}
	if after := settingsDump(t, dataDir); after != before {
		t.Fatalf("settings changed:\n%s\n%s", before, after)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "sqlite", "")); !maps.Equal(got, want) {
		t.Fatal("old copies deleted")
	}
	if err := bootOnce(t, dataDir, nil); err != nil {
		t.Fatalf("the old storage must still boot: %v", err)
	}
	moveSnapshot = saved
	if code, out, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old"); code != 0 || !strings.Contains(out, "2 copied by an earlier run") {
		t.Fatalf("%d %q %q", code, out, e)
	}
}

// sameStorageRefusal is the refusal when source and target are one place.
const sameStorageRefusal = "These are the same storage; nothing was moved."

// caseInsensitive skips a test on a case-sensitive filesystem, where a
// case variant of a path is another directory.
func caseInsensitive(t *testing.T, dir string) string {
	t.Helper()
	upper := filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)))
	if _, err := os.Stat(upper); err != nil {
		t.Skipf("%s is on a case-sensitive filesystem, where %s is another directory", dir, upper)
	}
	return upper
}

// N-R10: a VQ_BLOB_DIR that differs from the recorded directory only in
// case (the same directory on macOS's default filesystem) passes
// SameLocation, so only the probe stands between --delete-old and every file.
func TestMoveRefusesACaseVariantDir(t *testing.T) {
	dataDir, want := movedFrom(t, "dir")
	upper := caseInsensitive(t, filepath.Join(dataDir, "attachments"))
	t.Setenv("VQ_BLOB_DIR", upper)
	code, _, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
	if code != 1 || !strings.Contains(e, sameStorageRefusal) {
		t.Fatalf("%d %q", code, e)
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("files lost: %v", got)
	}
	if movePending(dataDir) || len(premoves(t, dataDir)) != 0 {
		t.Fatal("the refusal recorded a move")
	}
}

// N-R10: two bucket names for one store (another endpoint spelling for
// the same service): the probe put into the target is seen through the
// source, so nothing is moved or deleted, and no probe is left.
func TestMoveRefusesAliasedBuckets(t *testing.T) {
	withStores(t, nil)
	dataDir, want := movedFrom(t, "sqlite")
	t.Setenv("VQ_S3_ENDPOINT", "s3.example.test:9000")
	t.Setenv("VQ_S3_BUCKET", "vq")
	t.Setenv("VQ_S3_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "secret")
	if code, _, e := runCLI(t, "attachments", "move", "--to", "s3"); code != 0 {
		t.Fatal(e)
	}
	bucket := fakeS3Store(t, "s3.example.test:9000/vq")
	fakeBuckets.Lock()
	fakeBuckets.dirs["alias.example.test:9000/vq"] = fakeBuckets.dirs["s3.example.test:9000/vq"]
	fakeBuckets.Unlock()
	before := objectKeys(t, bucket)
	t.Setenv("VQ_S3_ENDPOINT", "alias.example.test:9000")
	code, _, e := runCLI(t, "attachments", "move", "--to", "s3", "--delete-old")
	if code != 1 || !strings.Contains(e, sameStorageRefusal) {
		t.Fatalf("%d %q", code, e)
	}
	if now := objectKeys(t, bucket); !slices.Equal(now, before) {
		t.Fatalf("the bucket changed: %v, was %v", now, before)
	}
	if got := attachmentBytes(t, dataDir, bucket); !maps.Equal(got, want) {
		t.Fatalf("files lost: %v", got)
	}
	if s := readSetting(t, dataDir, "blob_location"); s != "s3.example.test:9000/vq" {
		t.Fatalf("switched to %q", s)
	}
}

// N-R10: a record left by an interrupted move to another name for the
// storage in use (here a symlink to it) is never cleaned up by deleting:
// a rerun to elsewhere refuses and deletes nothing. The same holds for a
// record whose old storage is the one in use now.
func TestMoveAliasedLeftoversDeleteNothing(t *testing.T) {
	dataDir, want := movedFrom(t, "dir")
	instance := readSetting(t, dataDir, "instance_id")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Join(dataDir, "attachments"), alias); err != nil {
		t.Fatal(err)
	}
	inUse := movePlace{Backend: "dir", Location: "{data dir}/attachments"}
	other := filepath.Join(t.TempDir(), "other")
	for _, tc := range []struct {
		name string
		plan movePlan
		dir  string
	}{
		{"cleanup of the target", movePlan{Instance: instance, From: inUse, To: movePlace{Backend: "dir", Location: alias}}, other},
		{"finish of the deletes", movePlan{Instance: instance, From: movePlace{Backend: "dir", Location: alias}, To: inUse, DeleteOld: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(tc.plan)
			if err := os.WriteFile(filepath.Join(dataDir, "move.pending"), append(b, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VQ_BLOB_DIR", tc.dir)
			code, _, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
			if code != 1 || !strings.Contains(e, "These are the same storage; nothing was deleted.") {
				t.Fatalf("%d %q", code, e)
			}
			if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
				t.Fatalf("files lost: %v", got)
			}
			if !movePending(dataDir) {
				t.Fatal("the record went")
			}
		})
	}
}

// N-R10: a directory inside the other, either way, is refused.
func TestMoveRefusesNestedDirs(t *testing.T) {
	dataDir, want := movedFrom(t, "dir")
	for _, dir := range []string{
		filepath.Join(dataDir, "attachments", "nested"), // the target inside the source
		dataDir, // the source inside the target
	} {
		t.Setenv("VQ_BLOB_DIR", dir)
		code, _, e := runCLI(t, "attachments", "move", "--to", "dir", "--delete-old")
		if code != 1 || !strings.Contains(e, "is inside") {
			t.Fatalf("%s: %d %q", dir, code, e)
		}
	}
	if got := attachmentBytes(t, dataDir, backendStore(t, dataDir, "dir", "")); !maps.Equal(got, want) {
		t.Fatalf("files lost: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "attachments", "nested")); !os.IsNotExist(err) {
		t.Fatalf("the nested target was made: %v", err)
	}
}

// N-R10: --delete-old deletes only attachments/{instance}/{id} and
// thumbs/{instance}/{id}; anything else there is left and named.
func TestMoveDeleteOldOnlyDeletesAttachmentKeys(t *testing.T) {
	dataDir, _ := movedFrom(t, "dir")
	instance := readSetting(t, dataDir, "instance_id")
	old := backendStore(t, dataDir, "dir", "")
	stray := "attachments/" + instance + "/notes.txt"
	if err := old.Put(t.Context(), stray, strings.NewReader("mine"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	code, out, e := runCLI(t, "attachments", "move", "--to", "sqlite", "--delete-old")
	if code != 0 || !strings.Contains(e, stray) {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if left := objectKeys(t, old); !slices.Equal(left, []string{stray}) {
		t.Fatalf("left %v", left)
	}
}

// A move elsewhere also removes the half-written temporary files a killed
// copy left in the directory it was moving to.
func TestMoveElsewhereRemovesTempFiles(t *testing.T) {
	dataDir, _ := movedFrom(t, "sqlite")
	first := filepath.Join(t.TempDir(), "first")
	t.Setenv("VQ_BLOB_DIR", first)
	withStores(t, func(cfg config.Config, s blobs.Store) blobs.Store {
		if cfg.BlobBackend == "dir" {
			return &failingPut{Store: s, n: 2}
		}
		return s
	})
	if code, _, _ := runCLI(t, "attachments", "move", "--to", "dir"); code != 1 {
		t.Fatal("the first run passed")
	}
	instance := readSetting(t, dataDir, "instance_id")
	tmp := filepath.Join(first, "attachments", instance, ".tmp-01abc-123")
	if err := os.WriteFile(tmp, []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	withStores(t, nil)
	t.Setenv("VQ_BLOB_DIR", filepath.Join(t.TempDir(), "second"))
	if code, _, e := runCLI(t, "attachments", "move", "--to", "dir"); code != 0 {
		t.Fatal(e)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temporary file left: %v", err)
	}
}

// A move never deletes its own probe's leftovers as an attachment's.
func TestMoveProbeKeyIsNotDeletable(t *testing.T) {
	if deletable("attachments/i/move-probe-"+strings.ToLower(idgen.New()), "i") {
		t.Fatal("a probe key is deletable")
	}
}

// probeStore is a store whose Stat and Delete answer as told: the probe's
// own errors, for distinct.
type probeStore struct {
	blobs.Store
	statErr, delErr error
}

func (s probeStore) Stat(ctx context.Context, key string) (int64, error) {
	if s.statErr != nil {
		return 0, s.statErr
	}
	return s.Store.Stat(ctx, key)
}

func (s probeStore) Delete(ctx context.Context, key string) error {
	if s.delErr != nil {
		return s.delErr
	}
	return s.Store.Delete(ctx, key)
}

// A probe that can't be deleted is named, whatever else the probe found:
// one store, or a target that can't be read.
func TestMoveNamesAProbeLeftBehind(t *testing.T) {
	dir := t.TempDir()
	base, err := blobs.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: t.TempDir()}
	pa := movePlace{Backend: "dir", Location: dir}
	pb := movePlace{Backend: "s3", Location: "s3.example.test/bucket"}
	for _, c := range []struct {
		name string
		b    blobs.Store
		want string
	}{
		{"same storage", base, "These are the same storage; nothing was moved."},
		{"stat fails", probeStore{Store: base, statErr: errors.New("timeout")}, "can't read s3 s3.example.test/bucket (timeout), so nothing was moved"},
	} {
		a := probeStore{Store: base, delErr: errors.New("read-only")}
		err := distinct(t.Context(), cfg, a, pa, c.b, pb, "inst", "moved")
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "attachments/inst/move-probe-") ||
			!strings.Contains(err.Error(), "by hand") {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}
