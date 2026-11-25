package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
)

// serveOnce boots serve, waits until it is healthy, stops it and returns
// the log it wrote.
func serveOnce(t *testing.T, cfg config.Config) string {
	t.Helper()
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.NewTextHandler(&logs, nil))) }()
	waitHealthy(t, cfg.Addr)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return logs.String()
}

// VAQ-9: an existing data directory keeps its mode, with a warning.
func TestServeKeepsExistingDataDirMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logs := serveOnce(t, testConfig(t, freeAddr(t), dir))
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode changed: %v %v", fi, err)
	}
	if !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "chmod 700 "+dir) || !strings.Contains(logs, "755") {
		t.Fatalf("no warning naming the mode and the fix:\n%s", logs)
	}
}

func TestServeDoesNotWarnAboutPrivateDataDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if logs := serveOnce(t, testConfig(t, freeAddr(t), dir)); strings.Contains(logs, "chmod") {
		t.Fatalf("warned about a private directory:\n%s", logs)
	}
}

// VAQ-11: only an address already in use suggests a running vaquero.
func TestServeListenFailureNotBlamedOnRunningVaquero(t *testing.T) {
	cfg := testConfig(t, freeAddr(t), filepath.Join(t.TempDir(), "vaquero"))
	cfg.Addr = "127.0.0.1:99999"
	err := serve(context.Background(), cfg, slog.New(slog.DiscardHandler))
	if err == nil || strings.Contains(err.Error(), "already running") {
		t.Fatalf("%v", err)
	}
}

// VAQ-12: whatever was bound, local mode refuses a socket that isn't loopback.
func TestServeRefusesNonLoopbackSocket(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vaquero")
	cfg := testConfig(t, freeAddr(t), dir)
	cfg.Addr = "0.0.0.0:0"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := serve(ctx, cfg, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a refused boot touched the data directory: %v", err)
	}
}

// VAQ-13: the first signal starts shutdown and releases the handler, so a
// second one takes the default action at once.
func TestShutdownContextReleasesSignalHandler(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	var stops atomic.Int32
	old := notifyContext
	notifyContext = func(context.Context, ...os.Signal) (context.Context, context.CancelFunc) {
		return parent, func() { stops.Add(1) }
	}
	defer func() { notifyContext = old }()
	ctx, stop := shutdownContext()
	defer stop()
	cancel()
	<-ctx.Done()
	deadline := time.Now().Add(2 * time.Second)
	for stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if stops.Load() == 0 {
		t.Fatal("the signal handler was not released when shutdown began")
	}
}

// VAQ-14: the refusal reaches the operator through the real command.
func TestRunServeRefusesNonLoopbackAddr(t *testing.T) {
	t.Setenv("VQ_ADDR", "0.0.0.0:7878")
	t.Setenv("VQ_DATA_DIR", filepath.Join(t.TempDir(), "vaquero"))
	var out, errb bytes.Buffer
	if code := run([]string{"serve"}, &out, &errb); code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"config:", "VQ_ADDR", "loopback", "hosted mode"} {
		if !strings.Contains(errb.String(), want) {
			t.Fatalf("stderr lacks %q: %q", want, errb.String())
		}
	}
	if _, err := os.Stat(os.Getenv("VQ_DATA_DIR")); !os.IsNotExist(err) {
		t.Fatalf("a refused config touched the data directory: %v", err)
	}
}
