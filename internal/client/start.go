//go:build unix

package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type probeResult int

const (
	probeUp      probeResult = iota // vaquero answered /healthz
	probeRefused                    // nothing is listening
	probeOther                      // something answered, or hung, that isn't vaquero
)

// probe asks url's /healthz, briefly.
func probe(ctx context.Context, url string) probeResult {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(url, "/")+"/healthz", nil)
	if err != nil {
		return probeOther
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return probeRefused
		}
		return probeOther
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 16))
	if res.StatusCode == http.StatusOK && strings.TrimSpace(string(b)) == "ok" {
		return probeUp
	}
	return probeOther
}

// EnsureLocal makes sure the local server at url answers. When nothing is
// listening it starts `exe serve` in the background, in its own session
// (it outlives the CLI), with its output in {dataDir}/vaquero.log, and
// waits up to wait for it. It never hangs: something else on the address,
// a server that stops while starting, or one that doesn't come up in time
// are each a plain error naming the address or the log.
func EnsureLocal(ctx context.Context, exe, url, dataDir string, wait time.Duration) error {
	addr := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")
	switch probe(ctx, url) {
	case probeUp:
		return nil
	case probeOther:
		return fmt.Errorf("something other than vaquero is using %s; stop it, or set VQ_ADDR to another address", addr)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(dataDir, "vaquero.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "serve")
	cmd.Env = append(os.Environ(), "VQ_DATA_DIR="+dataDir, "VQ_ADDR="+addr)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	logf.Close()
	if err != nil {
		return fmt.Errorf("couldn't start vaquero: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return fmt.Errorf("vaquero stopped while starting on %s; see %s", addr, logPath)
		case <-deadline.C:
			return fmt.Errorf("vaquero didn't start on %s within %v; see %s", addr, wait, logPath)
		case <-tick.C:
			if probe(ctx, url) == probeUp {
				return nil
			}
		}
	}
}
