package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestStopUnderBrewServices(t *testing.T) {
	localEnv(t)
	home, _ := os.UserHomeDir()
	p := brewServiceFile(home, "darwin")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := serviceOS
	serviceOS = "darwin"
	t.Cleanup(func() { serviceOS = old })
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
