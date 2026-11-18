package db_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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
	names := []string{"vaquero-20261010-000000.sqlite", "vaquero-20261012-000000.sqlite", "vaquero-20261011-000000.sqlite", "vaquero-20261013-000000.sqlite"}
	for _, n := range names {
		touch(t, dir, n)
	}
	touch(t, dir, "notes.txt")
	touch(t, dir, ".tmp-vaquero-20261014-000000.sqlite")
	if err := os.Mkdir(filepath.Join(dir, "vaquero-20261015-000000.sqlite"), 0o750); err != nil {
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
	for _, n := range []string{"notes.txt", ".tmp-vaquero-20261014-000000.sqlite"} {
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

func TestVolumeSpaceReadsFreeAndTotal(t *testing.T) {
	free, total, err := appdb.VolumeSpace(filepath.Join(t.TempDir(), "not", "yet"))
	if err != nil {
		t.Fatal(err)
	}
	if total <= 0 || free <= 0 || free > total {
		t.Fatalf("free %d, total %d", free, total)
	}
}

func TestSpaceLow(t *testing.T) {
	const gb = int64(1) << 30
	for _, c := range []struct {
		name        string
		free, total int64
		low         bool
	}{
		{"plenty on 1 GB", 600 << 20, gb, false},
		{"under 200 MB of 3 GB", 150 << 20, 3 * gb, true},
		{"under a fifth of 3 GB", 500 << 20, 3 * gb, true},
		{"just over a fifth of 1 GB", 205 << 20, gb, false},
		{"just under 200 MB on 1 GB", 199 << 20, gb, true},
		{"unknown total", 10 << 20, 0, true},
		{"unknown total, roomy", 5 * gb, 0, false},
	} {
		if got := appdb.SpaceLow(c.free, c.total); got != c.low {
			t.Errorf("%s: SpaceLow(%d, %d) = %v, want %v", c.name, c.free, c.total, got, c.low)
		}
	}
}

// Snapshots may take at most half the volume, so the keep count
// shrinks as the database grows and never goes below one.
func TestEffectiveKeep(t *testing.T) {
	const mb = int64(1) << 20
	for _, c := range []struct {
		name              string
		keep              int
		size, total, want int64
	}{
		{"small database keeps the setting", 7, 40 * mb, 1024 * mb, 7},
		{"setting below the cap wins", 3, 40 * mb, 1024 * mb, 3},
		{"100 MB on 1 GB", 7, 100 * mb, 1024 * mb, 3},
		{"110 MB on 1 GB", 7, 110 * mb, 1024 * mb, 2},
		{"150 MB on 1 GB", 7, 150 * mb, 1024 * mb, 1},
		{"400 MB on 1 GB still keeps one", 7, 400 * mb, 1024 * mb, 1},
		{"400 MB on 3 GB", 7, 400 * mb, 3072 * mb, 1},
		{"200 MB on 3 GB", 7, 200 * mb, 3072 * mb, 5},
		{"unknown volume keeps the setting", 7, 40 * mb, 0, 7},
		{"unknown size keeps the setting", 7, 0, 1024 * mb, 7},
	} {
		if got := appdb.EffectiveKeep(c.keep, c.size, c.total); int64(got) != c.want {
			t.Errorf("%s: EffectiveKeep(%d, %d, %d) = %d, want %d", c.name, c.keep, c.size, c.total, got, c.want)
		}
	}
}

func TestKeepForReadsTheRealDatabaseAndVolume(t *testing.T) {
	d := dbtest.New(t)
	// A test database is far smaller than any volume's half, so the setting stands.
	if got := appdb.KeepFor(context.Background(), d.R, t.TempDir(), 7); got != 7 {
		t.Fatalf("KeepFor = %d, want 7", got)
	}
}

func TestBackupNameParsesBack(t *testing.T) {
	got, err := appdb.ParseBackupName(appdb.BackupName(snapAt))
	if err != nil || !got.Equal(snapAt) {
		t.Fatalf("ParseBackupName(BackupName(%v)) = %v, %v", snapAt, got, err)
	}
	for _, bad := range []string{"", "backup.sqlite", "vaquero-20261313-093005.sqlite", "vaquero-20261013-093005.sqlite.tmp"} {
		if _, err := appdb.ParseBackupName(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}
