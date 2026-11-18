package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web"
	"github.com/stazelabs/vaquero/internal/web/handlers"
)

// drainTimeout is how long shutdown waits for in-flight requests.
const drainTimeout = 30 * time.Second

// serve runs the server until ctx is done, then drains, checkpoints the WAL
// and returns. It binds the address before anything else, so a second
// instance fails fast without opening the first one's database.
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
	w, r, err := openDB(cfg.DBPath)
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
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		return fmt.Errorf("owner: %w", err)
	}
	shutdown := make(chan struct{})
	d := handlers.Deps{Cfg: cfg, DB: w, R: gen.New(r), Core: c, Actor: owner, Logger: logger, Now: time.Now, Shutdown: shutdown}
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
	logger.Info("vaquero is running", "url", "http://"+ln.Addr().String(), "data", cfg.DataDir, "version", version.Version)

	select {
	case err := <-served:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	logger.Info("shutting down")
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
