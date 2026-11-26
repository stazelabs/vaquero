package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/sweep"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/handlers"
)

// drainTimeout is how long shutdown waits for in-flight requests.
const drainTimeout = 30 * time.Second

// sweepInterval and sweepOnTick let tests run the sweep fast and watch it.
var (
	sweepInterval = sweep.Every
	sweepOnTick   func()
)

// serve runs the server until ctx is done, then drains, checkpoints the WAL
// and returns. It binds the address before anything else, so a second
// instance fails fast without opening the first one's database.
// bootNow is the clock the boot's snapshot is named by; tests fix it.
var bootNow = time.Now

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) (err error) {
	ln, err := net.Listen("tcp", cfg.Addr)
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("cannot listen on %s: %w (is vaquero already running?)", cfg.Addr, err)
	}
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", cfg.Addr, err)
	}
	defer ln.Close() // a no-op once the server owns it; frees it on an early return
	// Local mode has no sign-in: whatever the address said, the socket must
	// be a loopback one the Host check answers to.
	if cfg.Mode == config.ModeLocal {
		if ta, ok := ln.Addr().(*net.TCPAddr); !ok || !config.AllowedHostIP(ta.IP) {
			return fmt.Errorf("refusing to serve on %s: local mode listens on a loopback address only", ln.Addr())
		}
	}
	// The data directory will hold the API token, so only its owner should
	// read it. A new directory is made 0700; an existing one is left as it
	// is (it may be the working directory or shared with other tools), with
	// a warning when it is open to others.
	if err := prepareDataDir(cfg.DataDir, logger); err != nil {
		return err
	}
	logSettings(cfg, logger)
	// One server per data directory, whatever port it listens on. Released
	// last, after the WAL checkpoint (defers run in reverse).
	release, held, err := lockfile.Acquire(cfg.DataDir)
	if errors.Is(err, lockfile.ErrLocked) {
		return inUse(cfg.DataDir, held)
	}
	if err != nil {
		return fmt.Errorf("data directory lock: %w", err)
	}
	defer release()
	w, r, err := openDB(ctx, cfg.DBPath, filepath.Join(cfg.DataDir, "backups"), bootNow(), logger)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer func() {
		if cerr := appdb.Checkpoint(w); cerr != nil {
			logger.Warn("checkpoint", "err", cerr)
		}
		_ = r.Close()
		_ = w.Close()
	}()

	c := core.New(w, time.Now)
	c.BlobStore = cfg.BlobBackend
	c.BlobLocation = blobs.Location(cfg)
	c.Git = gitread.New()
	// The instance id names blob keys; the backend lock keeps existing
	// attachments from being stranded by a changed VQ_BLOB_STORE. Both come
	// before anything else is written, so a refused boot changes nothing.
	instance, err := ensureInstance(ctx, w, time.Now())
	if err != nil {
		return fmt.Errorf("instance id: %w", err)
	}
	if err := checkBlobBackend(ctx, gen.New(r), cfg); err != nil {
		return err
	}
	// Both are recorded on every boot that passes the check: unchanged with
	// attachments, free to change without them, and a location missing
	// from an older database is recorded here rather than refused.
	if err := gen.New(w).PutSetting(ctx, gen.PutSettingParams{Key: settingBlobStore, Value: cfg.BlobBackend, UpdatedAt: timefmt.Format(time.Now())}); err != nil {
		return fmt.Errorf("blob store setting: %w", err)
	}
	if err := gen.New(w).PutSetting(ctx, gen.PutSettingParams{Key: settingBlobLocation, Value: c.BlobLocation, UpdatedAt: timefmt.Format(time.Now())}); err != nil {
		return fmt.Errorf("blob location setting: %w", err)
	}
	// Opening checks the configuration now, so a bad directory or S3 setting
	// stops the boot instead of the first upload.
	store, err := blobs.Open(cfg, w, r, time.Now)
	if err != nil {
		return fmt.Errorf("attachment store: %w", err)
	}
	// What an import killed before it finished left in the store.
	if n, err := cleanPending(ctx, cfg, store, instance); err != nil {
		logger.Warn("interrupted import cleanup", "err", err)
	} else if n > 0 {
		logger.Info("removed files an interrupted import left", "count", n, "backend", cfg.BlobBackend)
	}
	// A move stopped part way: the storage above is still the one in use,
	// and the copies it made wait for the move to be run again.
	if _, err := os.Stat(filepath.Join(cfg.DataDir, moveFile)); err == nil {
		logger.Warn("an interrupted attachments move left copies in its target; run vaquero attachments move again to finish it", "record", filepath.Join(cfg.DataDir, moveFile))
	}
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		return fmt.Errorf("owner: %w", err)
	}
	if err := ensureLocalToken(ctx, c, gen.New(r), owner, cfg.DataDir); err != nil {
		return fmt.Errorf("local token: %w", err)
	}
	shutdown := make(chan struct{})
	d := handlers.Deps{Cfg: cfg, DB: w, R: gen.New(r), RDB: r, Core: c, Actor: owner, Logger: logger, Now: time.Now, Shutdown: shutdown, Touch: gen.New(w), MCPCalls: gen.New(w),
		Blobs: store, Instance: instance, Git: c.Git, Files: readiness.NewFiles(c.Git),
		BackupDir:       filepath.Join(cfg.DataDir, "backups"),
		Started:         time.Now(),
		BrewService:     underBrewService(cfg.DataDir),
		RenewLocalToken: func(ctx context.Context) error { return ensureLocalToken(ctx, c, gen.New(r), owner, cfg.DataDir) }}
	srv := &http.Server{
		Handler:           web.NewRouter(d),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	// Open live-update streams end as soon as shutdown starts, so the drain
	// doesn't wait on them.
	srv.RegisterOnShutdown(func() { close(shutdown) })
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	// The sweep ends lapsed claims, prunes the MCP log and deletes orphaned
	// attachment bytes. It stops before the drain, so nothing writes while
	// the server winds down.
	sweeper := &sweep.Sweeper{Core: c, DB: w, Logger: logger, Now: time.Now, Blobs: store, Instance: instance,
		Interval: sweepInterval, OnTick: sweepOnTick}
	sweeper.Start()
	defer sweeper.Stop()
	logger.Info("vaquero is running", "url", "http://"+ln.Addr().String(), "data", cfg.DataDir, "attachments", cfg.BlobBackend, "version", version.Version)

	select {
	case err := <-served:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	sweeper.Stop()
	sctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

// prepareDataDir creates dir 0700 when it is missing. An existing directory
// is never changed; one that grants group or other any access is warned
// about, since the API token lives in it.
func prepareDataDir(dir string, logger *slog.Logger) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("data directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("data directory: %s is not a directory", dir)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		logger.Warn("the data directory is open to other users, and it holds the API token; run chmod 700 "+dir, "path", dir, "mode", fmt.Sprintf("%04o", perm))
	}
	return nil
}

// inUse is the refusal of a second server on dir, whose lock pid holds (0:
// a pid that couldn't be read, left out).
func inUse(dir string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("another vaquero is using %s", dir)
	}
	return fmt.Errorf("another vaquero is using %s (pid %d)", dir, pid)
}

// Settings keys written by serve.
const (
	settingInstanceID   = "instance_id"
	settingBlobStore    = "blob_store"
	settingBlobLocation = "blob_location"
)

// ensureInstance returns the instance id, minting a lower-case ULID on the
// first boot. It names blob keys, so it never changes once set.
func ensureInstance(ctx context.Context, w *sql.DB, now time.Time) (string, error) {
	q := gen.New(w)
	s, err := q.GetSetting(ctx, settingInstanceID)
	if err == nil {
		return s.Value, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := strings.ToLower(idgen.New())
	if err := q.PutSetting(ctx, gen.PutSettingParams{Key: settingInstanceID, Value: id, UpdatedAt: timefmt.Format(now)}); err != nil {
		return "", err
	}
	return id, nil
}

// checkBlobBackend refuses a boot whose configured backend, or the place
// it keeps the bytes (blobs.Location, compared by blobs.SameLocation),
// differs from the one recorded in settings while attachments exist, since
// their bytes would be unreachable. A location not recorded yet (a
// database from before it was) is no refusal.
func checkBlobBackend(ctx context.Context, r *gen.Queries, cfg config.Config) error {
	store, err := r.GetSetting(ctx, settingBlobStore)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	loc, err := r.GetSetting(ctx, settingBlobLocation)
	locChanged := err == nil && store.Value == cfg.BlobBackend && !blobs.SameLocation(loc.Value, cfg)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if store.Value == cfg.BlobBackend && !locChanged {
		return nil
	}
	n, err := r.CountAttachments(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if locChanged {
		return blobLocationRefusal(cfg, loc.Value)
	}
	//lint:ignore ST1005 the refusal is a sentence the operator reads as is
	return fmt.Errorf("Attachments are stored in %s; VQ_BLOB_STORE is %s%s. Set it back to %s.", store.Value, cfg.BlobBackend, fromFile(cfg, "VQ_BLOB_STORE"), store.Value)
}

// fromFile is " (from vaquero.env)" when any of keys came from the file,
// so a refusal says where to set the value back.
func fromFile(cfg config.Config, keys ...string) string {
	for _, k := range keys {
		if cfg.Source(k) == config.SourceFile {
			return " (from vaquero.env)"
		}
	}
	return ""
}

// blobSettings are the storage settings whose sources the boot log names.
var blobSettings = []string{"VQ_BLOB_STORE", "VQ_BLOB_DIR", "VQ_S3_ENDPOINT", "VQ_S3_BUCKET", "VQ_S3_ACCESS_KEY_ID", "VQ_S3_SECRET_ACCESS_KEY", "VQ_S3_REGION"}

// logSettings logs where each storage setting came from (environment,
// file or default), never a value, and vaquero.env's unknown keys.
func logSettings(cfg config.Config, logger *slog.Logger) {
	args := []any{"file", config.EnvFilePath(cfg.DataDir)}
	for _, k := range blobSettings {
		args = append(args, k, cfg.Source(k))
	}
	logger.Info("storage settings", args...)
	for _, w := range cfg.Warnings {
		logger.Warn(w)
	}
}

// blobLocationRefusal says the attachments are in recorded, not where the
// configuration points, and which settings to put back.
func blobLocationRefusal(cfg config.Config, recorded string) error {
	now := blobs.Location(cfg)
	if cfg.BlobBackend == "s3" {
		ep, bucket, _ := strings.Cut(recorded, "/")
		//lint:ignore ST1005 the refusal is a sentence the operator reads as is
		return fmt.Errorf("Attachments are stored in %s; VQ_S3_ENDPOINT and VQ_S3_BUCKET name %s%s. Set them back to %s and %s.", recorded, now, fromFile(cfg, "VQ_S3_ENDPOINT", "VQ_S3_BUCKET"), ep, bucket)
	}
	was := blobs.ResolveLocation(recorded, cfg.DataDir)
	//lint:ignore ST1005 the refusal is a sentence the operator reads as is
	return fmt.Errorf("Attachments are stored in %s; VQ_BLOB_DIR is %s%s. Set it back to %s.", was, blobs.ResolveLocation(now, cfg.DataDir), fromFile(cfg, "VQ_BLOB_DIR"), was)
}

// TokenFile is where the local token lives in the data directory. The CLI
// and the MCP bridge read it to talk to the local server.
const TokenFile = "token"

// ensureLocalToken makes sure {dataDir}/token holds a live token for the
// owner: one is minted ("local") and written with mode 0600 when the file
// is missing, malformed, or names a token that is unknown, revoked or
// another actor's. Minting one revokes the owner's other live "local"
// tokens, whose secret the replaced file may have held.
func ensureLocalToken(ctx context.Context, c *core.Core, r *gen.Queries, owner gen.Actor, dataDir string) error {
	path := filepath.Join(dataDir, TokenFile)
	if b, err := os.ReadFile(path); err == nil {
		raw := strings.TrimSpace(string(b))
		if apitoken.Valid(raw) {
			if t, err := r.GetTokenByHash(ctx, apitoken.Hash(raw)); err == nil && !t.RevokedAt.Valid && t.ActorID == owner.ID {
				return os.Chmod(path, 0o600)
			}
		}
	}
	raw, made, err := c.CreateToken(ctx, owner.ID, owner.ID, "local")
	if err != nil {
		return err
	}
	if err := writeSecret(path, []byte(raw+"\n")); err != nil {
		return err
	}
	toks, err := r.ListTokens(ctx)
	if err != nil {
		return err
	}
	for _, t := range toks {
		if t.Name == "local" && t.ActorID == owner.ID && t.ID != made.ID && !t.RevokedAt.Valid {
			if err := c.RevokeToken(ctx, owner.ID, t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeSecret writes b to path (0600) through path.tmp and a rename. A
// .tmp left by an earlier run is removed first: writing over it would keep
// its mode.
func writeSecret(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// underBrewService reports whether brew services runs the server on
// dataDir (brewServiceState says it is running), for the admin page. The
// service manager is asked once, on first use, never at boot.
func underBrewService(dataDir string) func() bool {
	return sync.OnceValue(func() bool {
		_, state := brewServiceState(dataDir)
		return state == brewRunning
	})
}
