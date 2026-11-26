package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
)

const moveUsage = "vaquero attachments move --to sqlite|dir|s3 [--delete-old]"

// moveFile, in the data directory, records a move from before its first
// copy until it has switched (and, with --delete-old, deleted the old
// copies). Its first line is the movePlan as JSON; every later line names
// one copy already made and checked (moveDone). An interrupted move leaves
// it, and the next move resumes from it, cleans up after it or finishes
// it.
const moveFile = "move.pending"

// movePlace is a storage backend and its blobs.Location.
type movePlace struct {
	Backend  string `json:"backend"`
	Location string `json:"location"`
}

// where names p for a message: the backend, and the directory (resolved
// under dataDir) or bucket.
func (p movePlace) where(dataDir string) string {
	if p.Location == "" {
		return p.Backend
	}
	return p.Backend + " " + blobs.ResolveLocation(p.Location, dataDir)
}

// movePlan is moveFile's first line.
type movePlan struct {
	Instance  string    `json:"instance"`
	From      movePlace `json:"from"`
	To        movePlace `json:"to"`
	DeleteOld bool      `json:"delete_old"`
}

// moveDone is one copy in the target, made and checked.
type moveDone struct {
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

// moveResult is what a move did, as --json prints it.
type moveResult struct {
	Backend    string `json:"backend"`
	Location   string `json:"location"`
	Moved      int    `json:"moved"`       // attachments now in the target
	Copied     int    `json:"copied"`      // copied by this run
	Earlier    int    `json:"earlier"`     // copied and checked by an interrupted run
	DeletedOld bool   `json:"deleted_old"` // the old copies are gone
	Finished   bool   `json:"finished"`    // only an interrupted run's last steps were left
	From       string `json:"from"`
}

// moveFreeBytes is the free-space reading, replaceable by a test.
var moveFreeBytes = appdb.FreeBytes

// cmdAttachmentsMove is vaquero attachments move: every attachment's bytes
// copied to other storage, with the server stopped.
//
// Each file is read from the storage the database records (blob_store,
// blob_location), put into the target under the same key, then read back
// and compared with the original by size and SHA-256. Only once every copy
// is in place and checked are blob_store and blob_location switched, in
// one transaction; until then the old storage is the one in use, and an
// interruption at any step leaves it so. --delete-old deletes the old
// copies, previews included, after the switch. Previews (thumbs/) are not
// copied: the server makes them again on demand.
func cmdAttachmentsMove(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, moveUsage)
	to := c.fs.String("to", "", "the storage to move to: sqlite, dir or s3")
	deleteOld := c.fs.Bool("delete-old", false, "delete the old copies once the move is done")
	if _, err := c.parse(args, 0, 0); err != nil {
		return c.finish(err)
	}
	target := strings.ToLower(strings.TrimSpace(*to))
	switch target {
	case "sqlite", "dir", "s3":
	default:
		return c.finish(usageError{usage: moveUsage, hint: "--to names the storage to move to: sqlite, dir or s3"})
	}
	// The target is what serve will be started with afterwards, so a
	// VQ_BLOB_STORE naming something else is a mistake to catch now.
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("VQ_BLOB_STORE"))); env != "" && env != target {
		return c.finish(message(fmt.Sprintf("--to is %s, but VQ_BLOB_STORE is %s. Set VQ_BLOB_STORE to %s, or unset it, and run the move again.", target, env, target)))
	}
	cfg, err := config.LocalWithBlobs(func(k string) string {
		if k == "VQ_BLOB_STORE" {
			return target
		}
		return os.Getenv(k)
	})
	if err != nil {
		return c.finish(err)
	}
	res, err := moveAttachments(ctx, cfg, *deleteOld, os.Getenv, c.stderr)
	if err != nil {
		return c.finish(err)
	}
	c.emit(res, func() { printMove(c.stdout, cfg, res) })
	return exitOK
}

// printMove is a move's report and what to start the server with.
func printMove(w io.Writer, cfg config.Config, res moveResult) {
	switch {
	case res.Moved == 0 && !res.Finished:
		fmt.Fprintf(w, "There are no attachments to move. Start vaquero with %s to store new ones there.\n", startWith(cfg))
		return
	case res.Finished:
		fmt.Fprintf(w, "The earlier move to %s finished.", res.Backend)
	default:
		fmt.Fprintf(w, "Moved %s to %s", count(res.Moved, "attachment"), res.Backend)
		if res.Earlier > 0 {
			fmt.Fprintf(w, " (%d copied by an earlier run)", res.Earlier)
		}
		fmt.Fprint(w, ", each copy checked against the original.")
	}
	if res.DeletedOld {
		fmt.Fprintf(w, " Deleted the old copies from %s.", res.From)
		if strings.HasPrefix(res.From, "sqlite") {
			fmt.Fprint(w, " vaquero backup compact gives their space back.")
		}
	} else if !res.Finished {
		fmt.Fprintf(w, " The old copies are still in %s; nothing uses them now.", res.From)
	}
	fmt.Fprintf(w, "\nStart vaquero with %s; it refuses any other storage now.\n", startWith(cfg))
}

// startWith names the settings serve needs for cfg's storage.
func startWith(cfg config.Config) string {
	switch cfg.BlobBackend {
	case "dir":
		if blobs.Location(cfg) == blobs.DataDirMarker+"/attachments" {
			return "VQ_BLOB_STORE=dir"
		}
		return "VQ_BLOB_STORE=dir and VQ_BLOB_DIR=" + cfg.BlobDir
	case "s3":
		return "VQ_BLOB_STORE=s3 and these VQ_S3_ settings"
	}
	return "VQ_BLOB_STORE=sqlite (or unset)"
}

// notMoved is a move that stopped before the switch, which leaves the old
// storage in use.
type notMoved struct {
	err  error
	from movePlace
}

func (e notMoved) Error() string {
	msg := e.err.Error()
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return msg + " Attachments are still stored in " + e.from.Backend + "; run the same command again to resume."
}
func (e notMoved) Unwrap() error { return e.err }

// moveItem is one attachment to move.
type moveItem struct {
	id, key, ctype, ticket string
	size                   int64
}

// moveAttachments runs the move to cfg's storage; get reads the
// environment (the old bucket's keys) and warn gets the lines about an
// interrupted move's leftovers.
func moveAttachments(ctx context.Context, cfg config.Config, deleteOld bool, get func(string) string, warn io.Writer) (moveResult, error) {
	to := movePlace{Backend: cfg.BlobBackend, Location: blobs.Location(cfg)}
	res := moveResult{Backend: to.Backend, Location: to.Location}
	if _, err := os.Stat(cfg.DBPath); errors.Is(err, os.ErrNotExist) {
		return res, message("There's no Vaquero database in " + cfg.DataDir + ".")
	}
	release, err := lockStopped(cfg.DataDir)
	if err != nil {
		return res, err
	}
	defer release()
	w, err := appdb.Open(cfg.DBPath)
	if err != nil {
		return res, err
	}
	defer w.Close()
	r, err := appdb.OpenReader(cfg.DBPath, 2)
	if err != nil {
		return res, err
	}
	defer r.Close()
	if err := checkCurrentSchema(r); err != nil {
		return res, err
	}
	q := gen.New(r)
	instance, from, items, err := moveState(ctx, r, q)
	if err != nil {
		return res, err
	}
	res.From = from.where(cfg.DataDir)

	// An earlier, interrupted move.
	plan, done, err := readMovePlan(cfg.DataDir)
	if err != nil {
		return res, err
	}
	if plan != nil {
		switch {
		case plan.Instance != instance:
			return res, plan.byHand(cfg.DataDir, "it is for another instance")
		case plan.To == from:
			// It switched; only its deletes may be left.
			if to == from || sameTarget(plan.To, cfg) {
				res.Finished, res.From = true, plan.From.where(cfg.DataDir)
				res.Moved = len(items)
				if plan.DeleteOld || deleteOld {
					if err := deleteOldCopies(ctx, cfg, plan.From, instance, items, w, r, get); err != nil {
						return res, err
					}
					res.DeletedOld = true
				}
				removeMovePlan(cfg.DataDir)
				return res, nil
			}
			if plan.DeleteOld {
				return res, message(fmt.Sprintf("An earlier move to %s finished before it deleted the old copies in %s. Run vaquero attachments move --to %s --delete-old to delete them, or delete %s to keep them.",
					plan.To.Backend, plan.From.where(cfg.DataDir), plan.To.Backend, filepath.Join(cfg.DataDir, moveFile)))
			}
			removeMovePlan(cfg.DataDir)
			plan, done = nil, nil
		case plan.From != from:
			return res, plan.byHand(cfg.DataDir, "the database no longer records the storage it moved from")
		case sameTarget(plan.To, cfg):
			// Resumed below.
		default:
			n, err := cleanMoveLeftovers(ctx, cfg, plan, w, r)
			if err != nil {
				return res, err
			}
			fmt.Fprintf(warn, "Removed %s an interrupted move left in %s.\n", count(n, "file"), plan.To.where(cfg.DataDir))
			removeMovePlan(cfg.DataDir)
			plan, done = nil, nil
		}
	}

	if from.Backend == to.Backend && (from.Location == to.Location || blobs.SameLocation(from.Location, cfg)) {
		return res, message("Attachments are already stored there.")
	}
	if len(items) == 0 {
		return res, nil
	}

	src, err := openPlace(cfg, from, w, r, get)
	if err != nil {
		return res, err
	}
	dst, err := openBlobStore(cfg, w, r, time.Now)
	if err != nil {
		return res, fmt.Errorf("attachment store: %w", err)
	}

	// What an interrupted run copied and checked, and is still there.
	var todo []moveItem
	var need int64
	for _, a := range items {
		if size, ok := done[a.key]; ok && size == a.size {
			if got, err := dst.Stat(ctx, a.key); err == nil && got == a.size {
				res.Earlier++
				continue
			}
		}
		todo = append(todo, a)
		need += a.size
	}
	if err := checkMoveRoom(cfg, need); err != nil {
		return res, err
	}

	// Before the first put: the plan, with what is already done, so a kill
	// from here on leaves a record to resume or clean up from.
	plan = &movePlan{Instance: instance, From: from, To: to, DeleteOld: deleteOld}
	log, err := writeMovePlan(cfg.DataDir, *plan, items, todo)
	if err != nil {
		return res, err
	}
	for _, a := range todo {
		if err := copyChecked(ctx, src, dst, a, from, to); err != nil {
			log.Close()
			return res, notMoved{err, from}
		}
		if err := log.done(a); err != nil {
			log.Close()
			return res, notMoved{err, from}
		}
		res.Copied++
	}
	if err := log.Close(); err != nil {
		return res, notMoved{err, from}
	}
	// Every copy, this run's and earlier ones', is there at its size.
	for _, a := range items {
		got, err := dst.Stat(ctx, a.key)
		if err != nil || got != a.size {
			return res, notMoved{fmt.Errorf("the copy of attachment %s on %s isn't in %s at its size", a.id, a.ticket, to.Backend), from}
		}
	}
	if err := switchStorage(ctx, w, to); err != nil {
		return res, notMoved{err, from}
	}
	res.Moved = len(items)
	if deleteOld {
		if err := deleteOldCopies(ctx, cfg, from, instance, items, w, r, get); err != nil {
			//lint:ignore ST1005 the report is a sentence the operator reads as is
			return res, fmt.Errorf("Moved %s to %s, but %w", count(len(items), "attachment"), to.Backend, err)
		}
		res.DeletedOld = true
	}
	removeMovePlan(cfg.DataDir)
	return res, nil
}

// checkCurrentSchema refuses a database an older Vaquero wrote: serve
// upgrades it, with its snapshot, first.
func checkCurrentSchema(r *sql.DB) error {
	have, err := appdb.Version(r)
	if err != nil {
		return err
	}
	want, err := appdb.Newest()
	if err != nil {
		return err
	}
	if have < want {
		return message("The database is from an older Vaquero. Start vaquero once so it upgrades the database, stop it, then run the move again.")
	}
	return nil
}

// moveState reads the instance id, the storage the database records and
// every attachment.
func moveState(ctx context.Context, r *sql.DB, q *gen.Queries) (instance string, from movePlace, items []moveItem, err error) {
	setting := func(key string) (string, error) {
		s, err := q.GetSetting(ctx, key)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return s.Value, err
	}
	if instance, err = setting("instance_id"); err != nil {
		return
	}
	if from.Backend, err = setting(settingBlobStore); err != nil {
		return
	}
	if from.Location, err = setting(settingBlobLocation); err != nil {
		return
	}
	rows, err := r.QueryContext(ctx, `SELECT a.id, a.blob_key, a.content_type, a.size, p.key || '-' || t.number
		FROM attachments a
		JOIN tickets t ON t.id = a.ticket_id
		JOIN projects p ON p.id = t.project_id
		ORDER BY a.id`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a moveItem
		if err = rows.Scan(&a.id, &a.key, &a.ctype, &a.size, &a.ticket); err != nil {
			return
		}
		items = append(items, a)
	}
	if err = rows.Err(); err != nil {
		return
	}
	if len(items) > 0 && (from.Backend == "" || instance == "") {
		err = message("The database has attachments but doesn't record where they are. Start vaquero once, stop it, then run the move again.")
	}
	if from.Backend == "" {
		from.Backend = "sqlite" // nothing stored yet: serve's default
	}
	return
}

// sameTarget reports whether p is the place cfg names.
func sameTarget(p movePlace, cfg config.Config) bool {
	return p.Backend == cfg.BlobBackend && blobs.SameLocation(p.Location, cfg)
}

// openPlace opens the storage at p: the database's own pools for sqlite,
// the recorded directory, or the recorded bucket with the VQ_S3_ keys from
// the environment.
func openPlace(cfg config.Config, p movePlace, w, r *sql.DB, get func(string) string) (blobs.Store, error) {
	pc := config.Config{Mode: cfg.Mode, DataDir: cfg.DataDir, DBPath: cfg.DBPath, BlobBackend: p.Backend}
	switch p.Backend {
	case "dir":
		if p.Location == "" {
			return nil, message("The database doesn't record which directory holds the attachments. Start vaquero once, stop it, then run the move again.")
		}
		pc.BlobDir = blobs.ResolveLocation(p.Location, cfg.DataDir)
	case "s3":
		ep, bucket, ok := strings.Cut(p.Location, "/")
		if !ok || ep == "" || bucket == "" {
			return nil, message("The database doesn't record which bucket holds the attachments. Start vaquero once, stop it, then run the move again.")
		}
		pc.S3Endpoint, pc.S3Bucket = ep, bucket
		pc.S3AccessKeyID = strings.TrimSpace(get("VQ_S3_ACCESS_KEY_ID"))
		pc.S3SecretAccessKey = config.Secret(strings.TrimSpace(get("VQ_S3_SECRET_ACCESS_KEY")))
		pc.S3Region = strings.TrimSpace(get("VQ_S3_REGION"))
		if pc.S3Region == "" {
			pc.S3Region = "auto"
		}
		var missing []string
		if pc.S3AccessKeyID == "" {
			missing = append(missing, "VQ_S3_ACCESS_KEY_ID")
		}
		if len(pc.S3SecretAccessKey) == 0 {
			missing = append(missing, "VQ_S3_SECRET_ACCESS_KEY")
		}
		if len(missing) > 0 {
			return nil, message(fmt.Sprintf("Attachments are stored in s3 %s; reading them needs %s.", p.Location, strings.Join(missing, " and ")))
		}
	}
	s, err := openBlobStore(pc, w, r, time.Now)
	if err != nil {
		return nil, fmt.Errorf("the storage at %s: %w", p.where(cfg.DataDir), err)
	}
	return s, nil
}

// checkMoveRoom refuses a move whose copies won't fit: into a directory,
// on its volume; into the database, on the data directory's. A bucket's
// room is the provider's to know.
func checkMoveRoom(cfg config.Config, need int64) error {
	var dir string
	switch cfg.BlobBackend {
	case "dir":
		dir = cfg.BlobDir
	case "sqlite":
		dir = cfg.DataDir
	default:
		return nil
	}
	free, err := moveFreeBytes(dir)
	if err != nil {
		return fmt.Errorf("free space in %s: %w", dir, err)
	}
	if free < need {
		return message(fmt.Sprintf("Moving the attachments needs %s free in %s, and %s is free.", units.SizeWords(need), dir, units.SizeWords(free)))
	}
	return nil
}

// copyChecked copies a's bytes from src to dst under the same key, then
// reads the copy back and compares its size and SHA-256 with what was
// read from src.
func copyChecked(ctx context.Context, src, dst blobs.Store, a moveItem, from, to movePlace) error {
	rc, size, err := src.Get(ctx, a.key)
	if errors.Is(err, blobs.ErrNotFound) {
		return fmt.Errorf("the file for attachment %s on %s is missing from %s", a.id, a.ticket, from.Backend)
	}
	if err != nil {
		return fmt.Errorf("read attachment %s on %s: %w", a.id, a.ticket, err)
	}
	defer rc.Close()
	if size != a.size {
		return fmt.Errorf("the file for attachment %s on %s is %d bytes in %s, but its record says %d", a.id, a.ticket, size, from.Backend, a.size)
	}
	orig := &hashing{r: rc, h: sha256.New()}
	pctx, cancel := context.WithTimeout(ctx, blobs.PutTimeout(size))
	err = dst.Put(pctx, a.key, orig, size, a.ctype)
	cancel()
	if err != nil {
		return fmt.Errorf("copy attachment %s on %s to %s: %w", a.id, a.ticket, to.Backend, err)
	}
	if orig.n != size {
		return fmt.Errorf("read %d of %d bytes of attachment %s on %s", orig.n, size, a.id, a.ticket)
	}
	crc, csize, err := dst.Get(ctx, a.key)
	if err != nil {
		return fmt.Errorf("read back attachment %s on %s from %s: %w", a.id, a.ticket, to.Backend, err)
	}
	defer crc.Close()
	copied := &hashing{r: crc, h: sha256.New()}
	if _, err := io.Copy(io.Discard, copied); err != nil {
		return fmt.Errorf("read back attachment %s on %s from %s: %w", a.id, a.ticket, to.Backend, err)
	}
	if csize != size || copied.n != size || !bytes.Equal(copied.h.Sum(nil), orig.h.Sum(nil)) {
		return fmt.Errorf("the copy of attachment %s on %s in %s doesn't match the original", a.id, a.ticket, to.Backend)
	}
	return nil
}

// hashing hashes and counts what is read through it.
type hashing struct {
	r io.Reader
	h hash.Hash
	n int64
}

func (x *hashing) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	x.h.Write(p[:n])
	x.n += int64(n)
	return n, err
}

// switchStorage records to as the attachments' storage, both settings in
// one transaction, as serve and import write them.
func switchStorage(ctx context.Context, w *sql.DB, to movePlace) error {
	tx, err := w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := gen.New(tx)
	now := timefmt.Format(time.Now())
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "blob_store", Value: to.Backend, UpdatedAt: now}); err != nil {
		return err
	}
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: "blob_location", Value: to.Location, UpdatedAt: now}); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteOldCopies deletes, from the storage at from, every attachment's
// bytes and everything under this instance's attachments/ and thumbs/
// prefixes (previews, and files no record names). It runs only after the
// switch, so nothing reads from there any more.
func deleteOldCopies(ctx context.Context, cfg config.Config, from movePlace, instance string, items []moveItem, w, r *sql.DB, get func(string) string) error {
	old, err := openPlace(cfg, from, w, r, get)
	if err != nil {
		return oldNotDeleted(cfg, from, err)
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancel()
	keys := make([]string, 0, len(items))
	for _, a := range items {
		keys = append(keys, a.key)
	}
	for _, prefix := range []string{"attachments/" + instance + "/", "thumbs/" + instance + "/"} {
		if err := old.List(dctx, prefix, func(key string, _ time.Time) error {
			keys = append(keys, key)
			return nil
		}); err != nil {
			return oldNotDeleted(cfg, from, err)
		}
	}
	for _, k := range keys {
		if err := old.Delete(dctx, k); err != nil && !errors.Is(err, blobs.ErrNotFound) {
			return oldNotDeleted(cfg, from, err)
		}
	}
	if from.Backend == "dir" {
		root := blobs.ResolveLocation(from.Location, cfg.DataDir)
		for _, d := range []string{"attachments", "thumbs"} {
			_ = os.Remove(filepath.Join(root, d, instance)) // only when empty
			_ = os.Remove(filepath.Join(root, d))
		}
	}
	return nil
}

func oldNotDeleted(cfg config.Config, from movePlace, err error) error {
	return fmt.Errorf("the old copies in %s couldn't all be deleted (%w). Run the same command again to retry, or delete %s to keep them", from.where(cfg.DataDir), err, filepath.Join(cfg.DataDir, moveFile))
}

// cleanMoveLeftovers deletes what an interrupted move to plan.To put
// there: everything under this instance's attachments/ prefix, which the
// database never pointed at. A bucket is left for the operator: its keys
// may not be the ones in the environment now.
func cleanMoveLeftovers(ctx context.Context, cfg config.Config, plan *movePlan, w, r *sql.DB) (int, error) {
	if plan.To.Backend == "s3" {
		return 0, plan.byHand(cfg.DataDir, "it moved to another bucket")
	}
	store, err := openPlace(cfg, plan.To, w, r, func(string) string { return "" })
	if err != nil {
		return 0, plan.byHand(cfg.DataDir, err.Error())
	}
	prefix := "attachments/" + plan.Instance + "/"
	var keys []string
	if err := store.List(ctx, prefix, func(key string, _ time.Time) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		return 0, plan.byHand(cfg.DataDir, err.Error())
	}
	for i, k := range keys {
		if err := store.Delete(ctx, k); err != nil {
			return i, plan.byHand(cfg.DataDir, err.Error())
		}
	}
	if plan.To.Backend == "dir" {
		_ = os.Remove(filepath.Join(blobs.ResolveLocation(plan.To.Location, cfg.DataDir), "attachments", plan.Instance))
	}
	return len(keys), nil
}

// byHand is the refusal for a record this run can't act on.
func (p *movePlan) byHand(dataDir, why string) error {
	return message(fmt.Sprintf("An interrupted move to %s left copies under attachments/%s/ there (%s). Remove them by hand, or run the move to it again, then delete %s.",
		p.To.where(dataDir), p.Instance, why, filepath.Join(dataDir, moveFile)))
}

// readMovePlan reads moveFile: the plan and each copy already checked, by
// key and size. A last line cut short by a kill is ignored.
func readMovePlan(dataDir string) (*movePlan, map[string]int64, error) {
	path := filepath.Join(dataDir, moveFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	var plan movePlan
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &plan) != nil || plan.Instance == "" {
		return nil, nil, message(path + ", left by an interrupted move, can't be read. Check the storage it was moving to for files under attachments/, then delete it.")
	}
	done := map[string]int64{}
	for sc.Scan() {
		var d moveDone
		if json.Unmarshal(sc.Bytes(), &d) == nil && d.Key != "" {
			done[d.Key] = d.Size
		}
	}
	return &plan, done, nil
}

// moveLog appends checked copies to moveFile, each synced before the next
// copy starts.
type moveLog struct{ f *os.File }

func (l *moveLog) done(a moveItem) error {
	b, _ := json.Marshal(moveDone{Key: a.key, Size: a.size})
	if _, err := l.f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("record the move: %w", err)
	}
	if err := l.f.Sync(); err != nil {
		return fmt.Errorf("record the move: %w", err)
	}
	return nil
}

func (l *moveLog) Close() error { return l.f.Close() }

// writeMovePlan writes moveFile afresh (the plan, then the copies an
// earlier run made that this one keeps: every item not in todo) through a
// temporary file, synced and renamed, and opens it to append to.
func writeMovePlan(dataDir string, plan movePlan, items, todo []moveItem) (*moveLog, error) {
	path := filepath.Join(dataDir, moveFile)
	left := map[string]bool{}
	for _, a := range todo {
		left[a.key] = true
	}
	var buf bytes.Buffer
	b, _ := json.Marshal(plan)
	buf.Write(append(b, '\n'))
	for _, a := range items {
		if !left[a.key] {
			b, _ := json.Marshal(moveDone{Key: a.key, Size: a.size})
			buf.Write(append(b, '\n'))
		}
	}
	tmp := path + ".tmp"
	if err := copyFile(tmp, &buf); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("record the move in %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("record the move in %s: %w", path, err)
	}
	if err := syncDir(dataDir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("record the move in %s: %w", path, err)
	}
	return &moveLog{f: f}, nil
}

// removeMovePlan removes moveFile.
func removeMovePlan(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, moveFile))
}
