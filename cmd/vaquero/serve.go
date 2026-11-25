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
	"time"

	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/blobs"
	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/lockfile"
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
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w (is vaquero already running?)", cfg.Addr, err)
	}
	defer ln.Close() // a no-op once the server owns it; frees it on an early return
	// The data directory will hold the API token, so only its owner may read it.
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	if err := os.Chmod(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("data directory: %w", err)
	}
	// One server per data directory, whatever port it listens on. Released
	// last, after the WAL checkpoint (defers run in reverse).
	release, held, err := lockfile.Acquire(cfg.DataDir)
	if errors.Is(err, lockfile.ErrLocked) {
		return fmt.Errorf("another vaquero is using %s (pid %d)", cfg.DataDir, held)
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
	// The instance id names blob keys; the backend lock keeps existing
	// attachments from being stranded by a changed VQ_BLOB_STORE. Both come
	// before anything else is written, so a refused boot changes nothing.
	if _, err := ensureInstance(ctx, w, time.Now()); err != nil {
		return fmt.Errorf("instance id: %w", err)
	}
	if err := checkBlobBackend(ctx, gen.New(r), cfg.BlobBackend); err != nil {
		return err
	}
	if err := gen.New(w).PutSetting(ctx, gen.PutSettingParams{Key: settingBlobStore, Value: cfg.BlobBackend, UpdatedAt: timefmt.Format(time.Now())}); err != nil {
		return fmt.Errorf("blob store setting: %w", err)
	}
	// Opening checks the configuration now, so a bad directory or S3 setting
	// stops the boot instead of the first upload.
	store, err := blobs.Open(cfg, w, r, time.Now)
	if err != nil {
		return fmt.Errorf("attachment store: %w", err)
	}
	_ = store // Task 4 hands it to the handlers
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		return fmt.Errorf("owner: %w", err)
	}
	if err := ensureLocalToken(ctx, c, gen.New(r), owner, cfg.DataDir); err != nil {
		return fmt.Errorf("local token: %w", err)
	}
	shutdown := make(chan struct{})
	d := handlers.Deps{Cfg: cfg, DB: w, R: gen.New(r), RDB: r, Core: c, Actor: owner, Logger: logger, Now: time.Now, Shutdown: shutdown, Touch: gen.New(w), MCPCalls: gen.New(w),
		BackupDir:       filepath.Join(cfg.DataDir, "backups"),
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
	// The sweep ends lapsed claims and prunes the MCP log. It stops before
	// the drain, so nothing writes while the server winds down.
	sweeper := &sweep.Sweeper{Core: c, DB: w, Logger: logger, Now: time.Now, Interval: sweepInterval, OnTick: sweepOnTick}
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

// Settings keys written by serve.
const (
	settingInstanceID = "instance_id"
	settingBlobStore  = "blob_store"
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

// checkBlobBackend refuses a boot whose configured backend differs from the
// one recorded in settings while attachments exist, since their bytes would
// be unreachable.
func checkBlobBackend(ctx context.Context, r *gen.Queries, configured string) error {
	s, err := r.GetSetting(ctx, settingBlobStore)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && s.Value == configured) {
		return nil
	}
	if err != nil {
		return err
	}
	n, err := r.CountAttachments(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	//lint:ignore ST1005 the refusal is a sentence the operator reads as is
	return fmt.Errorf("Attachments are stored in %s; VQ_BLOB_STORE is %s. Set it back to %s.", s.Value, configured, s.Value)
}

// TokenFile is where the local token lives in the data directory. The CLI
// and the MCP bridge read it to talk to the local server.
const TokenFile = "token"

// ensureLocalToken makes sure {dataDir}/token holds a live token for the
// owner: one is minted ("local") and written with mode 0600 when the file
// is missing, malformed, or names a token that is unknown or revoked.
func ensureLocalToken(ctx context.Context, c *core.Core, r *gen.Queries, owner gen.Actor, dataDir string) error {
	path := filepath.Join(dataDir, TokenFile)
	if b, err := os.ReadFile(path); err == nil {
		raw := strings.TrimSpace(string(b))
		if apitoken.Valid(raw) {
			if t, err := r.GetTokenByHash(ctx, apitoken.Hash(raw)); err == nil && !t.RevokedAt.Valid {
				return os.Chmod(path, 0o600)
			}
		}
	}
	raw, _, err := c.CreateToken(ctx, owner.ID, owner.ID, "local")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(raw+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
