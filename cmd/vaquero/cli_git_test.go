package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func TestCLIFileAndCommits(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Board")
	if code, _, errOut := cli(t, "commits", "VAQ-1"); code != 1 || !strings.Contains(errOut, "No repository set for this project.") {
		t.Fatalf("no repo: %d %q", code, errOut)
	}
	r := gittest.Repo(t)
	old := r.WriteCommit("docs/a.md", "# Old\n", "Start (VAQ-1)")
	r.WriteCommit("docs/a.md", "# New\nMore.\n", "Finish (VAQ-1)")
	r.Tag("v0.1.0")
	mustCLI(t, "project", "edit", "VAQ", "--repo", r.Dir)

	if out := mustCLI(t, "file", "vaq", "docs/a.md"); out != "# New\nMore.\n" {
		t.Fatalf("file: %q", out)
	}
	if out := mustCLI(t, "file", "VAQ", "docs/a.md", "--at", old); out != "# Old\n" {
		t.Fatalf("file at: %q", out)
	}
	var f api.File
	if err := json.Unmarshal([]byte(mustCLI(t, "file", "VAQ", "docs/a.md", "--json")), &f); err != nil || f.Blob == "" || !f.Markdown {
		t.Fatalf("%v %+v", err, f)
	}
	if code, _, errOut := cli(t, "file", "VAQ", "docs/nope.md"); code != 1 || !strings.Contains(errOut, "No file docs/nope.md on main.") {
		t.Fatalf("missing: %d %q", code, errOut)
	}
	if code, _, errOut := cli(t, "file", "VAQ", "../x"); code != 1 || !strings.Contains(errOut, "isn't a path") {
		t.Fatalf("bad path: %d %q", code, errOut)
	}

	out := mustCLI(t, "commits", "vaq-1")
	for _, want := range []string{r.Rev("main")[:7], "Finish (VAQ-1)", "Start (VAQ-1)", "+3 −1 in 1 file", "Released in v0.1.0."} {
		if !strings.Contains(out, want) {
			t.Errorf("commits lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Finish") > strings.Index(out, "Start") {
		t.Errorf("not newest first:\n%s", out)
	}
	var tc api.TicketCommits
	if err := json.Unmarshal([]byte(mustCLI(t, "commits", "VAQ-1", "--json")), &tc); err != nil || len(tc.Commits) != 2 || tc.Released != "v0.1.0" {
		t.Fatalf("%v %+v", err, tc)
	}
	for _, args := range [][]string{{"file", "VAQ"}, {"commits"}, {"commits", "VAQ-1", "--at", "main"}} {
		if code, _, errOut := cli(t, args...); code != 2 || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: %d %q", args, code, errOut)
		}
	}
}
