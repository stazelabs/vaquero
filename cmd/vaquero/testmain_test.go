package main

import (
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/version"
)

// TestMain lets the test binary stand in for `vaquero` when the CLI starts
// a local server: with VQ_TEST_AS_VAQUERO set it runs the real command.
//
// No test asks this machine's real service manager (launchctl, systemctl)
// about the owner's own service: every probe fails unless a test fakes it.
func TestMain(m *testing.M) {
	serviceProbe = noManager
	if os.Getenv("VQ_TEST_AS_VAQUERO") == "1" {
		// A stand-in server runs in its own session, as `vaquero serve`
		// does, so nothing stops it when its test run is killed and skips
		// its cleanups. It stops itself once the test process is gone.
		if pid, err := strconv.Atoi(os.Getenv("VQ_TEST_OWNER_PID")); err == nil && pid > 0 {
			go stopWithOwner(pid)
		}
		// A test can stamp the background server with a release.
		if v := os.Getenv("VQ_TEST_VERSION"); v != "" {
			version.Version = v
		}
		// And make its start slow, as a long migration would.
		if d, err := time.ParseDuration(os.Getenv("VQ_TEST_START_DELAY")); err == nil {
			time.Sleep(d)
		}
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// noManager is a service manager that isn't there: every question fails.
func noManager(string, ...string) ([]byte, error) {
	return nil, errors.New("no service manager in tests")
}

// stopWithOwner sends this process SIGTERM once owner has exited, so serve
// drains and releases its lock; before serve installs its handler, SIGTERM
// simply ends the process. Not SIGINT: a process started from a background
// job inherits SIGINT ignored, and an interrupt that lands before serve's
// handler is lost. If the process is still alive after serve's drain
// window, it exits outright.
func stopWithOwner(owner int) {
	for {
		if err := syscall.Kill(owner, 0); errors.Is(err, syscall.ESRCH) {
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			time.Sleep(drainTimeout + 5*time.Second)
			os.Exit(1)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
