package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/units"
)

// compactSuffix names the compacted copy, built beside the database.
const compactSuffix = ".compact"

// verifyCompacted checks the compacted copy before it is swapped in: SQLite's
// integrity and foreign key checks, and every table holding the rows it had.
// A test replaces it.
var verifyCompacted = func(ctx context.Context, w *sql.DB, want map[string]int64) error {
	if err := checkDatabase(ctx, w); err != nil {
		return err
	}
	got, err := rowCounts(ctx, w)
	if err != nil {
		return err
	}
	for table, n := range want {
		if got[table] != n {
			return fmt.Errorf("%s has %d rows in the copy, %d in the database", table, got[table], n)
		}
	}
	return nil
}

// rowCounts is the row count of every table but the search indexes',
// which the copy rebuilds.
func rowCounts(ctx context.Context, d *sql.DB) (map[string]int64, error) {
	rows, err := d.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%'`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		tables = append(tables, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, t := range tables {
		var n int64
		if err := d.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(t, `"`, `""`)+`"`).Scan(&n); err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, nil
}

// backupCompact is `vaquero backup compact`: it gives back the space that
// deleted rows (swept attachment bytes, most of all) leave in the file. All
// or nothing, with the server stopped: a snapshot of the database first,
// then a compacted copy beside it (VACUUM INTO, never a VACUUM of the live
// file) with both search indexes rebuilt, checked, and only then swapped in.
// On any failure before the swap the copy is deleted and the database is the
// file it was.
func backupCompact(ctx context.Context, c *cmd, cfg config.Config, dir string) error {
	if _, err := os.Stat(cfg.DBPath); errors.Is(err, os.ErrNotExist) {
		return message("No database to compact yet.")
	} else if err != nil {
		return err
	}
	release, err := lockStopped(cfg.DataDir)
	if err != nil {
		return err
	}
	defer release()

	cur, err := appdb.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer cur.Close()
	applied, newest, err := appdb.Pending(cur)
	if err != nil {
		return err
	}
	switch {
	case applied > newest:
		return message("The database was written by a newer vaquero; compact it with that version or newer.")
	case applied < newest:
		return message("The database is from an older vaquero. Start vaquero once so it upgrades the database, then compact it.")
	}
	before, err := appdb.Size(ctx, cur)
	if err != nil {
		return err
	}
	want, err := rowCounts(ctx, cur)
	if err != nil {
		return err
	}
	info, err := appdb.SnapshotNext(ctx, cur, dir, appdb.KindPreCompact, localNow())
	if err != nil {
		return fmt.Errorf("couldn't save a snapshot first, so nothing changed: %w", err)
	}
	_, _ = appdb.PruneBackups(dir, appdb.BackupKeep)
	aside := filepath.Join(dir, info.Name)

	tmp := cfg.DBPath + compactSuffix
	swapped := false
	defer func() {
		if !swapped {
			removeDatabase(tmp)
		}
	}()
	notDone := func(err error) error {
		return fmt.Errorf("couldn't compact the database, so nothing changed: %w", err)
	}
	if err := appdb.CompactInto(ctx, cur, tmp); err != nil {
		return notDone(err)
	}
	if err := cur.Close(); err != nil {
		return notDone(err)
	}
	w, err := appdb.Open(tmp)
	if err != nil {
		return notDone(err)
	}
	after, err := appdb.Size(ctx, w)
	if err == nil {
		err = verifyCompacted(ctx, w, want)
	}
	if err == nil {
		err = appdb.Checkpoint(w)
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return notDone(err)
	}
	if st, err := os.Stat(tmp + "-wal"); err == nil && st.Size() > 0 {
		return notDone(errors.New("the copy's write-ahead log wasn't folded in"))
	}
	_ = os.Remove(tmp + "-wal")
	_ = os.Remove(tmp + "-shm")

	if _, err := installDatabase(cfg, tmp, aside, false); err != nil {
		return err
	}
	swapped = true
	fmt.Fprintf(c.stdout, "Compacted the database from %s to %s. %s\n", units.SizeWords(before), units.SizeWords(after), savedLine(cfg, aside))
	return nil
}
