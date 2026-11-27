package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
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

func TestCLIShowsReadiness(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Plain")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); strings.Contains(out, "Readiness") {
		t.Fatalf("workflow off: %q", out)
	}
	if out := mustCLI(t, "ready", "VAQ"); strings.Contains(out, "TIER") || !strings.Contains(out, "VAQ-1") {
		t.Fatalf("workflow off: %q", out)
	}
	mustCLI(t, "project", "edit", "VAQ", "--workflow", "on")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "Readiness: not ready. Ask for plan approval.") {
		t.Fatalf("%q", out)
	}
	if out := mustCLI(t, "ready", "VAQ"); !strings.Contains(out, "Nothing is ready.") {
		t.Fatalf("%q", out)
	}
}

func TestCLIProjectEditRepo(t *testing.T) {
	cliEnv(t)
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "first")
	r.Git("remote", "add", "origin", "https://user:secret@example.com/x.git")
	var p api.Project
	out := mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir, "--main-branch", "main", "--remote", "origin", "--fetch-minutes", "30", "--json")
	if err := json.Unmarshal([]byte(out), &p); err != nil || p.Repo == nil || p.Repo.FetchMinutes != 30 || p.Repo.RemoteURL != "https://example.com/x.git" || !p.Repo.Available {
		t.Fatalf("%v %+v %s", err, p.Repo, out)
	}
	if out := mustCLI(t, "project", "show", "VAQ"); !strings.Contains(out, "Repository: ") || !strings.Contains(out, "Remote: https://example.com/x.git") || strings.Contains(out, "secret") {
		t.Fatalf("%q", out)
	}
	if code, _, errOut := cli(t, "project", "edit", "VAQ", "--agent", "builder", "--repo", ""); code != 1 || !strings.Contains(errOut, "Only the owner changes the repo settings.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "edit", "VAQ", "--agent", "builder", "--fetch-minutes", "60"); code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "edit", "VAQ", "--fetch-minutes", "3"); code != 1 || !strings.Contains(errOut, "Fetch every 5 to 1440 minutes, or 0 for never.") {
		t.Fatalf("%d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "project", "show", "VAQ", "--repo", "x"); code != 2 || !strings.Contains(errOut, "--repo is for project edit") {
		t.Fatalf("%d %q", code, errOut)
	}
	mustCLI(t, "project", "edit", "VAQ", "--repo", "")
	if out := mustCLI(t, "project", "show", "VAQ", "--json"); strings.Contains(out, `"repo"`) {
		t.Fatalf("%s", out)
	}
}

// P2-R5: ticket show lists the files a ticket's approval pinned.
func TestCLIShowsReadinessFiles(t *testing.T) {
	cliEnv(t)
	r := gittest.Repo(t)
	r.WriteCommit("docs/a.md", "# A\n", "a")
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir, "--workflow", "on")
	mustCLI(t, "ticket", "new", "VAQ", "Build it", "--tier", "light")
	mustCLI(t, "ref", "new", "VAQ-1", "spec", "docs/a.md")
	id := strings.TrimSpace(mustCLI(t, "decision", "ask", "VAQ", "Approve the plan?", "--approval", "plan", "--ticket", "VAQ-1"))
	mustCLI(t, "decision", "answer", id, "Approve")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "Readiness: ready\nFiles: docs/a.md unchanged\n") {
		t.Fatalf("%q", out)
	}
	r.Rename("docs/a.md", "docs/b.md")
	r.Commit("rename")
	r.WriteCommit("docs/b.md", "# B\n", "change")
	if out := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(out, "Readiness: not ready. Changed since approval: docs/a.md. Ask again.\nFiles: docs/a.md changed (now docs/b.md)\n") {
		t.Fatalf("%q", out)
	}
}
