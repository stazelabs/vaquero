package handlers_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/config/configtest"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// snapAt is a fixed instant for snapshot names in these tests.
var snapAt = time.Date(2026, 11, 20, 9, 30, 5, 0, time.UTC)

// snap takes a real snapshot of e's database, of kind k at t.
func snap(t *testing.T, e *webtest.Env, k appdb.Kind, at time.Time) appdb.BackupInfo {
	t.Helper()
	info, err := appdb.SnapshotNext(bg, e.Deps.RDB, e.Deps.BackupDir, k, at)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// VAQ-150: each snapshot with its kind, date, size and schema version,
// newest first; the backups volume's free space; and the exact restore
// command on each row.
func TestBackupsPageLists(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	if r := c.Get("/admin/backups"); r.Status != 200 || !strings.Contains(r.Body, "No snapshots yet.") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	older := snap(t, e, appdb.KindPreDelete, snapAt)
	newer := snap(t, e, appdb.KindPreMigrate, snapAt.Add(time.Hour))
	schema, err := appdb.Version(e.Deps.RDB)
	if err != nil {
		t.Fatal(err)
	}
	page := c.Get("/admin/backups").Body
	contains(t, page,
		"Before a project delete", "Before an upgrade",
		"2026-11-20 09:30 UTC", "2026-11-20 10:30 UTC",
		units.SizeWords(older.Size),
		"Schema "+strconv.FormatInt(schema, 10),
		"vaquero backup restore "+older.Name, "vaquero backup restore "+newer.Name,
		"free on the backups volume", e.Deps.BackupDir,
		`href="/admin/backups"`)
	if i, j := strings.Index(page, newer.Name), strings.Index(page, older.Name); i < 0 || j < 0 || i > j {
		t.Fatal("not newest first")
	}
	free, err := appdb.FreeBytes(e.Deps.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	// Free space moves while the test runs; the page shows it in words.
	if !strings.Contains(page, " free on the backups volume") || free <= 0 {
		t.Fatalf("free %d", free)
	}
	// The restore needs the server stopped, and the page says how.
	contains(t, page, "vaquero stop")
	// The admin side list links the page.
	contains(t, c.Get("/admin").Body, `href="/admin/backups"`)
}

// Snapshot now saves a manual snapshot, pruned like the others, and says
// so in a toast. No event is written: a snapshot isn't a tracker change.
func TestSnapshotNow(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	seq, _ := e.Deps.R.MaxEventSeq(bg)
	r := c.Post("/admin/backups/snapshot", nil)
	if r.Status != http.StatusSeeOther || r.Location != "/admin/backups" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	want := appdb.BackupNameKind(appdb.KindManual, webtest.Start)
	page := c.Follow(r).Body
	contains(t, page, "Snapshot saved: "+want+".", "Taken from this page", "vaquero backup restore "+want)
	list, _ := appdb.ListBackups(e.Deps.BackupDir)
	if len(list) != 1 || list[0].Kind != appdb.KindManual {
		t.Fatalf("%+v", list)
	}
	if after, _ := e.Deps.R.MaxEventSeq(bg); after != seq {
		t.Fatalf("a snapshot wrote an event: %v → %v", seq, after)
	}
	// Pruning applies to manual snapshots: at most BackupKeep stay.
	for i := 1; i <= appdb.BackupKeep+2; i++ {
		e.Deps.Now = func() time.Time { return webtest.Start.Add(time.Duration(i) * time.Minute) }
		e.Rebuild()
		if r := c.Post("/admin/backups/snapshot", nil); r.Status != http.StatusSeeOther {
			t.Fatalf("%d", r.Status)
		}
	}
	list, _ = appdb.ListBackups(e.Deps.BackupDir)
	if len(list) != appdb.BackupKeep {
		t.Fatalf("%d manual snapshots kept", len(list))
	}
	// The directory can't be made: nothing saved, and the toast says so.
	e.Deps.Now = func() time.Time { return webtest.Start.Add(time.Hour) }
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.Deps.BackupDir = blocked // a file: no directory can be made there
	e.Rebuild()
	if r := c.Post("/admin/backups/snapshot", nil); r.Status != http.StatusSeeOther ||
		!strings.Contains(c.Follow(r).Body, "Couldn&#39;t save a snapshot in "+blocked+".") {
		t.Fatalf("%d", r.Status)
	}
}

// Deleting a snapshot asks first (the confirm dialog) and removes only it.
func TestDeleteBackup(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	keep := snap(t, e, appdb.KindPreDelete, snapAt)
	gone := snap(t, e, appdb.KindManual, snapAt.Add(time.Minute))
	page := c.Get("/admin/backups").Body
	contains(t, page, `action="/admin/backups/`+gone.Name+`/delete"`, `data-confirm="Delete `+gone.Name+`? It can&#39;t be undone."`)
	seq, _ := e.Deps.R.MaxEventSeq(bg)
	r := c.Post("/admin/backups/"+gone.Name+"/delete", nil)
	if r.Status != http.StatusSeeOther {
		t.Fatalf("%d", r.Status)
	}
	contains(t, c.Follow(r).Body, "Snapshot deleted: "+gone.Name+".")
	list, _ := appdb.ListBackups(e.Deps.BackupDir)
	if len(list) != 1 || list[0].Name != keep.Name {
		t.Fatalf("%+v", list)
	}
	if after, _ := e.Deps.R.MaxEventSeq(bg); after != seq {
		t.Fatal("a delete wrote an event")
	}
}

// Review focus 4: a name that isn't one the list shows is a 404, and
// nothing outside the backups directory is touched.
func TestDeleteBackupRefusesOutsideNames(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	dataDir := e.Deps.Cfg.DataDir
	snap(t, e, appdb.KindManual, snapAt)
	outside := map[string]string{
		"vaquero.db":                     filepath.Join(dataDir, "vaquero.db"),
		"vaquero-20261013-000000.sqlite": filepath.Join(dataDir, "vaquero-20261013-000000.sqlite"),
		"secret":                         filepath.Join(t.TempDir(), "secret"),
	}
	for _, p := range outside {
		if err := os.WriteFile(p, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A planted link with a snapshot's name, pointing outside.
	link := filepath.Join(e.Deps.BackupDir, "vaquero-20261014-000000.sqlite")
	if err := os.Symlink(outside["secret"], link); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../vaquero.db", "..%2Fvaquero.db", "%2E%2E%2Fvaquero.db",
		"x/y", "x%2Fy",
		"/etc/passwd", "%2Fetc%2Fpasswd", url.PathEscape(outside["secret"]),
		"..%2Fvaquero-20261013-000000.sqlite",
	} {
		if r := c.Post("/admin/backups/"+name+"/delete", nil); r.Status != http.StatusNotFound {
			t.Errorf("%s: %d", name, r.Status)
		}
	}
	// A snapshot's name with nothing listed behind it (ruling PR-3a): back
	// to the page with a toast, touching nothing.
	for _, name := range []string{
		"vaquero-20261015-000000.sqlite", // not listed
		"vaquero-20261014-000000.sqlite", // the link
	} {
		if r := c.Post("/admin/backups/"+name+"/delete", nil); r.Status != http.StatusSeeOther || r.Location != "/admin/backups" {
			t.Errorf("%s: %d %q", name, r.Status, r.Location)
		}
	}
	for _, p := range outside {
		if b, err := os.ReadFile(p); err != nil || string(b) != "keep" {
			t.Errorf("%s touched: %v", p, err)
		}
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("the link was removed")
	}
	if list, _ := appdb.ListBackups(e.Deps.BackupDir); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

// Ruling PR-3a: a snapshot already gone (pruned, or deleted in another tab)
// is a toast on the backups page, not a 404.
func TestDeleteGoneBackupIsAToast(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	info := snap(t, e, appdb.KindManual, snapAt)
	if r := c.Post("/admin/backups/"+info.Name+"/delete", nil); r.Status != http.StatusSeeOther {
		t.Fatalf("%d", r.Status)
	}
	r := c.Post("/admin/backups/"+info.Name+"/delete", nil)
	if r.Status != http.StatusSeeOther || r.Location != "/admin/backups" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "That snapshot is already gone.")
}

// As on every form, a cross-site post is refused.
func TestBackupFormsRefuseCrossSite(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	info := snap(t, e, appdb.KindManual, snapAt)
	for _, path := range []string{"/admin/backups/snapshot", "/admin/backups/" + info.Name + "/delete"} {
		if r := c.PostCrossSite(path, nil); r.Status != http.StatusForbidden {
			t.Errorf("%s: %d", path, r.Status)
		}
	}
	if list, _ := appdb.ListBackups(e.Deps.BackupDir); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

// VAQ-150: the version and uptime, the data directory, the database and
// WAL sizes, and brew services only when the server runs under it.
func TestSystemDetails(t *testing.T) {
	e := webtest.New(t)
	dbPath := e.Deps.Cfg.DBPath
	if err := os.WriteFile(dbPath, make([]byte, 3<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-wal", make([]byte, 5<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	e.Deps.Started = webtest.Start.Add(-3*time.Hour - 20*time.Minute)
	e.Rebuild()
	c := e.Client()
	page := c.Get("/admin").Body
	contains(t, page, version.Line(), "Uptime", "3 hours", e.Deps.Cfg.DataDir,
		"Database size", "3.0 MB", "Write-ahead log (WAL)", "5.0 MB", "Schema version")
	if strings.Contains(page, "Running under brew services") {
		t.Fatal("brew services named without it")
	}
	e.Deps.BrewService = func() bool { return true }
	e.Rebuild()
	contains(t, c.Get("/admin").Body, "Running under brew services")
	e.Deps.BrewService = func() bool { return false }
	e.Rebuild()
	if strings.Contains(c.Get("/admin").Body, "Running under brew services") {
		t.Fatal("brew services named when the probe says no")
	}
}

// VAQ-148: the Storage section shows the backend and location, the
// attachment count and size, where each setting came from, and the
// command to change it. The secret is shown only redacted.
func TestStorageSection(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	page := c.Get("/admin").Body
	contains(t, page, "Storage", "In the database", "No attachments yet.",
		"VQ_BLOB_STORE", "sqlite", "default", "vaquero attachments move --to")

	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Has files"})
	key := p.Key + "-" + strconv.FormatInt(tk.Number, 10)
	for _, body := range []string{"one", "three"} {
		if r := c.Upload("/t/"+key+"/attachments", "f.txt", "text/plain", []byte(body)); r.Status != http.StatusCreated {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
	}
	// S3 from the settings file, the region from the environment.
	dir := t.TempDir()
	text := "VQ_BLOB_STORE=s3\nVQ_S3_ENDPOINT=s3.example.test:9000\nVQ_S3_BUCKET=vq\n" +
		"VQ_S3_ACCESS_KEY_ID=" + configtest.AccessKeyID + "\nVQ_S3_SECRET_ACCESS_KEY=\"" + configtest.Secret + "\"\n"
	if err := os.WriteFile(config.EnvFilePath(dir), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.FromEnv(func(k string) string {
		return map[string]string{"VQ_DATA_DIR": dir, "VQ_S3_REGION": "eu-west-1"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	e.Deps.Cfg = cfg
	e.Rebuild()
	page = c.Get("/admin").Body
	contains(t, page, "s3.example.test:9000/vq", "2 attachments", units.SizeWords(8),
		"VQ_S3_ENDPOINT", "VQ_S3_REGION", "eu-west-1", "environment", "vaquero.env", config.EnvFilePath(dir),
		config.Redact("VQ_S3_SECRET_ACCESS_KEY", configtest.Secret),
		config.Redact("VQ_S3_ACCESS_KEY_ID", configtest.AccessKeyID))
	configtest.NoLeak(t, "/admin", page)
	configtest.NoLeak(t, "/admin/backups", c.Get("/admin/backups").Body)
	// Read-only: the section has no form.
	i := strings.Index(page, `id="storage"`)
	if i < 0 || strings.Contains(page[i:i+strings.Index(page[i:], "</section>")], "<form") {
		t.Fatal("no storage section, or it has a form")
	}
}
