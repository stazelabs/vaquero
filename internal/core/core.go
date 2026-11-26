// Package core is the only code that changes the tracker.
//
// Every operation runs in one writer transaction: it validates its input,
// makes its change, appends exactly one row to events, and commits. A
// refused operation (*Invalid or ErrNotFound) writes nothing. The web UI,
// and later the REST API and MCP, all call these functions, so they cannot
// disagree about what an operation does.
//
// Reads that only display data do not go through core; callers use the
// reader queries directly.
package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/stazelabs/vaquero/internal/db/gen"
)

// Core makes changes to the tracker.
type Core struct {
	DB  *sql.DB          // the writer pool
	Now func() time.Time // injected so tests control time

	// BlobStore names the attachment backend ("sqlite", "dir" or "s3"). The
	// first attachment records it in settings when nothing has yet.
	BlobStore string
	// BlobLocation is where that backend keeps the bytes (blobs.Location),
	// recorded with BlobStore as blob_location.
	BlobLocation string
}

// New returns a Core writing through db.
func New(db *sql.DB, now func() time.Time) *Core { return &Core{DB: db, Now: now} }

// tx runs fn in one writer transaction, committing when it returns nil.
func (c *Core) tx(ctx context.Context, fn func(q *gen.Queries) error) error {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback after a commit is a no-op; after an error or a panic it
	// frees the writer's only connection.
	defer func() { _ = tx.Rollback() }()
	if err := fn(gen.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// nullString is s as a nullable column: NULL when empty.
func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
