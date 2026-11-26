package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

// statusOrderCLI is the project's status names in order, through the CLI.
func statusOrderCLI(t *testing.T) string {
	t.Helper()
	var ss []api.Status
	if err := json.Unmarshal([]byte(mustCLI(t, "statuses", "VAQ", "--json")), &ss); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range ss {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

func TestCLIStatuses(t *testing.T) {
	e := cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Something")

	table := mustCLI(t, "statuses", "VAQ")
	rows := strings.Split(strings.TrimSpace(table), "\n")
	if len(rows) != 7 || !regexp.MustCompile(`^NAME\s+CATEGORY\s+TICKETS$`).MatchString(rows[0]) ||
		!regexp.MustCompile(`^Todo\s+todo\s+1$`).MatchString(rows[2]) || !regexp.MustCompile(`^In progress\s+in progress\s+0$`).MatchString(rows[3]) {
		t.Fatalf("%q", table)
	}

	if out := mustCLI(t, "status", "add", "vaq", "Needs decision", "--category", "backlog"); strings.TrimSpace(out) != "Needs decision (backlog), position 2" {
		t.Fatalf("%q", out)
	}
	mustCLI(t, "status", "add", "VAQ", "Blocked", "--category", "in progress", "--after", "in review")
	mustCLI(t, "status", "add", "VAQ", "Triage", "--before", "Backlog", "--category", "backlog")
	if got := statusOrderCLI(t); got != "Triage, Backlog, Needs decision, Todo, In progress, In review, Blocked, Done, Cancelled" {
		t.Fatal(got)
	}
	mustCLI(t, "status", "move", "VAQ", "Blocked", "--before", "In progress")
	mustCLI(t, "status", "rename", "VAQ", "triage", "Inbox")
	if got := statusOrderCLI(t); got != "Inbox, Backlog, Needs decision, Todo, Blocked, In progress, In review, Done, Cancelled" {
		t.Fatal(got)
	}
	if out := mustCLI(t, "status", "delete", "VAQ", "Inbox"); strings.TrimSpace(out) != "Deleted status Inbox from VAQ." {
		t.Fatalf("%q", out)
	}
	var s api.Status
	if err := json.Unmarshal([]byte(mustCLI(t, "status", "move", "VAQ", "Needs decision", "--after", "Todo", "--json")), &s); err != nil || s.Name != "Needs decision" || s.Position != 3 {
		t.Fatalf("%v %+v", err, s)
	}

	// Refusals: the server's message, exit 1; usage, exit 2.
	if code, _, errOut := cli(t, "status", "delete", "VAQ", "Todo"); code != 1 || !strings.Contains(errOut, "Move this status's tickets first.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "status", "rename", "VAQ", "Nope", "X"); code != 1 || !strings.Contains(errOut, "No status Nope in VAQ.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "status", "add", "VAQ", "X"); code != 2 || !strings.Contains(errOut, "--category") {
		t.Fatalf("no category: %d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "status", "move", "VAQ", "Todo"); code != 2 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("no place: %d %q", code, errOut)
	}
	if code, _, _ := cli(t, "status", "move", "VAQ", "Todo", "--before", "Done", "--after", "Done"); code != 2 {
		t.Fatalf("both: %d", code)
	}
	if code, _, _ := cli(t, "status", "frobnicate", "VAQ"); code != 2 {
		t.Fatalf("unknown verb: %d", code)
	}

	// --agent: the change is the agent's.
	mustCLI(t, "status", "add", "VAQ", "Spare", "--category", "todo", "--agent", "planner")
	evs, err := e.Deps.R.ListEventsNewest(t.Context(), 1)
	if err != nil || evs[0].Kind != "status.created" || evs[0].ActorName != "planner" {
		t.Fatalf("%v %+v", err, evs)
	}
}
