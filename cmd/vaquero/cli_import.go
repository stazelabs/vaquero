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

// openBlobStore is blobs.Open, replaceable by a test.
var openBlobStore = blobs.Open

// importFile is the database an import builds beside the current one.
const importFile = "vaquero.import.db"

// cmdImport is vaquero import FILE: an export restored into an empty
// instance, with the server stopped. It is the one writer of the tracker
// tables outside core, and runs offline.
//
// All or nothing: the archive is read and checked (its attachment bytes
// spooled into {data dir}/import-spool-…, removed on every way out), a new database is built
// at {data dir}/vaquero.import.db (attachment bytes put into the configured
// store first, under a fresh instance id), checked, and only then swapped
// in by swapDatabase. Until the swap, any failure deletes the new file and
// every object put, and the current database is never written. A kill
// (no cleanup) leaves import.pending for the next import or serve
// (cleanPending).
func cmdImport(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	if len(args) > 0 && args[0] == "linear" {
		return cmdImportLinear(ctx, stdout, stderr, args[1:])
	}
	c := newCmd(stdout, stderr, "vaquero import FILE")
	pos, err := c.parse(args, 1, 1)
	if err != nil {
		return c.finish(err)
	}
	// The data directory and the attachment storage it fills, not the
	// server's log settings.
	cfg, err := config.LocalWithBlobs(os.Getenv)
	if err != nil {
		return c.finish(err)
	}
	res, err := importArchive(ctx, cfg, pos[0], c.stderr)
	if err != nil {
		return c.finish(err)
	}
	c.emit(res, func() {
		fmt.Fprintf(c.stdout, "Imported %s, %s and %s into %s. Saved servers holding a token for the old instance need a new one: vaquero token new.\n",
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

// importArchive runs the import; warn gets the lines about an earlier,
// interrupted import's leftovers.
func importArchive(ctx context.Context, cfg config.Config, file string, warn io.Writer) (importResult, error) {
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
	instance := strings.ToLower(idgen.New())
	store, err := importStore(ctx, cfg)
	if err != nil {
		return res, notImported{err}
	}
	// What an import killed before it finished left in the store.
	current, err := currentInstance(ctx, cfg.DBPath)
	if err != nil {
		return res, notImported{err}
	}
	// A record that can't be cleaned stops the import: writing over it
	// would lose track of those files.
	if n, err := cleanPending(ctx, cfg, store, current); err != nil {
		return res, notImported{err}
	} else if n > 0 {
		fmt.Fprintf(warn, "Removed %s an interrupted import left in %s.\n", count(n, "file"), cfg.BlobBackend)
	}
	removeSpools(cfg.DataDir)
	rd, err := archive.Read(f, cfg.DataDir, importCopies(cfg))
	if err != nil {
		return res, notImported{err}
	}
	defer rd.Close()
	res.Previous, err = buildAndSwap(ctx, cfg, rd, store, instance)
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

// currentInstance is the instance id the database at path records, or ""
// when there is no database or no id yet.
func currentInstance(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	db, err := appdb.OpenReader(path, 1)
	if err != nil {
		return "", fmt.Errorf("couldn't read the current database: %w", err)
	}
	defer db.Close()
	s, err := gen.New(db).GetSetting(ctx, "instance_id")
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("couldn't read the current database: %w", err)
	}
	return s.Value, nil
}

// removeSpools deletes the attachment spools an interrupted import left in
// dataDir; the caller holds its lock, so none is in use.
func removeSpools(dataDir string) {
	left, _ := filepath.Glob(filepath.Join(dataDir, archive.SpoolPrefix+"*"))
	for _, p := range left {
		_ = os.RemoveAll(p)
	}
}

// removeDatabase deletes a database file and its WAL and shared memory.
func removeDatabase(path string) {
	for _, ext := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + ext)
	}
}

// sameFilesystem is appdb.SameFilesystem, replaceable by a test.
var sameFilesystem = appdb.SameFilesystem

// importCopies is how many times an import puts the attachment bytes on the
// data directory's volume: the spool, then the new database (SQLite
// storage) or the store directory when it is on the same volume.
func importCopies(cfg config.Config) int {
	switch cfg.BlobBackend {
	case "sqlite":
		return 2
	case "dir":
		if same, err := sameFilesystem(cfg.DataDir, cfg.BlobDir); err != nil || same {
			return 2 // when in doubt, ask for the room
		}
	}
	return 1
}

// importProbeKey is the empty object importStore puts and deletes. One
// fixed key, outside every instance, so a store that refuses the delete
// keeps one probe however often the import is retried.
const importProbeKey = "attachments/import-probe"

// importStore opens the directory or S3 store and checks it takes a write,
// before the archive is read or anything written: bad credentials or a
// read-only bucket stop the import at once, not at the first attachment. The
// check puts and deletes importProbeKey. When the delete fails the import
// stops and the error names the key to remove by hand. For SQLite storage,
// the new database file itself, it returns nil.
func importStore(ctx context.Context, cfg config.Config) (blobs.Store, error) {
	if cfg.BlobBackend == "sqlite" {
		return nil, nil
	}
	store, err := openBlobStore(cfg, nil, nil, time.Now)
	if err != nil {
		return nil, fmt.Errorf("attachment store: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, blobs.PutTimeout(0))
	defer cancel()
	if err := store.Put(pctx, importProbeKey, strings.NewReader(""), 0, "application/octet-stream"); err != nil {
		return nil, fmt.Errorf("attachment store: can't write to it (%w)", err)
	}
	if err := store.Delete(pctx, importProbeKey); err != nil {
		return nil, fmt.Errorf("attachment store: can't delete from it (%w); remove %s from it by hand", err, importProbeKey)
	}
	return store, nil
}

// buildAndSwap builds the new database from rd and swaps it in, returning
// where the database before it went. Attachment bytes go into store under
// instance (store nil: into the new database). On any failure before the
// swap it removes the new file and every object it put.
func buildAndSwap(ctx context.Context, cfg config.Config, rd *archive.Reader, store blobs.Store, instance string) (aside string, err error) {
	newPath := filepath.Join(cfg.DataDir, importFile)
	// A file left by an interrupted import goes; the lock is ours.
	removeDatabase(newPath)
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
		clean := true
		for _, k := range put {
			if err := store.Delete(dctx, k); err != nil && !errors.Is(err, blobs.ErrNotFound) {
				clean = false
			}
		}
		if cfg.BlobBackend == "dir" {
			_ = os.Remove(filepath.Join(cfg.BlobDir, "attachments", instance)) // only when empty
		}
		if clean {
			removePending(cfg.DataDir) // else the next import or serve tries again
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
	if cfg.BlobBackend != "sqlite" && len(atts) > 0 {
		// Before the first put: a kill from here on leaves the bytes for
		// the next import or serve to delete.
		if err = writePending(cfg, instance); err != nil {
			return "", err
		}
	}
	for _, a := range atts {
		id := a["id"].(string) // Read checked it
		body, size, _ := rd.Blob(id)
		ctype, _ := a["content_type"].(string)
		key := blobs.AttachmentKey(instance, id)
		put = append(put, key) // before Put: a half-written object is deleted too
		pctx, cancel := context.WithTimeout(ctx, blobs.PutTimeout(size))
		err = store.Put(pctx, key, body, size, ctype)
		cancel()
		if body != nil {
			body.Close()
		}
		if err != nil {
			return "", fmt.Errorf("store attachment %s: %w", id, err)
		}
	}

	if err = fill(ctx, w, rd, instance, cfg); err != nil {
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
	removePending(cfg.DataDir)
	return aside, nil
}

// fill loads rd in one transaction, keys the attachments under instance
// and records the instance, its blob backend and that backend's location.
func fill(ctx context.Context, w *sql.DB, rd *archive.Reader, instance string, cfg config.Config) error {
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
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "blob_store", Value: cfg.BlobBackend, UpdatedAt: now}); err != nil {
		return err
	}
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "blob_location", Value: blobs.Location(cfg), UpdatedAt: now}); err != nil {
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
