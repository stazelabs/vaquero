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

	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/logfile"
)

type probeResult int

// silentWait is how long a listener that isn't this data directory's
// vaquero may take to answer before it counts as something else.
const silentWait = 5 * time.Second

const (
	probeUp      probeResult = iota // vaquero answered /healthz
	probeRefused                    // nothing is listening
	probeSilent                     // something accepted but didn't answer in time (maybe vaquero, still migrating)
	probeOther                      // something answered that isn't vaquero
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
		return probeSilent
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
// waits up to wait for it. Several CLIs may do this at once: the servers
// that lose the race for the address exit, and their CLIs use the winner.
// A listener that hasn't answered yet may be vaquero still migrating, so
// it is given until wait. It never hangs: something else on the address,
// or no answer in time, is a plain error naming the address or the log.
// started is true when this call started a server (a server already
// answering gives false), so the caller can say where it is.
func EnsureLocal(ctx context.Context, exe, url, dataDir string, wait time.Duration) (started bool, err error) {
	addr := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")
	foreign := fmt.Errorf("something other than vaquero is using %s; stop it, or set VQ_ADDR to another address", addr)
	deadline := time.Now().Add(wait)
	// A listener that doesn't answer yet is given the whole wait only when a
	// vaquero holds this data directory (one still migrating, started by
	// another command); anything else gets a few seconds.
	silentUntil := time.Now().Add(min(wait, silentWait))
	if _, running, _ := lockfile.Holder(dataDir); running {
		silentUntil = deadline
	}
	first := probe(ctx, url)
	for first == probeSilent && time.Now().Before(silentUntil) {
		first = probe(ctx, url)
	}
	switch first {
	case probeUp:
		return false, nil
	case probeOther, probeSilent:
		return false, foreign
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return false, err
	}
	logPath := filepath.Join(dataDir, "vaquero.log")
	// One old log is kept: the server rotates it as it writes, and a log
	// already at the limit moves aside before this one starts.
	_ = logfile.Rotate(logPath, logfile.Max)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	cmd := exec.Command(exe, "serve")
	cmd.Env = append(os.Environ(), "VQ_DATA_DIR="+dataDir, "VQ_ADDR="+addr)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	logf.Close()
	if err != nil {
		return false, fmt.Errorf("couldn't start vaquero: %w", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	stopped := false
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-exited:
			// Usually another CLI's server won the address; keep probing.
			stopped, exited = true, nil
		case <-timer.C:
			if stopped {
				return false, fmt.Errorf("vaquero stopped while starting on %s; see %s", addr, logPath)
			}
			return false, fmt.Errorf("vaquero didn't start on %s within %v; see %s", addr, wait, logPath)
		case <-tick.C:
			switch probe(ctx, url) {
			case probeUp:
				return true, nil
			case probeRefused:
				// Ours stopped and nothing took the address: no server is
				// coming (one that lost a race exits because the winner
				// already listens).
				if stopped {
					return false, fmt.Errorf("vaquero stopped while starting on %s; see %s", addr, logPath)
				}
			}
		}
	}
}

// WaitUp waits up to wait for the server at url to answer /healthz, without
// starting one: for a server a service manager restarts.
func WaitUp(ctx context.Context, url string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		if probe(ctx, url) == probeUp {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vaquero didn't come back on %s within %v", strings.TrimPrefix(url, "http://"), wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
