package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
)

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func testConfig(t *testing.T, addr, dir string) config.Config {
	cfg, err := config.FromEnv(func(k string) string { return map[string]string{"VQ_ADDR": addr, "VQ_DATA_DIR": dir}[k] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestServeStartsAndStopsCleanly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "vaquero") // does not exist yet
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, dir), slog.New(slog.DiscardHandler)) }()
	var resp *http.Response
	var err error
	for range 50 {
		if resp, err = http.Get("http://" + addr + "/healthz"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("%q", b)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("data dir: %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "vaquero.db-wal")); err == nil && fi.Size() != 0 {
		t.Fatal("WAL not checkpointed")
	}
}

// Review Focus 4
func TestServeReportsAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	dir := filepath.Join(t.TempDir(), "vaquero")
	err = serve(context.Background(), testConfig(t, addr, dir), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), addr) || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a failed second instance touched the data directory: %v", err)
	}
}

// Review Focus 4: an open live-update stream doesn't hold up shutdown.
func TestServeStopsPromptlyWithOpenStream(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(t, addr, t.TempDir()), slog.New(slog.DiscardHandler)) }()
	var res *http.Response
	var err error
	for range 50 {
		if res, err = http.Get("http://" + addr + "/events/stream"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve held open by the stream")
	}
}
