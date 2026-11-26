package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	appdb "github.com/stazelabs/vaquero/internal/db"
)

// bloated is seedData with a ticket, VAQ-1 "Rope the lasso", and 4 MB of
// attachment bytes stored and swept again: the space a sweep frees and
// only compaction gives back. It returns the database's size.
func bloated(t *testing.T) (dataDir string, size int64) {
	t.Helper()
	dataDir = seedData(t)
	runCLI(t, "projects")
	if code, _, e := runCLI(t, "ticket", "new", "VAQ", "Rope the lasso"); code != 0 {
		t.Fatalf("ticket new: %s", e)
	}
	runCLI(t, "stop")
	path := filepath.Join(dataDir, "vaquero.db")
	w, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := appdb.OpenReader(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	store := blobs.NewSQLite(w, r, time.Now)
	key := "attachments/test/01jcompact0000000000000000"
	if err := store.Put(t.Context(), key, bytes.NewReader(bytes.Repeat([]byte("x"), 4<<20)), 4<<20, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if err := appdb.Checkpoint(w); err != nil {
		t.Fatal(err)
	}
	size, err = appdb.Size(t.Context(), w)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if size < 4<<20 {
		t.Fatalf("database is %d bytes; the swept blob didn't stay in the file", size)
	}
	return dataDir, size
}

func dbSize(t *testing.T, path string) int64 {
	t.Helper()
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	n, err := appdb.Size(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// dropFromSearch takes VAQ-1 out of the ticket search index, as a stale
// index would be, so a test can see compaction rebuild it.
func dropFromSearch(t *testing.T, path string) {
	t.Helper()
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO tickets_fts (tickets_fts, rowid, title, body) SELECT 'delete', rowid, title, body FROM tickets`); err != nil {
		t.Fatal(err)
	}
	if hits, _ := appdb.SearchTickets(t.Context(), d, []string{`"lasso"*`}, 10); len(hits) != 0 {
		t.Fatal("the ticket is still in the index")
	}
}

func TestCompactRefusedWhileRunning(t *testing.T) {
	startBackground(t)
	code, _, e := runCLI(t, "backup", "compact")
	if code != 1 || !strings.Contains(e, "Stop vaquero first: vaquero stop") {
		t.Fatalf("%d %q", code, e)
	}
}

// Review Focus 3: a snapshot first, then the compacted copy, with its
// search indexes rebuilt, swapped in.
func TestCompactShrinksAndKeepsEverything(t *testing.T) {
	dataDir, before := bloated(t)
	path := filepath.Join(dataDir, "vaquero.db")
	dropFromSearch(t, path)
	setNow(t, time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	code, out, e := runCLI(t, "backup", "compact")
	if code != 0 {
		t.Fatalf("%d %q %q", code, out, e)
	}
	name := appdb.BackupNameKind(appdb.KindPreCompact, time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	if !strings.Contains(out, "Compacted the database from ") || !strings.Contains(out, "The database before it is saved as "+name+".") {
		t.Fatalf("output %q", out)
	}
	if after := dbSize(t, path); after >= before/2 {
		t.Fatalf("database is %d bytes after compacting, %d before", after, before)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "backups", name)); err != nil {
		t.Fatalf("no snapshot before compacting: %v", err)
	}
	left, _ := filepath.Glob(path + ".compact*")
	if len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
	d, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if hits, err := appdb.SearchTickets(t.Context(), d, []string{`"lasso"*`}, 10); err != nil || len(hits) != 1 {
		t.Fatalf("search after compacting: %d hits, %v", len(hits), err)
	}
	if _, out, _ := runCLI(t, "tickets", "VAQ"); !strings.Contains(out, "Rope the lasso") {
		t.Fatalf("tickets after compacting:\n%s", out)
	}
}

// A compacted copy that fails its checks is deleted, and the database
// stays the very file it was.
func TestCompactFailedCheckChangesNothing(t *testing.T) {
	dataDir, _ := bloated(t)
	path := filepath.Join(dataDir, "vaquero.db")
	old := verifyCompacted
	verifyCompacted = func(context.Context, *sql.DB, map[string]int64) error {
		return errors.New("1 row in tickets is missing")
	}
	t.Cleanup(func() { verifyCompacted = old })
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	code, _, e := runCLI(t, "backup", "compact")
	if code != 1 || !strings.Contains(e, "1 row in tickets is missing") || !strings.Contains(e, "nothing changed") {
		t.Fatalf("%d %q", code, e)
	}
	now, err := os.Stat(path)
	if err != nil || !os.SameFile(st, now) {
		t.Fatalf("the database file was replaced (%v)", err)
	}
	left, _ := filepath.Glob(path + ".compact*")
	if len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
	if _, out, _ := runCLI(t, "tickets", "VAQ"); !strings.Contains(out, "Rope the lasso") {
		t.Fatalf("tickets:\n%s", out)
	}
}

// Without a snapshot there is no compaction.
func TestCompactWithoutSnapshotChangesNothing(t *testing.T) {
	dataDir, _ := bloated(t)
	path := filepath.Join(dataDir, "vaquero.db")
	backups := filepath.Join(dataDir, "backups")
	os.RemoveAll(backups)
	if err := os.WriteFile(backups, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	code, _, e := runCLI(t, "backup", "compact")
	if code != 1 || !strings.Contains(e, "nothing changed") {
		t.Fatalf("%d %q", code, e)
	}
	if now, err := os.Stat(path); err != nil || !os.SameFile(st, now) {
		t.Fatalf("the database file was replaced (%v)", err)
	}
	if left, _ := filepath.Glob(path + ".compact*"); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestCompactWithoutDatabase(t *testing.T) {
	localEnv(t)
	code, _, e := runCLI(t, "backup", "compact")
	if code != 1 || !strings.Contains(e, "No database to compact yet.") {
		t.Fatalf("%d %q", code, e)
	}
}

// rowCounts leaves out only the search indexes' tables: an underscore in a
// LIKE pattern is a wildcard, so "gifts" must not read as "_fts".
func TestRowCountsSkipsOnlySearchTables(t *testing.T) {
	d, err := appdb.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`CREATE TABLE gifts (id INTEGER)`,
		`INSERT INTO gifts VALUES (1), (2)`,
		`CREATE VIRTUAL TABLE tickets_fts USING fts5(title)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := rowCounts(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got["gifts"] != 2 {
		t.Fatalf("gifts left out: %v", got)
	}
	for name := range got {
		if strings.Contains(name, "_fts") {
			t.Fatalf("%s counted: %v", name, got)
		}
	}
}
