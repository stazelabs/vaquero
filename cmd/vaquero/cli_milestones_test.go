package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

func TestCLIMilestones(t *testing.T) {
	e := cliEnv(t)
	if out := mustCLI(t, "milestones", "VAQ"); strings.TrimSpace(out) != "No milestones in VAQ." {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "milestone", "new", "vaq", "Phase 4", "--description", "Sign-in.", "--due", "2027-03-31"); strings.TrimSpace(out) != "Phase 4 (open), 0 of 0 done, due 2027-03-31" {
		t.Fatalf("%q", out)
	}
	stdin = strings.NewReader("From standard input.\n")
	t.Cleanup(func() { stdin = nil })
	mustCLI(t, "milestone", "new", "VAQ", "Phase 5", "--description", "-")
	mustCLI(t, "ticket", "new", "VAQ", "Sign in", "--milestone", "phase 4")

	table := mustCLI(t, "milestones", "VAQ")
	rows := strings.Split(strings.TrimSpace(table), "\n")
	if len(rows) != 3 || !regexp.MustCompile(`^NAME\s+DUE\s+DONE\s+STATE$`).MatchString(rows[0]) ||
		!regexp.MustCompile(`^Phase 4\s+2027-03-31\s+0/1\s+open$`).MatchString(rows[1]) || !regexp.MustCompile(`^Phase 5\s+-\s+0/0\s+open$`).MatchString(rows[2]) {
		t.Fatalf("%q", table)
	}

	var m api.Milestone
	if err := json.Unmarshal([]byte(mustCLI(t, "milestone", "edit", "VAQ", "Phase 5", "--json")), &m); err != nil || m.Description != "From standard input." {
		t.Fatalf("%v %+v", err, m)
	}
	stdin = strings.NewReader("Rewritten.")
	m = api.Milestone{}
	if err := json.Unmarshal([]byte(mustCLI(t, "milestone", "edit", "VAQ", "phase 5", "--name", "Phase five", "--description", "-", "--json")), &m); err != nil ||
		m.Name != "Phase five" || m.Description != "Rewritten." {
		t.Fatalf("%v %+v", err, m)
	}
	if out := mustCLI(t, "milestone", "close", "VAQ", "Phase five"); strings.TrimSpace(out) != "Phase five (closed), 0 of 0 done" {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "milestone", "reopen", "VAQ", "Phase five"); !strings.Contains(out, "(open)") {
		t.Fatalf("%q", out)
	}

	if code, _, errOut := cli(t, "milestone", "new", "VAQ", "phase 4"); code != 1 || !strings.Contains(errOut, "A milestone with that name exists.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "milestone", "reopen", "VAQ", "Phase 4"); code != 1 || !strings.Contains(errOut, "It's already open.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "milestone", "close", "VAQ", "Nope"); code != 1 || !strings.Contains(errOut, "No milestone Nope in VAQ.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, _ := cli(t, "milestone", "close", "VAQ"); code != 2 {
		t.Fatalf("no name: %d", code)
	}
	for _, args := range [][]string{
		{"milestone", "close", "VAQ", "Phase 4", "--due", "2027-01-01"},
		{"milestone", "reopen", "VAQ", "Phase 4", "--description", "x"},
		{"milestone", "close", "VAQ", "Phase 4", "--name", "x"},
		{"milestone", "new", "VAQ", "Phase 9", "--name", "x"},
	} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage:") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
	if code, _, _ := cli(t, "milestone", "delete", "VAQ", "Phase 4"); code != 2 {
		t.Fatalf("delete: %d", code)
	}

	mustCLI(t, "milestone", "close", "VAQ", "Phase 4", "--agent", "planner")
	evs, err := e.Deps.R.ListEventsNewest(t.Context(), 1)
	if err != nil || evs[0].Kind != "milestone.closed" || evs[0].ActorName != "planner" {
		t.Fatalf("%v %+v", err, evs)
	}
}
