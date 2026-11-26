package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/archive"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/core/coreseed"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/idgen"
)

// importNow is the fixed clock of the seed and of both exports.
var importNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// sourceInstance names the seeded database's blob keys.
const sourceInstance = "01jx00000000000000000source"

func importHeader(t *testing.T, schema int64) archive.Header {
	t.Helper()
	if schema == 0 {
		var err error
		if schema, err = appdb.Newest(); err != nil {
			t.Fatal(err)
		}
	}
	return archive.NewHeader(schema, importNow, "1.2.3")
}

// seededArchive is the export of a database filled by coreseed, with
// export.json passed through edit (nil keeps it), written to a file.
func seededArchive(t *testing.T, h archive.Header, edit func(string) string) string {
	t.Helper()
	d := dbtest.New(t)
	clock := func() time.Time { return importNow }
	c := core.New(d.W, clock)
	c.BlobStore = "sqlite"
	store := blobs.NewSQLite(d.W, d.R, clock)
	if err := coreseed.Seed(t.Context(), c, d.Q, store, sourceInstance, importNow); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := archive.Write(t.Context(), &buf, d.R, store, h); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if edit != nil {
		b = rewriteExportJSON(t, b, edit)
	}
	file := filepath.Join(t.TempDir(), "export.tar.gz")
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// rewriteExportJSON passes an archive's export.json, or (with entry) one
// attachment entry, through edit.
func rewriteExportJSON(t *testing.T, b []byte, edit func(string) string) []byte {
	t.Helper()
	return rewriteEntry(t, b, func(name string, data []byte) []byte {
		if name == "export.json" {
			return []byte(edit(string(data)))
		}
		return data
	})
}

// rewriteEntry rebuilds an archive with every entry passed through edit.
func rewriteEntry(t *testing.T, b []byte, edit func(name string, data []byte) []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	tw := tar.NewWriter(zw)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		data = edit(h.Name, data)
		h.Size = int64(len(data))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	tw.Close()
	zw.Close()
	return out.Bytes()
}

// importedFiles lists what an import may leave behind in dataDir.
func importLeftovers(t *testing.T, dataDir string) []string {
	t.Helper()
	got, _ := filepath.Glob(filepath.Join(dataDir, "vaquero.import.db*"))
	spools, _ := filepath.Glob(filepath.Join(dataDir, "import-spool-*"))
	pending, _ := filepath.Glob(filepath.Join(dataDir, "import.pending"))
	return append(append(got, spools...), pending...)
}

// dirObjects counts the objects in a dir store's root.
func dirObjects(t *testing.T, root string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

const importTail = ". Profiles holding a token for the old instance need a new one: vaquero token create.\n"

func TestRoundTrip(t *testing.T) {
	for _, backend := range []string{"sqlite", "dir"} {
		t.Run(backend, func(t *testing.T) {
			dataDir, _ := localEnv(t) // a data directory that doesn't exist yet
			t.Setenv("VQ_BLOB_STORE", backend)
			h := importHeader(t, 0)
			file := seededArchive(t, h, nil)
			// The spool goes under the data directory, never the system's
			// temporary one (often a small tmpfs); one an interrupted
			// import left is removed.
			t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			stale := filepath.Join(dataDir, "import-spool-123", "0")
			if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, e := runCLI(t, "import", file)
			if code != 0 {
				t.Fatalf("%d %q", code, e)
			}
			if want := "Imported 2 projects, 9 tickets and 2 attachments into " + backend + importTail; out != want {
				t.Fatalf("got %q, want %q", out, want)
			}
			if left := importLeftovers(t, dataDir); len(left) != 0 {
				t.Fatalf("left %v", left)
			}

			dbPath := filepath.Join(dataDir, "vaquero.db")
			w, err := appdb.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			r, err := appdb.OpenReader(dbPath, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			var instance, stored string
			r.QueryRow(`SELECT value FROM settings WHERE key = 'instance_id'`).Scan(&instance)
			r.QueryRow(`SELECT value FROM settings WHERE key = 'blob_store'`).Scan(&stored)
			if instance == "" || instance == sourceInstance || instance != strings.ToLower(instance) || stored != backend {
				t.Fatalf("instance %q, blob_store %q", instance, stored)
			}
			var wrongKeys int
			r.QueryRow(`SELECT count(*) FROM attachments WHERE blob_key <> 'attachments/' || ? || '/' || lower(id)`, instance).Scan(&wrongKeys)
			if wrongKeys != 0 {
				t.Fatalf("%d attachments keyed outside the new instance", wrongKeys)
			}
			var store blobs.Store = blobs.NewSQLite(w, r, time.Now)
			if backend == "dir" {
				root := filepath.Join(dataDir, "attachments")
				if store, err = blobs.NewDir(root); err != nil {
					t.Fatal(err)
				}
				if n := dirObjects(t, filepath.Join(root, "attachments", instance)); n != 2 {
					t.Fatalf("%d objects in the dir store", n)
				}
				var inDB int
				r.QueryRow(`SELECT count(*) FROM blobs`).Scan(&inDB)
				if inDB != 0 {
					t.Fatalf("%d blobs in the database", inDB)
				}
			}
			var again bytes.Buffer
			if err := archive.Write(t.Context(), &again, r, store, h); err != nil {
				t.Fatal(err)
			}
			first, _ := os.ReadFile(file)
			if !bytes.Equal(first, again.Bytes()) {
				t.Fatal("the export of the imported instance differs from the archive")
			}
		})
	}
}

func TestImportRefusesNonEmpty(t *testing.T) {
	dataDir := seedData(t) // one project, VAQ
	file := seededArchive(t, importHeader(t, 0), nil)
	code, _, e := runCLI(t, "import", file)
	if want := "Import needs an empty instance; this one has 1 project and no tickets."; code != 1 || !strings.Contains(e, want) {
		t.Fatalf("%d %q, want %q", code, e, want)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
	if _, out, _ := runCLI(t, "projects"); !strings.Contains(out, "VAQ") || strings.Contains(out, "OLD") {
		t.Fatalf("projects:\n%s", out)
	}
}

func TestImportRefusesNewerSchema(t *testing.T) {
	dataDir, _ := localEnv(t)
	newest, _ := appdb.Newest()
	file := seededArchive(t, importHeader(t, newest+1), nil)
	code, _, e := runCLI(t, "import", file)
	want := "This export is from a newer vaquero (schema " + strconv.FormatInt(newest+1, 10) + "). Upgrade vaquero and import again."
	if code != 1 || !strings.Contains(e, want) {
		t.Fatalf("%d %q, want %q", code, e, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "vaquero.db")); !os.IsNotExist(err) {
		t.Fatalf("a database was created: %v", err)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestImportRefusesUnknownColumn(t *testing.T) {
	dataDir, _ := localEnv(t)
	file := seededArchive(t, importHeader(t, 0), func(s string) string {
		return strings.Replace(s, `"title":`, `"colour":"red","title":`, 1)
	})
	code, _, e := runCLI(t, "import", file)
	want := "The export's tickets table has a column called colour, which this vaquero doesn't know."
	if code != 1 || !strings.Contains(e, want) {
		t.Fatalf("%d %q, want %q", code, e, want)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "vaquero.db")); !os.IsNotExist(err) {
		t.Fatalf("a database was created: %v", err)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// oldState is what a failed import must leave as it was.
func oldState(t *testing.T, dataDir string) string {
	t.Helper()
	tok, err := os.ReadFile(filepath.Join(dataDir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	return settingsDump(t, dataDir) + tableCounts(t, dataDir, "actors", "api_tokens", "events", "projects") + string(tok)
}

func TestImportFailureLeavesOldDatabase(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(name string, data []byte) []byte
		want string
	}{
		// Refused while reading, before anything is stored.
		{"corrupt attachment entry", func(name string, data []byte) []byte {
			if strings.HasPrefix(name, "attachments/") {
				return data[:len(data)-1]
			}
			return data
		}, "bytes, but its record says"},
		// Refused at commit, after both files are stored.
		{"attachment on a missing ticket", func(name string, data []byte) []byte {
			if name != "export.json" {
				return data
			}
			s := string(data)
			i := strings.Index(s, `"attachments":[`)
			return []byte(s[:i] + strings.Replace(s[i:], `"ticket_id":"`, `"ticket_id":"missing-`, 1))
		}, "FOREIGN KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VQ_BLOB_STORE", "dir")
			dataDir := startBackground(t)
			if code, _, e := runCLI(t, "stop"); code != 0 {
				t.Fatal(e)
			}
			before := oldState(t, dataDir)
			src := seededArchive(t, importHeader(t, 0), nil)
			b, _ := os.ReadFile(src)
			if err := os.WriteFile(src, rewriteEntry(t, b, tc.edit), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, e := runCLI(t, "import", src)
			if code != 1 || !strings.Contains(e, tc.want) || !strings.Contains(e, "Nothing was imported.") {
				t.Fatalf("%d %q, want %q", code, e, tc.want)
			}
			if after := oldState(t, dataDir); after != before {
				t.Fatalf("the old database changed:\n%s\n%s", before, after)
			}
			if left := importLeftovers(t, dataDir); len(left) != 0 {
				t.Fatalf("left %v", left)
			}
			if n := dirObjects(t, filepath.Join(dataDir, "attachments")); n != 0 {
				t.Fatalf("%d objects left in the dir store", n)
			}
			if code, out, e := runCLI(t, "projects"); code != 0 || strings.TrimSpace(out) != "KEY  NAME  OPEN" {
				t.Fatalf("the old database doesn't read: %q %q", out, e)
			}
		})
	}
}

func TestImportRefusesRunningServer(t *testing.T) {
	dataDir := startBackground(t)
	file := seededArchive(t, importHeader(t, 0), nil)
	code, _, e := runCLI(t, "import", file)
	if code != 1 || !strings.Contains(e, "Stop vaquero first: vaquero stop") {
		t.Fatalf("%d %q", code, e)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// Review Focus 5: import writes to dir from the shell, then the server
// starts with sqlite (as brew services would, with its own environment).
func TestImportThenServeWithOtherBackendRefuses(t *testing.T) {
	dataDir, _ := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "dir")
	file := seededArchive(t, importHeader(t, 0), nil)
	if code, _, e := runCLI(t, "import", file); code != 0 {
		t.Fatal(e)
	}
	before := settingsDump(t, dataDir) + tableCounts(t, dataDir, "actors", "api_tokens", "events", "blobs")
	err := bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "sqlite"})
	want := "Attachments are stored in dir; VQ_BLOB_STORE is sqlite. Set it back to dir."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%v", err)
	}
	if after := settingsDump(t, dataDir) + tableCounts(t, dataDir, "actors", "api_tokens", "events", "blobs"); after != before {
		t.Fatalf("the refused boot changed the database:\n%s\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "token")); !os.IsNotExist(err) {
		t.Fatal("the refused boot wrote a token")
	}
	if err := bootOnce(t, dataDir, map[string]string{"VQ_BLOB_STORE": "dir"}); err != nil {
		t.Fatalf("the backend import wrote to must boot: %v", err)
	}
}

// serving runs serve on dataDir with env until fn returns.
func serving(t *testing.T, dataDir string, env map[string]string, fn func(base string)) {
	t.Helper()
	addr := freeAddr(t)
	cfg, err := config.FromEnv(func(k string) string {
		switch k {
		case "VQ_ADDR":
			return addr
		case "VQ_DATA_DIR":
			return dataDir
		}
		return env[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.DiscardHandler)) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitHealthy(t, addr)
	fn("http://" + addr)
}

func statusWith(t *testing.T, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestImportMintsNewToken(t *testing.T) {
	dataDir := startBackground(t)
	if code, _, e := runCLI(t, "stop"); code != 0 {
		t.Fatal(e)
	}
	tokenFile := filepath.Join(dataDir, "token")
	old, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	file := seededArchive(t, importHeader(t, 0), nil)
	code, out, e := runCLI(t, "import", file)
	if code != 0 {
		t.Fatal(e)
	}
	// The served database before it was snapshotted, and the output says so.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	bs, _ := appdb.ListBackups(filepath.Join(dataDir, "backups"))
	if len(lines) != 2 || len(bs) != 1 || lines[1] != "The database before it is saved as "+bs[0].Name+"." {
		t.Fatalf("output %q, backups %v", out, bs)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("import kept the old token file: %v", err)
	}
	serving(t, dataDir, nil, func(base string) {
		fresh, err := os.ReadFile(tokenFile)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(fresh, old) {
			t.Fatal("serve wrote the old token again")
		}
		url := base + "/api/v1/projects"
		if got := statusWith(t, url, strings.TrimSpace(string(old))); got != http.StatusUnauthorized {
			t.Errorf("old token: %d", got)
		}
		if got := statusWith(t, url, strings.TrimSpace(string(fresh))); got != http.StatusOK {
			t.Errorf("new token: %d", got)
		}
	})
	// The new token belongs to the imported owner, not to a second one.
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var owners int
	db.QueryRow(`SELECT count(*) FROM actors WHERE kind = 'human' AND name = ?`, core.OwnerName).Scan(&owners)
	if owners != 1 {
		t.Fatalf("%d owners", owners)
	}
}

// A store that can't be opened stops the import before anything is written.
func TestImportRefusesBadStore(t *testing.T) {
	dataDir, _ := localEnv(t)
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VQ_BLOB_STORE", "dir")
	t.Setenv("VQ_BLOB_DIR", notDir)
	file := seededArchive(t, importHeader(t, 0), nil)
	code, _, e := runCLI(t, "import", file)
	if code != 1 || !strings.Contains(e, "attachment store") || !strings.Contains(e, "Nothing was imported.") {
		t.Fatalf("%d %q", code, e)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "vaquero.db")); !os.IsNotExist(err) {
		t.Fatalf("a database was created: %v", err)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// deniedStore is a bucket whose credentials are wrong: every call is refused,
// as S3 answers AccessDenied. puts counts the Put calls.
type deniedStore struct {
	blobs.Store
	puts int
}

func (d *deniedStore) Put(context.Context, string, io.Reader, int64, string) error {
	d.puts++
	return errors.New("s3 put: Access Denied.")
}

func (d *deniedStore) Delete(context.Context, string) error {
	return errors.New("s3 delete: Access Denied.")
}

// Bad S3 credentials stop the import at once, before the archive is unpacked
// or a database built, rather than at the first attachment's Put.
func TestImportChecksStoreAccessFirst(t *testing.T) {
	dataDir, _ := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "s3")
	t.Setenv("VQ_S3_ENDPOINT", "s3.invalid")
	t.Setenv("VQ_S3_BUCKET", "b")
	t.Setenv("VQ_S3_ACCESS_KEY_ID", "wrong")
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "wrong")
	denied := &deniedStore{}
	saved := openBlobStore
	openBlobStore = func(config.Config, *sql.DB, *sql.DB, func() time.Time) (blobs.Store, error) { return denied, nil }
	t.Cleanup(func() { openBlobStore = saved })
	file := seededArchive(t, importHeader(t, 0), nil)
	code, _, e := runCLI(t, "import", file)
	if code != 1 || !strings.Contains(e, "attachment store: can't write to it (s3 put: Access Denied.)") || !strings.Contains(e, "Nothing was imported.") {
		t.Fatalf("%d %q", code, e)
	}
	if denied.puts != 1 {
		t.Fatalf("%d puts, want only the probe", denied.puts)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "vaquero.db")); !os.IsNotExist(err) {
		t.Fatalf("a database was created: %v", err)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// A swap that fails after the old database was moved or saved says where
// it is and how to put it back, never that nothing changed.
func TestSwapFailureNamesTheOldDatabase(t *testing.T) {
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name string
		old  func(path string)
		want func(cfg config.Config, aside string) string
	}{
		{"moved aside", func(path string) {
			if err := os.WriteFile(path, []byte("this is not a database"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, func(cfg config.Config, aside string) string {
			return "The database before it is at " + aside + ", with any " + aside + "-wal and " + aside + "-shm files; move them back to " +
				cfg.DBPath + ", " + cfg.DBPath + "-wal and " + cfg.DBPath + "-shm to undo."
		}},
		{"snapshot", func(path string) {
			d, err := appdb.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Exec("CREATE TABLE x (y)"); err != nil {
				t.Fatal(err)
			}
			d.Close()
		}, func(cfg config.Config, aside string) string {
			name := filepath.Base(aside)
			return "The database before it is saved as " + name + "; vaquero backup restore " + name + " puts it back."
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := testConfig(t, freeAddr(t), dir)
			tc.old(cfg.DBPath)
			missing := filepath.Join(dir, importFile) // never made, so the rename fails
			aside, err := swapDatabase(t.Context(), cfg, missing)
			if err == nil || aside == "" {
				t.Fatalf("%q %v", aside, err)
			}
			if want := tc.want(cfg, aside); !strings.Contains(err.Error(), want) {
				t.Fatalf("got %q, want %q", err, want)
			}
			if _, serr := os.Stat(aside); serr != nil {
				t.Fatalf("the old database isn't at %s: %v", aside, serr)
			}
			if msg := (notImported{err}).Error(); strings.Contains(msg, "Nothing was imported") {
				t.Fatalf("%q", msg)
			}
		})
	}
}

// undeletableStore takes a Put and refuses every Delete; keys records what
// was put.
type undeletableStore struct {
	blobs.Store
	keys []string
}

func (u *undeletableStore) Put(_ context.Context, key string, _ io.Reader, _ int64, _ string) error {
	u.keys = append(u.keys, key)
	return nil
}

func (u *undeletableStore) Delete(context.Context, string) error {
	return errors.New("s3 delete: Access Denied.")
}

// VAQ-108: a store that takes the probe but refuses its delete stops the
// import naming the object to remove, and a retry puts the same key, not
// another under a fresh instance.
func TestImportProbeLeftNamesItsKey(t *testing.T) {
	localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "s3")
	t.Setenv("VQ_S3_ENDPOINT", "s3.invalid")
	t.Setenv("VQ_S3_BUCKET", "b")
	t.Setenv("VQ_S3_ACCESS_KEY_ID", "k")
	t.Setenv("VQ_S3_SECRET_ACCESS_KEY", "s")
	store := &undeletableStore{}
	saved := openBlobStore
	openBlobStore = func(config.Config, *sql.DB, *sql.DB, func() time.Time) (blobs.Store, error) { return store, nil }
	t.Cleanup(func() { openBlobStore = saved })
	file := seededArchive(t, importHeader(t, 0), nil)
	for range 2 {
		code, _, e := runCLI(t, "import", file)
		if code != 1 || !strings.Contains(e, "can't delete from it (s3 delete: Access Denied.)") ||
			!strings.Contains(e, "remove "+importProbeKey+" from it by hand") {
			t.Fatalf("%d %q", code, e)
		}
	}
	if len(store.keys) != 2 || store.keys[0] != importProbeKey || store.keys[1] != importProbeKey {
		t.Fatalf("probes put %v, want %s twice", store.keys, importProbeKey)
	}
}

// VAQ-108: the bytes an import stores land on the data directory's volume
// twice (the spool, then the new database or a store directory on the same
// volume), and the rows export.json holds become the new database: the
// free-space check counts all of it up front.
func TestImportCopies(t *testing.T) {
	dataDir := t.TempDir()
	for _, tc := range []struct {
		backend, blobDir string
		want             int
	}{
		{"sqlite", "", 2},
		{"dir", filepath.Join(dataDir, "attachments"), 2},
		{"s3", "", 1},
	} {
		cfg := config.Config{DataDir: dataDir, BlobBackend: tc.backend, BlobDir: tc.blobDir}
		if got := importCopies(cfg); got != tc.want {
			t.Errorf("%s: %d copies, want %d", tc.backend, got, tc.want)
		}
	}
	old := sameFilesystem
	sameFilesystem = func(a, b string) (bool, error) { return false, nil }
	t.Cleanup(func() { sameFilesystem = old })
	if got := importCopies(config.Config{DataDir: dataDir, BlobBackend: "dir", BlobDir: "/elsewhere"}); got != 1 {
		t.Errorf("dir on another volume: %d copies, want 1", got)
	}
}

// leftBehind is what an import killed after its first put leaves: objects
// under an instance no database records, and import.pending naming it.
func leftBehind(t *testing.T, dataDir, root, instance string) blobs.Store {
	t.Helper()
	store, err := blobs.NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01jkilled000000000000000001", "01jkilled000000000000000002"} {
		if err := store.Put(t.Context(), blobs.AttachmentKey(instance, id), strings.NewReader("bytes"), 5, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Config{DataDir: dataDir, BlobBackend: "dir", BlobDir: root}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePending(cfg, instance); err != nil {
		t.Fatal(err)
	}
	return store
}

// VAQ-87: the next import deletes what a killed import left, and the file
// naming it.
func TestImportCleansUpAKilledImport(t *testing.T) {
	dataDir, _ := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "dir")
	root := filepath.Join(dataDir, "attachments")
	killed := strings.ToLower(idgen.New())
	leftBehind(t, dataDir, root, killed)
	file := seededArchive(t, importHeader(t, 0), nil)
	code, out, e := runCLI(t, "import", file)
	if code != 0 || !strings.Contains(e, "Removed 2 files an interrupted import left in dir.") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if n := dirObjects(t, filepath.Join(root, "attachments", killed)); n != 0 {
		t.Fatalf("%d objects left", n)
	}
	if n := dirObjects(t, root); n != 2 {
		t.Fatalf("%d objects in the store, want the import's 2", n)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// VAQ-87: so does the next serve; and the instance the database records is
// never deleted, whatever the file says.
func TestServeCleansUpAKilledImport(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(t.TempDir(), "store")
	env := map[string]string{"VQ_BLOB_STORE": "dir", "VQ_BLOB_DIR": root}
	if err := bootOnce(t, dir, env); err != nil {
		t.Fatal(err)
	}
	leftBehind(t, dir, root, strings.ToLower(idgen.New()))
	if err := bootOnce(t, dir, env); err != nil {
		t.Fatal(err)
	}
	if n := dirObjects(t, root); n != 0 {
		t.Fatalf("%d objects left", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "import.pending")); !os.IsNotExist(err) {
		t.Fatalf("import.pending stays: %v", err)
	}
	live := readSetting(t, dir, "instance_id")
	leftBehind(t, dir, root, live) // a finished import whose file outlived it
	if err := bootOnce(t, dir, env); err != nil {
		t.Fatal(err)
	}
	if n := dirObjects(t, root); n != 2 {
		t.Fatalf("the live instance lost objects: %d left", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "import.pending")); !os.IsNotExist(err) {
		t.Fatalf("import.pending stays: %v", err)
	}
}

// pendingCheck is a dir store that records, at each attachment put,
// whether import.pending named the instance by then.
type pendingCheck struct {
	blobs.Store
	dataDir string
	named   []bool
}

func (p *pendingCheck) Put(ctx context.Context, key string, r io.Reader, size int64, ctype string) error {
	if key != importProbeKey {
		b, _ := os.ReadFile(filepath.Join(p.dataDir, "import.pending"))
		parts := strings.Split(key, "/")
		p.named = append(p.named, len(parts) == 3 && strings.Contains(string(b), `"instance":"`+parts[1]+`"`) && strings.Contains(string(b), `"backend":"dir"`))
	}
	return p.Store.Put(ctx, key, r, size, ctype)
}

// VAQ-87: the file names the instance and backend before the first put,
// and goes once the import is done.
func TestImportRecordsPendingBeforeItsPuts(t *testing.T) {
	dataDir, _ := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "dir")
	check := &pendingCheck{dataDir: dataDir}
	saved := openBlobStore
	openBlobStore = func(cfg config.Config, w, r *sql.DB, now func() time.Time) (blobs.Store, error) {
		s, err := saved(cfg, w, r, now)
		check.Store = s
		return check, err
	}
	t.Cleanup(func() { openBlobStore = saved })
	file := seededArchive(t, importHeader(t, 0), nil)
	if code, _, e := runCLI(t, "import", file); code != 0 {
		t.Fatal(e)
	}
	if len(check.named) != 2 || !check.named[0] || !check.named[1] {
		t.Fatalf("pending named the instance at the puts: %v", check.named)
	}
	if left := importLeftovers(t, dataDir); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

// refusingPrefix is a store that refuses deletes under one prefix.
type refusingPrefix struct {
	blobs.Store
	prefix string
}

func (r refusingPrefix) Delete(ctx context.Context, key string) error {
	if strings.HasPrefix(key, r.prefix) {
		return errors.New("delete refused")
	}
	return r.Store.Delete(ctx, key)
}

// VAQ-87 review: a killed import's files that can't be deleted stop the next
// import, which names them and keeps the record, rather than writing over it.
func TestImportStopsWhenCleanupFails(t *testing.T) {
	dataDir, _ := localEnv(t)
	t.Setenv("VQ_BLOB_STORE", "dir")
	root := filepath.Join(dataDir, "attachments")
	killed := strings.ToLower(idgen.New())
	leftBehind(t, dataDir, root, killed)
	saved := openBlobStore
	openBlobStore = func(cfg config.Config, w, r *sql.DB, now func() time.Time) (blobs.Store, error) {
		s, err := saved(cfg, w, r, now)
		return refusingPrefix{s, "attachments/" + killed + "/"}, err
	}
	t.Cleanup(func() { openBlobStore = saved })
	before, _ := os.ReadFile(filepath.Join(dataDir, "import.pending"))
	code, _, e := runCLI(t, "import", seededArchive(t, importHeader(t, 0), nil))
	if code != 1 || !strings.Contains(e, "an interrupted import left files under attachments/"+killed+"/") ||
		!strings.Contains(e, "remove them by hand, then delete "+filepath.Join(dataDir, "import.pending")) || !strings.Contains(e, "Nothing was imported.") {
		t.Fatalf("%d %q", code, e)
	}
	if after, _ := os.ReadFile(filepath.Join(dataDir, "import.pending")); string(after) != string(before) {
		t.Fatalf("the record changed: %q, was %q", after, before)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "vaquero.db")); !os.IsNotExist(err) {
		t.Fatalf("a database was made: %v", err)
	}
}

// VAQ-87 review: a record naming something other than an instance id
// deletes nothing.
func TestCleanPendingRefusesABadInstance(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "attachments")
	store, err := blobs.NewDir(root)
	if err != nil {
		t.Fatal(err)
	}
	live := strings.ToLower(idgen.New())
	if err := store.Put(t.Context(), blobs.AttachmentKey(live, strings.ToLower(idgen.New())), strings.NewReader("x"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dataDir, BlobBackend: "dir", BlobDir: root}
	for _, bad := range []string{"", "..", live[:10], strings.ToUpper(live), "x/../" + live} {
		rec, _ := json.Marshal(pendingImport{Instance: bad, Backend: "dir", Location: blobs.Location(cfg)})
		if err := os.WriteFile(filepath.Join(dataDir, pendingFile), rec, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := cleanPending(t.Context(), cfg, store, "other"); err == nil || !strings.Contains(err.Error(), "isn't an instance id") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if n := dirObjects(t, root); n != 1 {
		t.Fatalf("%d objects left, want 1", n)
	}
}
