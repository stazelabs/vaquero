package db_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	appdb "github.com/stazelabs/vaquero/internal/db"
)

// A transaction that reads and then writes must wait out another process's
// write, not fail with SQLITE_BUSY_SNAPSHOT. That needs BEGIN IMMEDIATE.
func TestWriterTxReadThenWriteWaitsForOtherWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	a, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := a.Exec(`CREATE TABLE t (n INTEGER)`); err != nil {
		t.Fatal(err)
	}

	atx, err := a.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := atx.Exec(`INSERT INTO t VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		btx, err := b.BeginTx(ctx, nil)
		if err != nil {
			done <- err
			return
		}
		var n int
		if err := btx.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
			_ = btx.Rollback()
			done <- err
			return
		}
		if _, err := btx.Exec(`INSERT INTO t VALUES (2)`); err != nil {
			_ = btx.Rollback()
			done <- err
			return
		}
		done <- btx.Commit()
	}()

	time.Sleep(300 * time.Millisecond)
	if err := atx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("read-then-write tx failed instead of waiting: %v", err)
	}
	var n int
	if err := a.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows = %d, %v; want 2", n, err)
	}
}
