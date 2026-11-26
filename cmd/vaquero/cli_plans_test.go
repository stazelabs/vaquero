package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLIPlanSaveFromStdin(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Planned")
	if code, _, errOut := cli(t, "plan", "show", "VAQ-1"); code != 1 || !strings.Contains(errOut, "VAQ-1 has no plan.") {
		t.Fatalf("no plan: %d %q", code, errOut)
	}

	stdin = strings.NewReader("Step one.\nStep two.\n")
	t.Cleanup(func() { stdin = nil })
	if out := mustCLI(t, "plan", "save", "vaq-1", "-"); !strings.Contains(out, "Saved the plan for VAQ-1 (revision 1).") {
		t.Fatalf("%q", out)
	}
	out := mustCLI(t, "plan", "show", "VAQ-1")
	if !strings.Contains(out, "Revision 1") || !strings.Contains(out, "Step one.\nStep two.") {
		t.Fatalf("%q", out)
	}

	file := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(file, []byte("Step one.\nStep two, revised.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var p api.Plan
	if err := json.Unmarshal([]byte(mustCLI(t, "plan", "save", "VAQ-1", file, "--revision", "1", "--json")), &p); err != nil || p.Revision != 2 {
		t.Fatalf("%v %+v", err, p)
	}
	if out := mustCLI(t, "plan", "show", "VAQ-1", "--revision", "1"); !strings.Contains(out, "Step two.") || strings.Contains(out, "revised") {
		t.Fatalf("revision 1: %q", out)
	}

	// A save based on an old revision exits 3 and shows the plan as it is.
	stdin = strings.NewReader("Someone else's take.\n")
	code, out, errOut := cli(t, "plan", "save", "VAQ-1", "-", "--revision", "1")
	if code != 3 || !strings.Contains(errOut, "changed since you read it") || !strings.Contains(out, "Step two, revised.") || !strings.Contains(out, "Revision 2") {
		t.Fatalf("stale: %d %q %q", code, out, errOut)
	}
	for _, args := range [][]string{{"plan"}, {"plan", "save", "VAQ-1"}, {"plan", "show", "VAQ-1", "--revision", "0"}, {"plan", "edit", "VAQ-1"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}
