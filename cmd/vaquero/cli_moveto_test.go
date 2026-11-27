package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

// vaquero ticket move-to TICKET KEY moves the ticket and prints its new
// key; the old key keeps working; a stale --version is exit 3.
func TestCLITicketMoveTo(t *testing.T) {
	e := cliEnv(t)
	if _, err := e.Deps.Core.CreateProject(context.Background(), e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	mustCLI(t, "ticket", "new", "VAQ", "One")
	mustCLI(t, "ticket", "new", "VAQ", "Two")
	if code, _, errOut := cli(t, "ticket", "transfer", "VAQ-1", "OTH", "--version", "7"); code != 3 || !strings.Contains(errOut, "changed since you read it") {
		t.Fatalf("stale: %d %q", code, errOut)
	}
	if out := mustCLI(t, "ticket", "transfer", "vaq-1", "oth", "--version", "1"); strings.TrimSpace(out) != "VAQ-1 is now OTH-1." {
		t.Fatalf("%q", out)
	}
	var tk struct {
		Key        string
		FormerKeys []string `json:"former_keys"`
	}
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", "VAQ-1", "--json")), &tk); err != nil || tk.Key != "OTH-1" || len(tk.FormerKeys) != 1 {
		t.Fatalf("%v %+v", err, tk)
	}
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "Formerly VAQ-1") {
		t.Fatalf("%q", out)
	}
	// Typed by its old key, it names the key it had just before.
	if _, err := e.Deps.Core.CreateProject(context.Background(), e.Deps.Actor.ID, core.ProjectInput{Key: "THR", Name: "Third"}); err != nil {
		t.Fatal(err)
	}
	if out := mustCLI(t, "ticket", "transfer", "VAQ-1", "THR"); strings.TrimSpace(out) != "OTH-1 is now THR-1." {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := cli(t, "ticket", "transfer", "VAQ-2", "NOPE"); code != 1 || !strings.Contains(errOut, "No project NOPE.") {
		t.Fatalf("%d %q", code, errOut)
	}
	for _, args := range [][]string{{"ticket", "transfer", "VAQ-2"}, {"ticket", "transfer", "VAQ-2", "OTH", "extra"}, {"ticket", "transfer", "VAQ-2", "OTH", "--version", "0"}} {
		if code, _, _ := cli(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
