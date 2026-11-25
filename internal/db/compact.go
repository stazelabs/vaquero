package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
)

// RebuildSearch rebuilds both search indexes from the tickets and comments
// they index, leaving deleted comments out as the triggers do. An import
// runs it after loading, and CompactInto in its copy.
func RebuildSearch(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		"INSERT INTO tickets_fts (tickets_fts) VALUES ('rebuild')",
		// A rebuild indexes every comment; the triggers index only live ones.
		"INSERT INTO comments_fts (comments_fts) VALUES ('rebuild')",
		"INSERT INTO comments_fts (comments_fts, rowid, body) SELECT 'delete', rowid, body FROM comments WHERE deleted_at IS NOT NULL",
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("rebuild the search index: %w", err)
		}
	}
	return nil
}

// CompactInto writes a compacted copy of src's database to dest, a new
// file: VACUUM INTO after the free-space check a snapshot makes, then both
// search indexes rebuilt in the copy, because a vacuum may renumber the
// rowids they are keyed on. src itself is only read; never VACUUM the live
// file. On an error, dest and its journal files are removed.
func CompactInto(ctx context.Context, src *sql.DB, dest string) (err error) {
	defer func() {
		if err != nil {
			cleanTemp(dest)
		}
	}()
	path, err := mainFile(ctx, src)
	if err != nil {
		return err
	}
	size, err := Size(ctx, src)
	if err != nil {
		return err
	}
	free, err := freeBytes(filepath.Dir(dest))
	if err != nil {
		return fmt.Errorf("check free space: %w", err)
	}
	if need := RequiredFree(size); free < need {
		return fmt.Errorf("%w: %d bytes free, %d needed", ErrLowSpace, free, need)
	}
	cleanTemp(dest) // a leftover from a crash
	if err := vacuumInto(ctx, path, dest); err != nil {
		return err
	}
	c, err := openCopyConn(dest)
	if err != nil {
		return err
	}
	defer c.Close()
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := RebuildSearch(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return c.Close()
}
