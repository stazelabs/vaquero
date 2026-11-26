package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLIRefs(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Referenced")
	out := mustCLI(t, "ref", "add", "vaq-1", "design", "https://example.com/d", "--anchor", "top", "--label", "Mock")
	if !strings.Contains(out, "Added a design reference to VAQ-1.") {
		t.Fatalf("%q", out)
	}
	show := mustCLI(t, "ticket", "show", "VAQ-1")
	if !strings.Contains(show, "References:") || !strings.Contains(show, "design") || !strings.Contains(show, "https://example.com/d#top") || !strings.Contains(show, "Mock") {
		t.Fatalf("show: %q", show)
	}
	var tk api.Ticket
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", "VAQ-1", "--json")), &tk); err != nil || len(tk.References) != 1 {
		t.Fatalf("%v %+v", err, tk)
	}
	id := tk.References[0].ID
	if !strings.Contains(show, id) {
		t.Fatalf("show lacks the id %s: %q", id, show)
	}
	if code, _, errOut := cli(t, "ref", "add", "VAQ-1", "spec", "docs/a.md"); code != 1 || !strings.Contains(errOut, "Set the project's repository first.") {
		t.Fatalf("repo path: %d %q", code, errOut)
	}
	if out := mustCLI(t, "ref", "remove", "VAQ-1", id); !strings.Contains(out, "Removed the reference from VAQ-1.") {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := cli(t, "ref", "remove", "VAQ-1", id); code != 1 || !strings.Contains(errOut, "No such reference.") {
		t.Fatalf("again: %d %q", code, errOut)
	}
	for _, args := range [][]string{{"ref"}, {"ref", "add", "VAQ-1", "spec"}, {"ref", "remove", "VAQ-1"}, {"ref", "list", "VAQ-1"},
		{"ref", "remove", "VAQ-1", "x", "--label", "l"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}
