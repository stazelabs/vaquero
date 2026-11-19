package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/lockfile"
	"github.com/stazelabs/vaquero/internal/version"
)

// asRelease makes this CLI, and any server it starts, release v.
func asRelease(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Setenv("VQ_TEST_VERSION", v)
	t.Cleanup(func() { version.Version = old })
}

// serverAt starts a background server stamped with release v and returns
// its pid.
func serverAt(t *testing.T, dataDir, v string) int {
	t.Helper()
	asRelease(t, v)
	if code, _, e := runCLI(t, "projects"); code != 0 {
		t.Fatalf("projects: %s", e)
	}
	pid, running, _ := lockfile.Holder(dataDir)
	if !running {
		t.Fatal("no background server")
	}
	return pid
}

// serverVersion asks the running local server its release.
func serverVersion(t *testing.T) string {
	t.Helper()
	p, _, err := client.Resolve("", os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	c := &client.Client{BaseURL: p.URL, Token: client.LocalToken(p.DataDir)}
	if err := c.Do(t.Context(), "GET", "/api/v1/me", nil, nil); err != nil {
		t.Fatal(err)
	}
	return c.ServerVersion()
}

const restartLine = "vaquero: restarting the local server to run 0.2.0 (it was running 0.1.0)"

func TestStaleLocalServerRestarted(t *testing.T) {
	dataDir, _ := localEnv(t)
	before := serverAt(t, dataDir, "0.1.0")
	asRelease(t, "0.2.0") // as if brew upgrade replaced the binary
	code, out, e := runCLI(t, "projects")
	if code != 0 || !strings.Contains(e, restartLine) {
		t.Fatalf("%d out=%q err=%q", code, out, e)
	}
	if strings.Count(e, "restarting") != 1 {
		t.Fatalf("said it more than once: %q", e)
	}
	after, running, _ := lockfile.Holder(dataDir)
	if !running || after == before {
		t.Fatalf("server not replaced: before %d after %d running %v", before, after, running)
	}
	if v := serverVersion(t); v != "0.2.0" {
		t.Fatalf("server runs %q", v)
	}
	// The next command finds it current and says nothing.
	if _, _, e := runCLI(t, "projects"); strings.Contains(e, "restarting") {
		t.Fatalf("restarted again: %q", e)
	}
}

func TestNewerServerNotRestarted(t *testing.T) {
	dataDir, _ := localEnv(t)
	before := serverAt(t, dataDir, "0.3.0")
	asRelease(t, "0.2.0")
	_, _, e := runCLI(t, "projects")
	if pid, _, _ := lockfile.Holder(dataDir); pid != before || strings.Contains(e, "restarting") {
		t.Fatalf("restarted a newer server: %q", e)
	}
}

func TestDevCLINeverRestarts(t *testing.T) {
	dataDir, _ := localEnv(t)
	before := serverAt(t, dataDir, "0.1.0")
	asRelease(t, "0.0.0-dev")
	_, _, e := runCLI(t, "projects")
	if pid, _, _ := lockfile.Holder(dataDir); pid != before || strings.Contains(e, "restarting") {
		t.Fatalf("a dev CLI restarted the server: %q", e)
	}
}

func TestDevServerNotRestarted(t *testing.T) {
	dataDir, _ := localEnv(t)
	before := serverAt(t, dataDir, "0.0.0-dev")
	asRelease(t, "0.2.0")
	_, _, e := runCLI(t, "projects")
	if pid, _, _ := lockfile.Holder(dataDir); pid != before || strings.Contains(e, "restarting") {
		t.Fatalf("restarted a dev server: %q", e)
	}
}

func TestConcurrentStaleRestart(t *testing.T) {
	dataDir, _ := localEnv(t)
	before := serverAt(t, dataDir, "0.1.0")
	asRelease(t, "0.2.0")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	errs := make([]string, 2)
	for i := range 2 {
		wg.Go(func() { codes[i], _, errs[i] = runCLI(t, "projects") })
	}
	wg.Wait()
	for i := range 2 {
		if codes[i] != 0 {
			t.Fatalf("command %d failed: %q", i, errs[i])
		}
	}
	after, running, _ := lockfile.Holder(dataDir)
	if !running || after == before {
		t.Fatal("server not replaced")
	}
	if v := serverVersion(t); v != "0.2.0" {
		t.Fatalf("server runs %q", v)
	}
}

func TestStaleRestartUnderBrewServices(t *testing.T) {
	dataDir, addr := localEnv(t)
	before := serverAt(t, dataDir, "0.1.0")
	home, _ := os.UserHomeDir()
	p := brewServiceFile(home, "darwin")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, nil, 0o600)
	old := serviceOS
	serviceOS = "darwin"
	t.Cleanup(func() { serviceOS = old })
	asRelease(t, "0.2.0")
	// The fake service manager: once the old server stops, start the new
	// one, a moment later, as launchd would.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if pid, running, _ := lockfile.Holder(dataDir); !running || pid != before {
				time.Sleep(300 * time.Millisecond)
				exe, _ := os.Executable()
				_ = client.EnsureLocal(ctx, exe, "http://"+addr, dataDir, 5*time.Second)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	code, _, e := runCLI(t, "projects")
	if code != 0 || !strings.Contains(e, restartLine) {
		t.Fatalf("%d %q", code, e)
	}
	if v := serverVersion(t); v != "0.2.0" {
		t.Fatalf("server runs %q", v)
	}
}

// Under brew services a command that finds no server waits for the service
// manager instead of starting its own, which would hold the lock and leave
// launchd's copy failing to start, again and again.
func TestBrewServiceStartIsLeftToTheManager(t *testing.T) {
	dataDir, addr := localEnv(t)
	home, _ := os.UserHomeDir()
	p := brewServiceFile(home, "darwin")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, nil, 0o600)
	old := serviceOS
	serviceOS = "darwin"
	t.Cleanup(func() { serviceOS = old })
	// The fake service manager starts its server, stamped so it can be
	// told apart, a second late.
	go func() {
		time.Sleep(time.Second)
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "serve")
		cmd.Env = append(os.Environ(), "VQ_TEST_VERSION=9.9.9-svc", "VQ_DATA_DIR="+dataDir, "VQ_ADDR="+addr)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		_ = cmd.Start()
	}()
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range 2 {
		wg.Go(func() { codes[i], _, _ = runCLI(t, "projects") })
	}
	wg.Wait()
	if codes[0] != 0 || codes[1] != 0 {
		t.Fatalf("exit codes %v", codes)
	}
	if v := serverVersion(t); v != "9.9.9-svc" {
		t.Fatalf("the server running is %q, not the service manager's", v)
	}
}

// The first boot after an upgrade snapshots and migrates before it serves,
// which can take a while on a big database; the command waits for it.
func TestStaleRestartWaitsForASlowStart(t *testing.T) {
	dataDir, _ := localEnv(t)
	serverAt(t, dataDir, "0.1.0")
	asRelease(t, "0.2.0")
	t.Setenv("VQ_TEST_START_DELAY", "7s")
	code, _, e := runCLI(t, "projects")
	if code != 0 {
		t.Fatalf("%d %q", code, e)
	}
}
