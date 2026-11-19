package main

import (
	"os"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/version"
)

// TestMain lets the test binary stand in for `vaquero` when the CLI starts
// a local server: with VQ_TEST_AS_VAQUERO set it runs the real command.
func TestMain(m *testing.M) {
	if os.Getenv("VQ_TEST_AS_VAQUERO") == "1" {
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
