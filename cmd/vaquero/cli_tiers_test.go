package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLITicketEditTier(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Tiered", "--tier", "design")
	out := mustCLI(t, "ticket", "edit", "VAQ-1", "--tier", "heavy", "--model", "big-model")
	if !strings.Contains(out, "Tier: heavy · Model: big-model") {
		t.Fatalf("%q", out)
	}
	var tk api.Ticket
	if err := json.Unmarshal([]byte(mustCLI(t, "ticket", "show", "VAQ-1", "--json")), &tk); err != nil || tk.Tier != "heavy" || tk.Model != "big-model" {
		t.Fatalf("%v %+v", err, tk)
	}
	if code, _, errOut := cli(t, "ticket", "edit", "VAQ-1", "--tier", "huge"); code != 1 || !strings.Contains(errOut, "Choose design, light, standard or heavy.") {
		t.Fatalf("%d %q", code, errOut)
	}
	out = mustCLI(t, "ticket", "edit", "VAQ-1", "--tier", "", "--model", "")
	if strings.Contains(out, "Tier:") {
		t.Fatalf("cleared: %q", out)
	}
}

func TestCLIProjectEditWorkflow(t *testing.T) {
	cliEnv(t)
	var p api.Project
	if err := json.Unmarshal([]byte(mustCLI(t, "project", "edit", "VAQ", "--workflow", "on", "--json")), &p); err != nil || !p.Workflow {
		t.Fatalf("%v %+v", err, p)
	}
	if out := mustCLI(t, "project", "show", "VAQ"); !strings.Contains(out, "Workflow: on") {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "project", "edit", "VAQ", "--workflow", "off"); strings.Contains(out, "Workflow: on") {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := cli(t, "project", "edit", "VAQ", "--workflow", "maybe"); code != 2 || !strings.Contains(errOut, "--workflow is on or off") {
		t.Fatalf("%d %q", code, errOut)
	}
}
