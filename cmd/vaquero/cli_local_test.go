package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/lockfile"
)

// localEnv points the CLI at the built-in local profile: a fresh data
// directory and a free address, with the test binary as `vaquero`.
func localEnv(t *testing.T) (dataDir, addr string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("VAQUERO_PROFILE", "")
	dataDir = filepath.Join(t.TempDir(), "data")
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr = ln.Addr().String()
	ln.Close()
	t.Setenv("VQ_DATA_DIR", dataDir)
	t.Setenv("VQ_ADDR", addr)
	t.Setenv("VQ_TEST_AS_VAQUERO", "1")
	t.Cleanup(func() { killTestServer(dataDir) })
	return dataDir, addr
}

// killTestServer stops a background server started by a test: only a live
// holder of the lock, so a pid this process left in the file (a restore
// holds the lock too) is never signalled.
func killTestServer(dataDir string) {
	pid, running, err := lockfile.Holder(dataDir)
	if err != nil || !running || pid <= 0 || pid == os.Getpid() {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(os.Interrupt)
		time.Sleep(300 * time.Millisecond)
	}
}

// Review Focus 5 through the CLI.
func TestCLILocalPortTakenByOther(t *testing.T) {
	_, _ = localEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	t.Setenv("VQ_ADDR", ln.Addr().String())
	start := time.Now()
	code, _, errOut := cli(t, "projects")
	if code != 1 || !strings.Contains(errOut, ln.Addr().String()) || time.Since(start) > 6*time.Second {
		t.Fatalf("exit %d after %v: %q", code, time.Since(start), errOut)
	}
}

// Parallel commands on a cold start all succeed: the ones whose server
// lost the race use the winner's.
func TestCLIParallelColdStart(t *testing.T) {
	localEnv(t)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	errs := make([]string, 4)
	for i := range codes {
		wg.Go(func() { codes[i], _, errs[i] = cli(t, "projects") })
	}
	wg.Wait()
	for i, c := range codes {
		if c != 0 {
			t.Errorf("command %d: exit %d %q", i, c, errs[i])
		}
	}
}

// Revoking the local token on the admin page doesn't strand the CLI: the
// server writes a new one and the CLI reads it.
func TestCLISurvivesRevokedLocalToken(t *testing.T) {
	dataDir, _ := localEnv(t)
	mustCLI(t, "projects")
	old, _ := os.ReadFile(filepath.Join(dataDir, "token"))
	prefix := strings.TrimSpace(string(old))[:9]
	mustCLI(t, "token", "revoke", prefix)
	mustCLI(t, "projects")
	if now, _ := os.ReadFile(filepath.Join(dataDir, "token")); string(now) == string(old) {
		t.Fatal("token file not renewed")
	}
}
