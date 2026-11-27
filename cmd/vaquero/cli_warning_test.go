package main

import (
	"strings"
	"testing"
)

// K1 review M1: a write to a ticket someone else claims succeeds, and the
// CLI passes the answer's warning on, on stderr.
func TestCLIPrintsClaimWarning(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Shared")
	mustCLI(t, "claim", "VAQ-1", "--agent", "builder")
	code, out, errOut := cli(t, "comment", "new", "VAQ-1", "a note")
	if code != 0 || !strings.Contains(errOut, "vaquero: warning: Claimed by builder until ") || strings.Contains(out, "warning") {
		t.Fatalf("%d %q %q", code, out, errOut)
	}
	// The claimant hears nothing.
	if code, _, errOut := cli(t, "comment", "new", "VAQ-1", "mine", "--agent", "builder"); code != 0 || errOut != "" {
		t.Fatalf("%d %q", code, errOut)
	}
}
