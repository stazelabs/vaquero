package readiness_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/readiness"
)

// gitWorld is a world whose project has a repo with docs/a.md on main,
// a core that pins files, and a checker over the same git.
func gitWorld(t *testing.T) (*world, *gittest.R) {
	t.Helper()
	w := newWorld(t)
	w.c.Git = gitread.New()
	w.files = readiness.NewFiles(w.c.Git)
	r := gittest.Repo(t)
	r.WriteCommit("docs/a.md", "# A\n", "a")
	w.p = w.repo(w.p, r)
	return w, r
}

func (w *world) repo(p gen.Project, r *gittest.R) gen.Project {
	w.t.Helper()
	dir := r.Dir
	p, err := w.c.UpdateProject(bg, w.owner.ID, p.ID, core.ProjectPatch{RepoPath: &dir})
	if err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) ref(tk gen.Ticket, target string) gen.TicketRef {
	w.t.Helper()
	x, err := w.c.AddRef(bg, w.owner.ID, tk.ID, core.RefInput{Kind: "spec", Target: target})
	if err != nil {
		w.t.Fatal(err)
	}
	return x
}

func files(t *testing.T, got readiness.Readiness, want ...string) {
	t.Helper()
	var have []string
	for _, f := range got.Files {
		s := f.Path + " " + f.State
		if f.Now != "" && f.Now != f.Path {
			s += " " + f.Now
		}
		have = append(have, s)
	}
	if !slices.Equal(have, want) {
		t.Fatalf("files %q, want %q", have, want)
	}
}

func TestReasonsOrder(t *testing.T) {
	want := []string{"Not a todo.", "No tier.", "Plan not approved.", "Epic's plan not approved.",
		"Requirement changed since approval.", "Plan changed since approval.",
		"Epic's requirement changed since approval.", "Epic's plan changed since approval.",
		"Files changed since approval.", "Epic's files changed since approval.", "Files can't be checked."}
	if !slices.Equal(readiness.Reasons, want) {
		t.Fatalf("Reasons %q", readiness.Reasons)
	}
}

// P2-R3: a pinned file is judged by its content on main, renames followed.
func TestReadinessFileChanged(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	got := w.check(tk)
	expect(t, got)
	files(t, got, "docs/a.md unchanged")

	r.WriteCommit("docs/a.md", "# A, changed\n", "change a")
	got = w.check(tk)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md changed")

	// Changed back: the content is what was pinned, whatever the commits.
	r.WriteCommit("docs/a.md", "# A\n", "back")
	r.WriteCommit("other.md", "o\n", "elsewhere")
	expect(t, w.check(tk))

	r.Rename("docs/a.md", "docs/b.md")
	r.Commit("rename")
	got = w.check(tk)
	expect(t, got)
	files(t, got, "docs/a.md renamed docs/b.md")

	r.WriteCommit("docs/b.md", "# B\n", "change b")
	got = w.check(tk)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md changed docs/b.md")

	r.Remove("docs/b.md")
	r.Commit("remove")
	got = w.check(tk)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md gone")
}

// P2-R4: a repo-path reference added or removed since the approval is a
// file change; a URL reference isn't.
func TestReadinessRefAddedAfterApproval(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	w.ref(tk, "https://example.com/spec")
	expect(t, w.check(tk))

	r.WriteCommit("docs/b.md", "# B\n", "b")
	b := w.ref(tk, "docs/b.md")
	got := w.check(tk)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md unchanged", "docs/b.md added")
	if err := w.c.RemoveRef(bg, w.owner.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, w.check(tk))

	// A reference removed: the pin is gone from the ticket.
	other := w.ticket(core.TicketInput{Title: "Other", Tier: "light"})
	a := w.ref(other, "docs/a.md")
	w.approve(other)
	if err := w.c.RemoveRef(bg, w.owner.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	got = w.check(other)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md gone")

	// A ticket approved with no references runs no git at all.
	plain := w.ticket(core.TicketInput{Title: "Plain", Tier: "light"})
	w.approve(plain)
	rec := gittest.Record(w.c.Git)
	expect(t, w.check(plain))
	if n := len(rec.Calls()); n != 0 {
		t.Fatalf("%d git commands for a ticket with no files", n)
	}
}

// A child leaning on its epic is not ready when the epic's pinned file
// changes.
func TestReadinessEpicFilesChanged(t *testing.T) {
	w, r := gitWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Epic", Tier: "heavy"})
	w.plan(epic, "The plan.", 0)
	w.ref(epic, "docs/a.md")
	child := w.ticket(core.TicketInput{Title: "Child", Tier: "light", ParentID: epic.ID})
	w.approve(epic)
	expect(t, w.check(child))
	r.WriteCommit("docs/a.md", "# A, changed\n", "change a")
	expect(t, w.check(child), readiness.EpicFilesChanged)
	expect(t, w.check(epic), readiness.FilesChanged)

	// A leaning child's own repo references weren't asked about: it needs
	// its own approval for them.
	r.WriteCommit("docs/a.md", "# A\n", "back")
	w.ref(child, "docs/a.md")
	got := w.check(child)
	expect(t, got, readiness.FilesChanged)
	files(t, got, "docs/a.md added")
	w.approve(child)
	expect(t, w.check(child))
}

// Files that can't be checked never pass: no repo path, no checker, no git.
func TestReadinessFilesUnchecked(t *testing.T) {
	w, _ := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	expect(t, w.check(tk))

	keep := w.files
	w.files = nil
	got := w.check(tk)
	expect(t, got, readiness.FilesUnchecked)
	files(t, got, "docs/a.md unchecked")

	t.Setenv("PATH", t.TempDir())
	w.files = readiness.NewFiles(gitread.New())
	expect(t, w.check(tk), readiness.FilesUnchecked)

	w.files = keep
	empty := ""
	if _, err := w.c.UpdateProject(bg, w.owner.ID, w.p.ID, core.ProjectPatch{RepoPath: &empty}); err != nil {
		t.Fatal(err)
	}
	got = w.check(tk)
	expect(t, got, readiness.FilesUnchecked)
	files(t, got, "docs/a.md unchecked")
}

// CheckAll reads main's head once per project, and reads no blob again
// while main hasn't moved.
func TestReadinessGitBatched(t *testing.T) {
	w := newWorld(t)
	rs := gitread.New()
	w.c.Git = rs
	w.files = readiness.NewFiles(rs)
	var ids []string
	for i, key := range []string{"VAQ", "OTH"} {
		p := w.p
		if i == 1 {
			var err error
			if p, err = w.c.CreateProject(bg, w.owner.ID, core.ProjectInput{Key: key, Name: "Other"}); err != nil {
				t.Fatal(err)
			}
			on := true
			if p, err = w.c.UpdateProject(bg, w.owner.ID, p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
				t.Fatal(err)
			}
		}
		r := gittest.Repo(t)
		for n := range 20 {
			r.Write(fmt.Sprintf("docs/%d.md", n), fmt.Sprintf("# %d\n", n))
		}
		r.Commit("docs")
		p = w.repo(p, r)
		w.p = p
		for n := range 20 {
			tk := w.ticket(core.TicketInput{Title: fmt.Sprintf("T%d", n), Tier: "light"})
			w.ref(tk, fmt.Sprintf("docs/%d.md", n))
			w.approve(tk)
			ids = append(ids, tk.ID)
		}
		if i == 0 {
			r.WriteCommit("docs/0.md", "# changed\n", "change 0")
		}
	}
	rec := gittest.Record(rs)
	heads := func() int {
		n := 0
		for _, c := range rec.Calls() {
			if slices.ContainsFunc(c, func(a string) bool { return strings.HasPrefix(a, "refs/heads/main") }) && gitread.Subcommand(c) == "rev-parse" {
				n++
			}
		}
		return n
	}
	all, err := readiness.CheckAll(bg, w.d.RQ, w.files, ids)
	if err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, id := range ids {
		if all[id].Ready {
			ready++
		}
	}
	if ready != 39 || all[ids[0]].Ready {
		t.Fatalf("%d ready; the changed one %+v", ready, all[ids[0]])
	}
	if n := heads(); n > 2 {
		t.Fatalf("main's head read %d times for two projects: %q", n, rec.Calls())
	}
	if n := len(rec.Calls()); n > 12 {
		t.Fatalf("%d git commands for 40 tickets in two projects: %q", n, rec.Calls())
	}
	rec.Reset()
	if _, err := readiness.CheckAll(bg, w.d.RQ, w.files, ids); err != nil {
		t.Fatal(err)
	}
	if n := heads(); n > 2 {
		t.Fatalf("main's head read %d times on the second call", n)
	}
	for _, sub := range []string{"ls-tree", "cat-file", "log", "show", "diff"} {
		if n := rec.Count(sub); n != 0 {
			t.Fatalf("the second call ran %d %s: %q", n, sub, rec.Calls())
		}
	}
}
