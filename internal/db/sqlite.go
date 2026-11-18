// Package db opens the SQLite pools and runs migrations.
//
// Migrations (goose, embedded) start from 0001_init.sql and are append-only
// and forward-only. Migrate refuses a database whose applied version is
// beyond the newest embedded file (one written by a newer vaquero), so a
// rollback after an upgrade stops at boot instead of running on an unknown
// schema.
//
// Two pools over one WAL file: a single-connection writer (every write and
// every transaction) and a query_only reader pool (a stray write fails with
// "attempt to write a readonly database").
//
// Writer transactions begin immediate (_txlock=immediate). A deferred BEGIN
// takes the write lock at the first write, so a transaction that reads first
// and meets a write from another connection in between fails at once with
// SQLITE_BUSY, because its snapshot is stale and busy_timeout cannot help.
// BEGIN IMMEDIATE takes the lock up front and waits out busy_timeout instead.
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Open opens the writer pool: one connection, WAL, foreign keys, 5s busy
// timeout, transactions begin immediate.
func Open(path string) (*sql.DB, error) {
	p := url.Values{}
	p.Set("_txlock", "immediate")
	p.Add("_pragma", "journal_mode(WAL)")
	p.Add("_pragma", "busy_timeout(5000)")
	p.Add("_pragma", "foreign_keys(on)")
	p.Add("_pragma", "synchronous(NORMAL)")
	addPerf(p)
	db, err := openDSN(path, p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// OpenReader opens the reader pool. Open the writer and migrate first.
func OpenReader(path string, maxConns int) (*sql.DB, error) {
	if maxConns < 1 {
		maxConns = 1
	}
	p := url.Values{}
	p.Add("_pragma", "busy_timeout(5000)")
	p.Add("_pragma", "foreign_keys(on)")
	p.Add("_pragma", "query_only(true)")
	addPerf(p)
	db, err := openDSN(path, p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	return db, nil
}

func addPerf(p url.Values) {
	p.Add("_pragma", "cache_size(-20000)")
	p.Add("_pragma", "temp_store(MEMORY)")
}

func openDSN(path string, p url.Values) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	// SQLite reads the DSN as a URI, so the path must be escaped: a raw "#",
	// "?" or "%" in a directory name would cut it short or garble it.
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: p.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// Checkpoint folds the WAL into the main file. Call on the writer at shutdown.
func Checkpoint(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	return nil
}
