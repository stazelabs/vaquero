package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

var snapAt = time.Date(2026, 10, 13, 9, 30, 5, 0, time.UTC)

func TestSnapshotCopiesTheSchemaAndLeavesNoTempFile(t *testing.T) {
	d := dbtest.New(t)
	dir := filepath.Join(t.TempDir(), "backups") // created by Snapshot
	if _, err := d.W.Exec(`CREATE TABLE marker (v TEXT); INSERT INTO marker VALUES ('hello')`); err != nil {
		t.Fatal(err)
	}
	info, err := appdb.Snapshot(context.Background(), d.R, dir, snapAt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "vaquero-20261013-093005.sqlite" || info.Size == 0 {
		t.Fatalf("info %+v", info)
	}
	snap, err := sql.Open("sqlite", "file:"+filepath.Join(dir, info.Name)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var v string
	if err := snap.QueryRow(`SELECT v FROM marker`).Scan(&v); err != nil || v != "hello" {
		t.Fatalf("marker %q, %v", v, err)
	}
	var n int
	if err := snap.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'tickets'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema missing: %d, %v", n, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %d files, want only the snapshot", len(entries))
	}
	// The same second again is refused, not overwritten.
	if _, err := appdb.Snapshot(context.Background(), d.R, dir, snapAt); !errors.Is(err, appdb.ErrBackupExists) {
		t.Fatalf("second snapshot: %v", err)
	}
}

func TestSnapshotFailureLeavesNoPartialFile(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	// A cancelled context fails VACUUM INTO after the space check.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := appdb.Snapshot(ctx, d.R, dir, snapAt); err == nil {
		t.Fatal("a cancelled snapshot succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a failed snapshot left %d files", len(entries))
	}
	if list, _ := appdb.ListBackups(dir); len(list) != 0 {
		t.Fatalf("listed %v", list)
	}
}

func TestRequiredFree(t *testing.T) {
	if got := appdb.RequiredFree(10 << 20); got != 10<<20+appdb.MinBackupMargin {
		t.Fatalf("small db: %d", got)
	}
	if got := appdb.RequiredFree(400 << 20); got != 800<<20 {
		t.Fatalf("large db: %d", got)
	}
}

func TestFreeBytesWalksUpToAnExistingParent(t *testing.T) {
	n, err := appdb.FreeBytes(filepath.Join(t.TempDir(), "a", "b"))
	if err != nil || n <= 0 {
		t.Fatalf("free %d, %v", n, err)
	}
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListPruneAndNames(t *testing.T) {
	dir := t.TempDir()
	if list, err := appdb.ListBackups(filepath.Join(dir, "missing")); err != nil || len(list) != 0 {
		t.Fatalf("missing dir: %v %v", list, err)
	}
	names := []string{"vaquero-predelete-20261010-000000.sqlite", "vaquero-predelete-20261012-000000.sqlite", "vaquero-predelete-20261011-000000.sqlite", "vaquero-predelete-20261013-000000.sqlite"}
	for _, n := range names {
		touch(t, dir, n)
	}
	touch(t, dir, "notes.txt")
	touch(t, dir, ".tmp-vaquero-predelete-20261014-000000.sqlite")
	if err := os.Mkdir(filepath.Join(dir, "vaquero-predelete-20261015-000000.sqlite"), 0o750); err != nil {
		t.Fatal(err)
	}
	list, err := appdb.ListBackups(dir)
	if err != nil || len(list) != 4 || list[0].Name != names[3] || list[3].Name != names[0] {
		t.Fatalf("list %v %v", list, err)
	}
	if removed, err := appdb.PruneBackups(dir, 0); err != nil || len(removed) != 0 {
		t.Fatalf("keep 0 pruned %v %v", removed, err)
	}
	removed, err := appdb.PruneBackups(dir, 2)
	if err != nil || len(removed) != 2 || removed[0] != names[2] || removed[1] != names[0] {
		t.Fatalf("pruned %v %v", removed, err)
	}
	if list, _ = appdb.ListBackups(dir); len(list) != 2 || list[0].Name != names[3] || list[1].Name != names[1] {
		t.Fatalf("after prune %v", list)
	}
	// Unrelated files are never touched.
	for _, n := range []string{"notes.txt", ".tmp-vaquero-predelete-20261014-000000.sqlite"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s was removed", n)
		}
	}
}

func TestBadNamesNeverLeaveTheDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backups")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	touch(t, root, "secret.txt")
	touch(t, root, "vaquero-20261013-000000.sqlite") // outside dir
	for _, name := range []string{"", "..", "../secret.txt", "../vaquero-20261013-000000.sqlite", "vaquero-20261013-000000.sqlite/../../secret.txt",
		"/etc/passwd", `..\secret.txt`, "vaquero-20261013-000000.sqlite\x00.txt", "vaquero-2026101-000000.sqlite", "STARTCASE-20261013-000000.sqlite", ".tmp-vaquero-20261013-000000.sqlite", "vaquero-20261013-000000.sqlite\n"} {
		if f, _, err := appdb.OpenBackup(dir, name); !errors.Is(err, appdb.ErrBadBackupName) {
			if f != nil {
				f.Close()
			}
			t.Errorf("open %q: %v", name, err)
		}
		if err := appdb.DeleteBackup(dir, name); !errors.Is(err, appdb.ErrBadBackupName) {
			t.Errorf("delete %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "secret.txt")); err != nil {
		t.Fatal("a file outside the directory was touched")
	}
	if _, _, err := appdb.OpenBackup(dir, "vaquero-20261013-000000.sqlite"); !errors.Is(err, appdb.ErrBackupNotFound) {
		t.Fatalf("valid name, no file: %v", err)
	}
}

func TestSymlinkedSnapshotIsIgnored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backups")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	touch(t, root, "secret.txt")
	const name = "vaquero-20261013-000000.sqlite"
	if err := os.Symlink(filepath.Join(root, "secret.txt"), filepath.Join(dir, name)); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if list, _ := appdb.ListBackups(dir); len(list) != 0 {
		t.Fatalf("listed a symlink: %v", list)
	}
	if _, _, err := appdb.OpenBackup(dir, name); !errors.Is(err, appdb.ErrBackupNotFound) {
		t.Fatalf("open symlink: %v", err)
	}
	if err := appdb.DeleteBackup(dir, name); !errors.Is(err, appdb.ErrBackupNotFound) {
		t.Fatalf("delete symlink: %v", err)
	}
}

func TestOpenAndDelete(t *testing.T) {
	dir := t.TempDir()
	const name = "vaquero-20261013-000000.sqlite"
	touch(t, dir, name)
	f, info, err := appdb.OpenBackup(dir, name)
	if err != nil || info.Size != 1 {
		t.Fatalf("open %+v %v", info, err)
	}
	f.Close()
	if err := appdb.DeleteBackup(dir, name); err != nil {
		t.Fatal(err)
	}
	if err := appdb.DeleteBackup(dir, name); !errors.Is(err, appdb.ErrBackupNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestSnapshotRefusesWhenTheVolumeIsTooFull(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	size, err := appdb.Size(context.Background(), d.R)
	if err != nil || size == 0 {
		t.Fatalf("size %d %v", size, err)
	}
	need := appdb.RequiredFree(size)
	defer appdb.SetFreeBytes(func(string) (int64, error) { return need - 1, nil })()
	_, err = appdb.Snapshot(context.Background(), d.R, dir, snapAt)
	if !errors.Is(err, appdb.ErrLowSpace) {
		t.Fatalf("one byte short: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused snapshot left %d files", len(entries))
	}
	appdb.SetFreeBytes(func(string) (int64, error) { return need, nil })
	if _, err := appdb.Snapshot(context.Background(), d.R, dir, snapAt); err != nil {
		t.Fatalf("exactly enough: %v", err)
	}
}

func TestSnapshotRemovesStaleTempFilesOnly(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	old := filepath.Join(dir, ".tmp-vaquero-20250101-000000.sqlite")
	fresh := filepath.Join(dir, ".tmp-vaquero-20261013-093000.sqlite")
	stranger := filepath.Join(dir, ".tmp-notes.txt")
	for _, p := range []string{old, fresh, stranger} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	set := func(p string, age time.Duration) {
		if err := os.Chtimes(p, snapAt.Add(-age), snapAt.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	set(old, appdb.StaleTempAge+time.Minute)
	set(fresh, time.Minute)
	set(stranger, 24*time.Hour)
	if _, err := appdb.Snapshot(context.Background(), d.R, dir, snapAt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a stale temp file survived")
	}
	for _, p := range []string{fresh, stranger} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed", filepath.Base(p))
		}
	}
}

// The copy runs on its own connection: a write through the writer pool
// succeeds while VACUUM INTO is in progress.
func TestSnapshotDoesNotHoldTheWriterPool(t *testing.T) {
	d := dbtest.New(t)
	real := appdb.RealVacuumInto
	var wrote error
	ran := false
	defer appdb.SetVacuumInto(func(ctx context.Context, path, dest string) error {
		ran = true
		wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, wrote = d.W.ExecContext(wctx, `CREATE TABLE written_during (v TEXT)`)
		return real(ctx, path, dest)
	})()
	if _, err := appdb.Snapshot(context.Background(), d.R, t.TempDir(), snapAt); err != nil {
		t.Fatal(err)
	}
	if !ran || wrote != nil {
		t.Fatalf("ran %v, write during the snapshot: %v", ran, wrote)
	}
}

func TestFreeBytesReadsAMissingDirThroughItsParent(t *testing.T) {
	free, err := appdb.FreeBytes(filepath.Join(t.TempDir(), "not", "yet"))
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 {
		t.Fatalf("free %d", free)
	}
}

// Each kind of snapshot is kept on its own: a run of project deletes can't
// prune the snapshot taken before an upgrade. Snapshots from before kinds
// existed (plain names) still list and open, and are never pruned.
func TestPruneKeepsEachKindOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "vaquero-20260101-000000.sqlite") // an old release's
	touch(t, dir, "vaquero-20260102-000000.sqlite")
	touch(t, dir, "vaquero-premigrate-20261020-000000.sqlite")
	for i := 1; i <= 9; i++ {
		touch(t, dir, appdb.BackupNameKind(appdb.KindPreDelete, snapAt.Add(time.Duration(i)*time.Second)))
	}
	removed, err := appdb.PruneBackups(dir, appdb.BackupKeep)
	if err != nil || len(removed) != 2 {
		t.Fatalf("pruned %v %v", removed, err)
	}
	for _, n := range removed {
		if !strings.HasPrefix(n, "vaquero-predelete-") {
			t.Errorf("pruned %s", n)
		}
	}
	list, err := appdb.ListBackups(dir)
	if err != nil || len(list) != 10 {
		t.Fatalf("list %v %v", list, err)
	}
	if list[0].Name != "vaquero-premigrate-20261020-000000.sqlite" || list[0].Kind != appdb.KindPreMigrate {
		t.Errorf("newest first: %+v", list[0])
	}
	if last := list[len(list)-1]; last.Name != "vaquero-20260101-000000.sqlite" || last.Kind != "" {
		t.Errorf("an old name lists as a plain snapshot: %+v", last)
	}
	for _, n := range []string{"vaquero-20260101-000000.sqlite", "vaquero-premigrate-20261020-000000.sqlite"} {
		f, _, err := appdb.OpenBackup(dir, n)
		if err != nil {
			t.Errorf("open %s: %v", n, err)
			continue
		}
		f.Close()
	}
}

func TestSnapshotNextNamesTheKind(t *testing.T) {
	d := dbtest.New(t)
	dir := t.TempDir()
	for _, k := range []appdb.Kind{appdb.KindPreMigrate, appdb.KindPreDelete, appdb.KindPreRestore, appdb.KindPreImport, appdb.KindPreCompact} {
		info, err := appdb.SnapshotNext(context.Background(), d.R, dir, k, snapAt)
		if err != nil {
			t.Fatal(err)
		}
		want := "vaquero-" + string(k) + "-"
		if !strings.HasPrefix(info.Name, want) || info.Kind != k {
			t.Errorf("%s: %+v", k, info)
		}
	}
	// Same second, same kind: the next free second.
	a, _ := appdb.SnapshotNext(context.Background(), d.R, dir, appdb.KindPreDelete, snapAt)
	if a.Name == appdb.BackupNameKind(appdb.KindPreDelete, snapAt) {
		t.Errorf("reused a name: %s", a.Name)
	}
	if _, err := appdb.ListBackups(dir); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"vaquero-other-20261013-093005.sqlite", "vaquero-premigrate-premigrate-20261013-093005.sqlite"} {
		if _, _, err := appdb.OpenBackup(dir, bad); !errors.Is(err, appdb.ErrBadBackupName) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// atVersion writes a snapshot that records schema version v, as goose does.
func atVersion(t *testing.T, dir, name string, v int64) {
	t.Helper()
	d, err := appdb.Open(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY, version_id INTEGER, is_applied INTEGER, tstamp TEXT);
		INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1), (?, 1)`, v); err != nil {
		t.Fatal(err)
	}
}

// VAQ-113: pruning keeps seven pre-migration snapshots and, beyond them, the
// newest one of each schema version they don't cover, so many migrating
// upgrades never remove the last copy of an older schema.
func TestPruneKeepsOnePreMigratePerSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	name := func(i int) string {
		return appdb.BackupNameKind(appdb.KindPreMigrate, snapAt.Add(time.Duration(i)*time.Second))
	}
	atVersion(t, dir, name(1), 5) // older copy of 5: pruned
	atVersion(t, dir, name(2), 5) // newest of 5: kept
	atVersion(t, dir, name(3), 6) // the only 6: kept
	touch(t, dir, name(4))        // unreadable: pruned as before
	for i := 5; i <= 13; i++ {
		atVersion(t, dir, name(i), 7) // seven kept, two pruned
	}
	removed, err := appdb.PruneBackups(dir, appdb.BackupKeep)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{name(1): true, name(4): true, name(5): true, name(6): true}
	if len(removed) != len(want) {
		t.Fatalf("pruned %v", removed)
	}
	for _, n := range removed {
		if !want[n] {
			t.Errorf("pruned %s", n)
		}
	}
	for _, n := range []string{name(2), name(3), name(13)} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s gone: %v", n, err)
		}
	}
}
