package main

import (
	"os"
	"testing"
)

// TestMain lets the test binary stand in for `vaquero` when the CLI starts
// a local server: with VQ_TEST_AS_VAQUERO set it runs the real command.
func TestMain(m *testing.M) {
	if os.Getenv("VQ_TEST_AS_VAQUERO") == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}
