package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/lockfile"
)

// runCLI runs a command in-process and returns its exit code and output.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// startBackground starts a real background server through the CLI.
func startBackground(t *testing.T) string {
	t.Helper()
	dataDir, _ := localEnv(t)
	if code, _, e := runCLI(t, "projects"); code != 0 {
		t.Fatalf("projects: %d %s", code, e)
	}
	if _, running, _ := lockfile.Holder(dataDir); !running {
		t.Fatal("no background server")
	}
	return dataDir
}

func TestStopNotRunning(t *testing.T) {
	localEnv(t)
	code, out, _ := runCLI(t, "stop")
	if code != 0 || strings.TrimSpace(out) != "Vaquero isn't running." {
		t.Fatalf("%d %q", code, out)
	}
}

func TestStopEndsBackgroundServer(t *testing.T) {
	dataDir := startBackground(t)
	code, out, e := runCLI(t, "stop")
	if code != 0 || strings.TrimSpace(out) != "Vaquero stopped." {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if _, running, _ := lockfile.Holder(dataDir); running {
		t.Fatal("server still holds the lock")
	}
}

// brewServiceFileExists writes the file brew services keeps for Vaquero
// under the test's HOME, as for macOS.
func brewServiceFileExists(t *testing.T) {
	t.Helper()
	p := brewServiceFile(os.Getenv("HOME"), "darwin")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := serviceOS
	serviceOS = "darwin"
	t.Cleanup(func() { serviceOS = old })
}

// brewEnv is localEnv for the instance brew services runs: the service file
// exists and the data directory is the default one under the test's HOME
// (set in VQ_DATA_DIR too, so a server started here uses it on any OS).
func brewEnv(t *testing.T) (dataDir, addr string) {
	t.Helper()
	_, addr = localEnv(t)
	home := os.Getenv("HOME")
	dataDir, err := config.DefaultDataDir("darwin", os.Getenv)
	if err != nil || !strings.HasPrefix(dataDir, home+string(filepath.Separator)) || home == "" {
		t.Fatalf("default data dir %q is not under the test HOME %q: %v", dataDir, home, err)
	}
	t.Setenv("VQ_DATA_DIR", dataDir)
	t.Cleanup(func() { killTestServer(dataDir) })
	brewServiceFileExists(t)
	return dataDir, addr
}

// Only the default data directory's instance is the service's: the formula
// sets no VQ_DATA_DIR.
func TestBrewManaged(t *testing.T) {
	home := t.TempDir()
	env := func(kv ...string) func(string) string {
		m := map[string]string{"HOME": home}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	mac := filepath.Join(home, "Library", "Application Support", "vaquero")
	linux := filepath.Join(home, ".local", "share", "vaquero")
	if brewManaged("darwin", mac, env()) {
		t.Fatal("managed without a service file")
	}
	for _, goos := range []string{"darwin", "linux"} {
		p := brewServiceFile(home, goos)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]struct {
		goos, dir string
		get       func(string) string
		want      bool
	}{
		"mac default":          {"darwin", mac, env(), true},
		"mac default, slash":   {"darwin", mac + "/", env(), true},
		"mac custom":           {"darwin", filepath.Join(home, "other"), env(), false},
		"linux default":        {"linux", linux, env(), true},
		"linux custom":         {"linux", filepath.Join(home, "other"), env(), false},
		"linux XDG default":    {"linux", filepath.Join(home, "xdg", "vaquero"), env("XDG_DATA_HOME", filepath.Join(home, "xdg")), true},
		"linux XDG, plain dir": {"linux", linux, env("XDG_DATA_HOME", filepath.Join(home, "xdg")), false},
		"no HOME":              {"darwin", mac, func(string) string { return "" }, false},
	} {
		if got := brewManaged(c.goos, c.dir, c.get); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}

// A symlinked spelling of the default directory is the same directory.
func TestBrewManagedSymlink(t *testing.T) {
	home := t.TempDir()
	get := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	p := brewServiceFile(home, "darwin")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(home, "Library", "Application Support", "vaquero")
	if err := os.MkdirAll(def, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(def, link); err != nil {
		t.Fatal(err)
	}
	if !brewManaged("darwin", link, get) {
		t.Fatal("symlink to the default directory not recognised")
	}
}

func TestStopUnderBrewServices(t *testing.T) {
	brewEnv(t)
	code, _, e := runCLI(t, "stop")
	if code != 1 || !strings.Contains(e, "Vaquero runs under brew services; run brew services stop vaquero.") {
		t.Fatalf("%d %q", code, e)
	}
}

// setNow fixes the clock the local commands use.
func setNow(t *testing.T, at time.Time) {
	t.Helper()
	old := localNow
	localNow = func() time.Time { return at }
	t.Cleanup(func() { localNow = old })
}

// seedData makes a database with one project, VAQ, and stops its server.
func seedData(t *testing.T) (dataDir string) {
	t.Helper()
	dataDir = startBackground(t)
	if code, _, e := runCLI(t, "project", "create", "VAQ", "Vaquero"); code != 0 {
		t.Fatalf("project new: %s", e)
	}
	if code, _, e := runCLI(t, "stop"); code != 0 {
		t.Fatalf("stop: %s", e)
	}
	return dataDir
}

func snapshotNow(t *testing.T, dataDir string) string {
	t.Helper()
	d, err := appdb.Open(filepath.Join(dataDir, "vaquero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	info, err := appdb.Snapshot(t.Context(), d, filepath.Join(dataDir, "backups"), localNow())
	if err != nil {
		t.Fatal(err)
	}
	return info.Name
}

func TestBackupListEmptyAndSorted(t *testing.T) {
	localEnv(t)
	code, out, _ := runCLI(t, "backup", "list")
	if code != 0 || strings.TrimSpace(out) != "No backups yet." {
		t.Fatalf("%d %q", code, out)
	}
	dataDir := seedData(t)
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	a := snapshotNow(t, dataDir)
	setNow(t, time.Date(2026, 11, 18, 12, 0, 2, 0, time.UTC))
	b := snapshotNow(t, dataDir)
	_, out, _ = runCLI(t, "backup", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], b) || !strings.HasPrefix(lines[1], a) {
		t.Fatalf("want %s then %s, got:\n%s", b, a, out)
	}
}

// An instance with its own data directory isn't the service's, even when
// brew services runs Vaquero too: stop stops it.
func TestStopWithOwnDataDirUnderBrewServices(t *testing.T) {
	dataDir := startBackground(t)
	brewServiceFileExists(t)
	code, out, e := runCLI(t, "stop")
	if code != 0 || strings.TrimSpace(out) != "Vaquero stopped." {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if _, running, _ := lockfile.Holder(dataDir); running {
		t.Fatal("server still holds the lock")
	}
}

// The restore refusal names the command that stops this instance.
func TestRestoreRefusedNamesBrewServices(t *testing.T) {
	brewEnv(t)
	if code, _, e := runCLI(t, "projects"); code != 0 {
		t.Fatalf("projects: %s", e)
	}
	code, _, e := runCLI(t, "backup", "restore", "vaquero-20261118-120000.sqlite")
	if code != 1 || !strings.Contains(e, "Stop vaquero first: brew services stop vaquero") {
		t.Fatalf("%d %q", code, e)
	}
}

func TestRestoreRefusedWithOwnDataDirUnderBrewServices(t *testing.T) {
	startBackground(t)
	brewServiceFileExists(t)
	code, _, e := runCLI(t, "backup", "restore", "vaquero-20261118-120000.sqlite")
	if code != 1 || !strings.Contains(e, "Stop vaquero first: vaquero stop") {
		t.Fatalf("%d %q", code, e)
	}
}

func TestRestoreRefusedWhileRunning(t *testing.T) {
	startBackground(t)
	code, _, e := runCLI(t, "backup", "restore", "vaquero-20261118-120000.sqlite")
	if code != 1 || !strings.Contains(e, "Stop vaquero first: vaquero stop") {
		t.Fatalf("%d %q", code, e)
	}
}

func TestRestoreReplacesAndKeepsPrevious(t *testing.T) {
	dataDir := seedData(t) // one project, VAQ
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	name := snapshotNow(t, dataDir)
	// A change after the snapshot, which the restore undoes.
	runCLI(t, "projects") // starts the server again
	if code, _, e := runCLI(t, "project", "create", "OTH", "Other"); code != 0 {
		t.Fatalf("project new: %s", e)
	}
	runCLI(t, "stop")
	setNow(t, time.Date(2026, 11, 18, 12, 0, 5, 0, time.UTC))
	code, out, e := runCLI(t, "backup", "restore", name)
	if code != 0 || !strings.Contains(out, "Restored "+name+". The database before it is saved as ") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	for _, f := range []string{"vaquero.db-wal", "vaquero.db-shm"} {
		if _, err := os.Stat(filepath.Join(dataDir, f)); err == nil {
			t.Errorf("%s left behind", f)
		}
	}
	bs, _ := appdb.ListBackups(filepath.Join(dataDir, "backups"))
	if len(bs) != 2 {
		t.Fatalf("backups %v, want the original and the pre-restore copy", bs)
	}
	_, out, _ = runCLI(t, "projects")
	if !strings.Contains(out, "VAQ") || strings.Contains(out, "OTH") {
		t.Fatalf("restored database has the wrong projects:\n%s", out)
	}
}

// Each restore saves the database before it, and those copies are pruned as
// they are made (7 kept) rather than at the next unrelated prune; a snapshot
// of another kind is left alone.
func TestRestorePrunesItsOwnSnapshots(t *testing.T) {
	dataDir := seedData(t)
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	name := snapshotNow(t, dataDir) // a plain name: never pruned
	dir := filepath.Join(dataDir, "backups")
	keepName := appdb.BackupNameKind(appdb.KindPreMigrate, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err := os.WriteFile(filepath.Join(dir, keepName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range 9 {
		setNow(t, time.Date(2026, 11, 18, 12, 1, i, 0, time.UTC))
		if code, _, e := runCLI(t, "backup", "restore", name); code != 0 {
			t.Fatalf("restore %d: %q", i, e)
		}
	}
	bs, _ := appdb.ListBackups(dir)
	counts := map[appdb.Kind]int{}
	for _, b := range bs {
		counts[b.Kind]++
	}
	if counts[appdb.KindPreRestore] != appdb.BackupKeep || counts[appdb.KindPreMigrate] != 1 || counts[""] != 1 {
		t.Fatalf("kept %v:\n%v", counts, bs)
	}
}

func TestRestoreUnknownName(t *testing.T) {
	localEnv(t)
	code, _, e := runCLI(t, "backup", "restore", "vaquero-20000101-000000.sqlite")
	if code != 1 || !strings.Contains(e, "No backup called vaquero-20000101-000000.sqlite.") {
		t.Fatalf("%d %q", code, e)
	}
}

// A damaged database is a reason to restore: it can't be snapshotted, so it
// is moved aside instead, and the restore goes ahead.
func TestRestoreOverDamagedDatabase(t *testing.T) {
	dataDir := seedData(t)
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	name := snapshotNow(t, dataDir)
	db := filepath.Join(dataDir, "vaquero.db")
	os.Remove(db + "-wal")
	os.Remove(db + "-shm")
	if err := os.WriteFile(db, []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, e := runCLI(t, "backup", "restore", name)
	if code != 0 || !strings.Contains(out, "Restored "+name+".") || !strings.Contains(out, "couldn't be copied, so it was moved to ") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	moved, _ := filepath.Glob(db + ".before-restore-*")
	if len(moved) != 1 {
		t.Fatalf("moved aside: %v", moved)
	}
	if b, _ := os.ReadFile(moved[0]); string(b) != "this is not a database" {
		t.Fatalf("moved file holds %q", b)
	}
	if _, out, _ := runCLI(t, "projects"); !strings.Contains(out, "VAQ") {
		t.Fatalf("restored database:\n%s", out)
	}
}

// R11: a bad server-only setting in the shell (log level, log format,
// attachment storage) stops only the commands that use it: serve checks
// them all, import the storage settings; every other command ignores them.
func TestServerOnlySettingsStopOnlyServeAndImport(t *testing.T) {
	startBackground(t)
	t.Setenv("VQ_LOG_LEVEL", "verbose")
	t.Setenv("VQ_LOG_FORMAT", "xml")
	for _, args := range [][]string{{"projects"}, {"backup", "list"}, {"stop"}} {
		if code, _, e := runCLI(t, args...); code != 0 {
			t.Fatalf("%v: %d %q", args, code, e)
		}
	}
	if code, _, e := runCLI(t, "serve"); code != 1 || !strings.Contains(e, "VQ_LOG_LEVEL") || !strings.Contains(e, "VQ_LOG_FORMAT") {
		t.Fatalf("serve: %d %q", code, e)
	}
	t.Setenv("VQ_BLOB_STORE", "floppy")
	if code, _, e := runCLI(t, "backup", "list"); code != 0 {
		t.Fatalf("backup list: %d %q", code, e)
	}
	file := seededArchive(t, importHeader(t, 0), nil)
	if code, _, e := runCLI(t, "import", file); code != 1 || !strings.Contains(e, "VQ_BLOB_STORE") || strings.Contains(e, "VQ_LOG") {
		t.Fatalf("import with bad storage: %d %q", code, e)
	}
	t.Setenv("VQ_BLOB_STORE", "")
	if code, _, e := runCLI(t, "import", file); code != 0 {
		t.Fatalf("import: %d %q", code, e)
	}
}

func TestSyncDir(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := syncDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing directory synced")
	}
}

// A snapshot is a VACUUM INTO copy, which may renumber the rowids the search
// indexes are keyed on, so a restore rebuilds both before the swap.
func TestRestoreRebuildsSearch(t *testing.T) {
	dataDir := seedData(t)
	runCLI(t, "projects")
	if code, _, e := runCLI(t, "ticket", "new", "VAQ", "Rope the lasso"); code != 0 {
		t.Fatalf("ticket new: %s", e)
	}
	runCLI(t, "stop")
	path := filepath.Join(dataDir, "vaquero.db")
	dropFromSearch(t, path) // the snapshot carries a stale index
	setNow(t, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	name := snapshotNow(t, dataDir)
	setNow(t, time.Date(2026, 11, 18, 12, 0, 5, 0, time.UTC))
	if code, out, e := runCLI(t, "backup", "restore", name); code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	if left, _ := filepath.Glob(path + ".restore*"); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if hits, err := appdb.SearchTickets(t.Context(), d, `"lasso"*`, 10); err != nil || len(hits) != 1 {
		t.Fatalf("search after restoring: %d hits, %v", len(hits), err)
	}
}
