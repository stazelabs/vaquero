package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appdb "github.com/stazelabs/vaquero/internal/db"
)

// stageDB makes a database at cfg's path migrated only to version v.
func stageDB(t *testing.T, path string, v int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.MigrateTo(d, v); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Checkpoint(d); err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func dbVersion(t *testing.T, path string) int64 {
	t.Helper()
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	v, err := appdb.Version(d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// serveBriefly boots serve, waits for it to answer, then stops it.
func serveBriefly(t *testing.T, dir string) {
	t.Helper()
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, dir), slog.New(slog.DiscardHandler)) }()
	waitHealthy(t, addr)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve didn't stop")
	}
}

func backupNames(t *testing.T, dir string) []string {
	t.Helper()
	bs, err := appdb.ListBackups(filepath.Join(dir, "backups"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
	}
	return names
}

func TestServeSnapshotsBeforeMigrating(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, "127.0.0.1:0", dir)
	stageDB(t, cfg.DBPath, 5)
	serveBriefly(t, dir)
	names := backupNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("backups %v, want one", names)
	}
	if v := dbVersion(t, filepath.Join(dir, "backups", names[0])); v != 5 {
		t.Fatalf("snapshot is at version %d, want 5", v)
	}
	newest, _ := appdb.Newest()
	if v := dbVersion(t, cfg.DBPath); v != newest {
		t.Fatalf("database at %d, want %d", v, newest)
	}
}

func TestServeNoSnapshotWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	serveBriefly(t, dir) // fresh
	serveBriefly(t, dir) // current
	if names := backupNames(t, dir); len(names) != 0 {
		t.Fatalf("backups %v, want none", names)
	}
}

func TestServeRefusesMigrationWithoutSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, freeAddr(t), dir)
	stageDB(t, cfg.DBPath, 5)
	if err := os.WriteFile(filepath.Join(dir, "backups"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := serve(ctx, cfg, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("serve started")
	}
	for _, want := range []string{"needs migrating from version 5 to", "the snapshot before it failed", "start vaquero again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if v := dbVersion(t, cfg.DBPath); v != 5 {
		t.Fatalf("database at %d, want 5 (untouched)", v)
	}
}

// brokenAt5 stages a version-5 database whose next migration fails (the
// column 0006 adds is already there), as a half-applied upgrade might.
func brokenAt5(t *testing.T, path string) {
	t.Helper()
	stageDB(t, path, 5)
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec("ALTER TABLE tickets ADD COLUMN claimed_by TEXT"); err != nil {
		t.Fatal(err)
	}
}

func TestServeRetryReusesSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, freeAddr(t), dir)
	brokenAt5(t, cfg.DBPath)
	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := serve(ctx, cfg, slog.New(slog.DiscardHandler))
		cancel()
		if err == nil || !strings.Contains(err.Error(), "goose up") {
			t.Fatalf("attempt %d: want the migration's own error, got %v", i, err)
		}
	}
	if names := backupNames(t, dir); len(names) != 1 {
		t.Fatalf("backups %v, want the one snapshot from the first attempt", names)
	}
}

func TestSnapshotFailureNamesTheCause(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, freeAddr(t), dir)
	stageDB(t, cfg.DBPath, 5)
	os.WriteFile(filepath.Join(dir, "backups"), nil, 0o600)
	err := serve(context.Background(), cfg, slog.New(slog.DiscardHandler))
	if err == nil || strings.Contains(err.Error(), "free some space") || !strings.Contains(err.Error(), "check that "+filepath.Join(dir, "backups")+" can be written") {
		t.Fatalf("%v", err)
	}
}

func markerPath(dir string) string { return filepath.Join(dir, "backups", premigrateMarker) }

// A marker left by a failed upgrade must not outlive the version it was for:
// a boot that finds the database current clears it, so a later upgrade from
// the same version takes a fresh snapshot.
func TestMarkerClearedWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	serveBriefly(t, dir) // a current database
	os.MkdirAll(filepath.Dir(markerPath(dir)), 0o700)
	if err := os.WriteFile(markerPath(dir), []byte("5 vaquero-20261118-120000.sqlite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	serveBriefly(t, dir)
	if _, err := os.Stat(markerPath(dir)); err == nil {
		t.Fatal("marker survived a boot on a current database")
	}
}

func TestMarkerClearedAfterMigrating(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, "127.0.0.1:0", dir)
	stageDB(t, cfg.DBPath, 5)
	serveBriefly(t, dir)
	if _, err := os.Stat(markerPath(dir)); err == nil {
		t.Fatal("marker survived a successful migration")
	}
}

// A boot in the same second as another snapshot (just after a restore,
// say) takes the next free name rather than failing.
func TestPreMigrateSnapshotTakesNextFreeName(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, "127.0.0.1:0", dir)
	stageDB(t, cfg.DBPath, 5)
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	old := bootNow
	bootNow = func() time.Time { return at }
	t.Cleanup(func() { bootNow = old })
	os.MkdirAll(filepath.Join(dir, "backups"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "backups", appdb.BackupName(at)), []byte("taken a moment ago"), 0o600); err != nil {
		t.Fatal(err)
	}
	serveBriefly(t, dir)
	if names := backupNames(t, dir); len(names) != 2 {
		t.Fatalf("backups %v, want the earlier one and the new one", names)
	}
}
