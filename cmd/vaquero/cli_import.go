package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/archive"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func init() { cliCommands["import"] = cmdImport }

// importFile is the database an import builds beside the current one.
const importFile = "vaquero.import.db"

// cmdImport is vaquero import FILE: an export restored into an empty
// instance, with the server stopped. It is the one writer of the tracker
// tables outside core, and runs offline.
//
// All or nothing: the archive is read and checked, a new database is built
// at {data dir}/vaquero.import.db (attachment bytes put into the configured
// store first, under a fresh instance id), checked, and only then swapped
// in by swapDatabase. Until the swap, any failure deletes the new file and
// every object put, and the current database is never written.
func cmdImport(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero import FILE")
	pos, err := c.parse(args, 1, 1)
	if err != nil {
		return c.finish(err)
	}
	cfg, err := localConfig()
	if err != nil {
		return c.finish(err)
	}
	res, err := importArchive(ctx, cfg, pos[0])
	if err != nil {
		return c.finish(err)
	}
	c.emit(res, func() {
		fmt.Fprintf(c.stdout, "Imported %s, %s and %s into %s. Profiles holding a token for the old instance need a new one: vaquero token create.\n",
			count(res.Projects, "project"), count(res.Tickets, "ticket"), count(res.Attachments, "attachment"), res.Backend)
		if line := savedLine(cfg, res.Previous); line != "" {
			fmt.Fprintln(c.stdout, line)
		}
	})
	return exitOK
}

// importResult is what an import did, as --json prints it.
type importResult struct {
	Projects    int    `json:"projects"`
	Tickets     int    `json:"tickets"`
	Attachments int    `json:"attachments"`
	Backend     string `json:"backend"`
	Previous    string `json:"previous,omitempty"` // where the database before it went
}

// count is n and noun, plural unless n is 1, "no" for 0.
func count(n int, noun string) string {
	switch n {
	case 0:
		return "no " + noun + "s"
	case 1:
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// notImported is a failed import, which changed nothing.
type notImported struct{ err error }

func (e notImported) Error() string {
	if _, ok := errors.AsType[*swapFailed](e.err); ok {
		return e.err.Error() // the old database moved: that, not "nothing", is the news
	}
	msg := e.err.Error()
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg + " Nothing was imported."
}
func (e notImported) Unwrap() error { return e.err }

func importArchive(ctx context.Context, cfg config.Config, file string) (importResult, error) {
	res := importResult{Backend: cfg.BlobBackend}
	f, err := os.Open(file)
	if err != nil {
		return res, fmt.Errorf("can't read %s: %w", file, errUnwrapped(err))
	}
	defer f.Close()
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return res, fmt.Errorf("data directory: %w", err)
	}
	release, err := lockStopped(cfg.DataDir)
	if err != nil {
		return res, err
	}
	defer release()
	if err := checkEmpty(ctx, cfg.DBPath); err != nil {
		return res, err
	}
	rd, err := archive.Read(f)
	if err != nil {
		return res, notImported{err}
	}
	defer rd.Close()
	res.Previous, err = buildAndSwap(ctx, cfg, rd)
	if err != nil {
		return res, notImported{err}
	}
	res.Projects, res.Tickets, res.Attachments = len(rd.Tables["projects"]), len(rd.Tables["tickets"]), len(rd.Tables["attachments"])
	// The old token names an owner token the new database doesn't have; the
	// next serve mints one for the imported owner.
	if err := os.Remove(filepath.Join(cfg.DataDir, TokenFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("the import finished, but the old token file couldn't be removed: %w", err)
	}
	return res, nil
}

// checkEmpty refuses a database holding any project or ticket. A missing
// one is empty; so is one that has only been served (its owner, token,
// their events and settings are replaced).
func checkEmpty(ctx context.Context, path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := appdb.OpenReader(path, 1)
	if err != nil {
		return notImported{fmt.Errorf("couldn't read the current database: %w", err)}
	}
	defer db.Close()
	var projects, tickets int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM projects), (SELECT count(*) FROM tickets)`).Scan(&projects, &tickets); err != nil {
		return notImported{fmt.Errorf("couldn't read the current database: %w", err)}
	}
	if projects+tickets > 0 {
		return message(fmt.Sprintf("Import needs an empty instance; this one has %s and %s.", count(projects, "project"), count(tickets, "ticket")))
	}
	return nil
}

// removeDatabase deletes a database file and its WAL and shared memory.
func removeDatabase(path string) {
	for _, ext := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + ext)
	}
}

// buildAndSwap builds the new database from rd and swaps it in, returning
// where the database before it went. On any failure before the swap it
// removes the new file and every object it put.
func buildAndSwap(ctx context.Context, cfg config.Config, rd *archive.Reader) (aside string, err error) {
	newPath := filepath.Join(cfg.DataDir, importFile)
	// The directory and S3 stores are opened before anything is written, so
	// a bad setting stops the import at once. SQLite storage is the new file.
	var store blobs.Store
	if cfg.BlobBackend != "sqlite" {
		if store, err = blobs.Open(cfg, nil, nil, time.Now); err != nil {
			return "", fmt.Errorf("attachment store: %w", err)
		}
	}
	removeDatabase(newPath) // left by an interrupted import; the lock is ours
	instance := strings.ToLower(idgen.New())
	var put []string // every key put, deleted again on failure
	var w, r *sql.DB
	closeDB := func() {
		if r != nil {
			r.Close()
			r = nil
		}
		if w != nil {
			w.Close()
			w = nil
		}
	}
	swapped := false
	defer func() {
		closeDB()
		if swapped {
			return
		}
		removeDatabase(newPath)
		if cfg.BlobBackend == "sqlite" {
			return // the bytes went into the file just removed
		}
		// Ctrl-C may have ended ctx; the cleanup must still run.
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		for _, k := range put {
			_ = store.Delete(dctx, k)
		}
		if cfg.BlobBackend == "dir" {
			_ = os.Remove(filepath.Join(cfg.BlobDir, "attachments", instance)) // only when empty
		}
	}()

	if w, err = appdb.Open(newPath); err != nil {
		return "", err
	}
	if err = appdb.Migrate(w); err != nil {
		return "", err
	}
	if r, err = appdb.OpenReader(newPath, 2); err != nil {
		return "", err
	}
	if store == nil {
		store = blobs.NewSQLite(w, r, time.Now)
	}

	atts := rd.Tables["attachments"]
	for _, a := range atts {
		id := a["id"].(string) // Read checked it
		body, size, _ := rd.Blob(id)
		ctype, _ := a["content_type"].(string)
		key := blobs.AttachmentKey(instance, id)
		put = append(put, key) // before Put: a half-written object is deleted too
		pctx, cancel := context.WithTimeout(ctx, blobs.PutDeadline)
		err = store.Put(pctx, key, body, size, ctype)
		cancel()
		if body != nil {
			body.Close()
		}
		if err != nil {
			return "", fmt.Errorf("store attachment %s: %w", id, err)
		}
	}

	if err = fill(ctx, w, rd, instance, cfg.BlobBackend); err != nil {
		return "", err
	}
	if err = checkDatabase(ctx, w); err != nil {
		return "", err
	}
	if err = appdb.Checkpoint(w); err != nil {
		return "", err
	}
	closeDB()
	// Closed after a full checkpoint, the WAL holds nothing; the next open
	// starts a fresh one.
	if st, err := os.Stat(newPath + "-wal"); err == nil && st.Size() > 0 {
		return "", fmt.Errorf("the new database's write-ahead log wasn't folded in")
	}
	_ = os.Remove(newPath + "-wal")
	_ = os.Remove(newPath + "-shm")

	if aside, err = swapDatabase(ctx, cfg, newPath); err != nil {
		return "", err
	}
	swapped = true
	return aside, nil
}

// fill loads rd in one transaction, keys the attachments under instance
// and records the instance and its blob backend.
func fill(ctx context.Context, w *sql.DB, rd *archive.Reader, instance, backend string) error {
	tx, err := w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := archive.Load(ctx, tx, rd); err != nil {
		return err
	}
	for _, a := range rd.Tables["attachments"] {
		id := a["id"].(string)
		if _, err := tx.ExecContext(ctx, `UPDATE attachments SET blob_key = ? WHERE id = ?`, blobs.AttachmentKey(instance, id), id); err != nil {
			return fmt.Errorf("key attachment %s: %w", id, err)
		}
	}
	q := gen.New(tx)
	now := timefmt.Format(time.Now())
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "instance_id", Value: instance, UpdatedAt: now}); err != nil {
		return err
	}
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "blob_store", Value: backend, UpdatedAt: now}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("the export's records don't fit together (%w)", err)
	}
	return nil
}

// checkDatabase runs SQLite's integrity and foreign key checks.
func checkDatabase(ctx context.Context, w *sql.DB) error {
	var ok string
	if err := w.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&ok); err != nil {
		return err
	}
	if ok != "ok" {
		return fmt.Errorf("the new database failed its integrity check: %s", ok)
	}
	rows, err := w.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			return err
		}
		return fmt.Errorf("a row in %s refers to a missing %s", table, parent)
	}
	return rows.Err()
}
