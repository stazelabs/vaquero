package readiness_test

import (
	"context"
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
	expect(t, got, readiness.Changed("docs/a.md"))
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
	expect(t, got, readiness.Changed("docs/a.md"))
	files(t, got, "docs/a.md changed docs/b.md")

	r.Remove("docs/b.md")
	r.Commit("remove")
	got = w.check(tk)
	expect(t, got, readiness.Changed("docs/a.md"))
	files(t, got, "docs/a.md gone")
}

// Review I1: a pinned file renamed away and replaced at its path by other
// content is changed, not renamed: the reference names that path, and what
// is there now wasn't approved. Renamed, then the new name edited, is
// changed too.
func TestReadinessRenamedAwayAndReplaced(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	r.Rename("docs/a.md", "docs/b.md")
	r.Commit("rename")
	expect(t, w.check(tk))
	r.WriteCommit("docs/a.md", "# Something else\n", "a new a")
	got := w.check(tk)
	expect(t, got, readiness.Changed("docs/a.md"))
	files(t, got, "docs/a.md changed")

	other := w.ticket(core.TicketInput{Title: "Other", Tier: "light"})
	w.ref(other, "docs/c.md")
	r.WriteCommit("docs/c.md", "# C\n", "c")
	w.approve(other)
	r.Rename("docs/c.md", "docs/d.md")
	r.Commit("rename c")
	r.WriteCommit("docs/d.md", "# D\n", "edit d")
	got = w.check(other)
	expect(t, got, readiness.Changed("docs/c.md"))
	files(t, got, "docs/c.md changed docs/d.md")
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
	expect(t, got, readiness.Changed("docs/b.md"))
	files(t, got, "docs/a.md unchanged", "docs/b.md added")
	if err := w.c.RemoveRef(bg, w.owner.ID, tk.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, w.check(tk))

	// A reference removed: the pin is gone from the ticket.
	other := w.ticket(core.TicketInput{Title: "Other", Tier: "light"})
	a := w.ref(other, "docs/a.md")
	w.approve(other)
	if err := w.c.RemoveRef(bg, w.owner.ID, other.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	got = w.check(other)
	expect(t, got, readiness.Changed("docs/a.md"))
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
	expect(t, w.check(child), readiness.EpicChanged("docs/a.md"))
	expect(t, w.check(epic), readiness.Changed("docs/a.md"))

	// A leaning child's own repo references weren't asked about: it needs
	// its own approval for them.
	r.WriteCommit("docs/a.md", "# A\n", "back")
	w.ref(child, "docs/a.md")
	got := w.check(child)
	expect(t, got, readiness.AskForApproval)
	files(t, got, "docs/a.md added")
	w.approve(child)
	expect(t, w.check(child))
}

// Ruling R-Q16: when the files that can't be checked are the epic's, and
// only because the repo settings changed, the action is to ask again on
// the epic.
func TestReadinessEpicRepoChangedAsksAgainOnTheEpic(t *testing.T) {
	w, _ := gitWorld(t)
	e := w.ticket(core.TicketInput{Title: "Epic"})
	w.plan(e, "1. Child.", 0)
	w.ref(e, "docs/a.md")
	c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
	w.approve(e)
	expect(t, w.check(c))
	clone := gittest.Repo(t)
	clone.WriteCommit("docs/a.md", "# A\n", "a")
	w.p = w.repo(w.p, clone)
	got := w.check(c)
	expect(t, got, readiness.Unchecked(readiness.WhyRepoChanged))
	if got.Action != readiness.ActionAskAgain || got.Epic != w.key(e) {
		t.Fatalf("action %q epic %q, want ask again on %s", got.Action, got.Epic, w.key(e))
	}
}

// Files that can't be checked never pass: no repo path, no checker, no git.
func TestReadinessFilesUnchecked(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	expect(t, w.check(tk))

	keep := w.files
	w.files = nil
	got := w.check(tk)
	expect(t, got, readiness.Unchecked(readiness.WhyNoGit))
	files(t, got, "docs/a.md unchecked")
	if got.Action != "" {
		t.Fatalf("no git: action %q, want none", got.Action)
	}

	// In a subtest, so PATH comes back for the repos made below.
	t.Run("no git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		w.files = readiness.NewFiles(gitread.New())
		expect(t, w.check(tk), readiness.Unchecked(readiness.WhyNoGit))
	})

	w.files = keep
	// Ruling M3: a pin holds only against the repo and main branch it was
	// taken from. Another clone with the same content is unchecked, and so
	// is another branch of the same repo.
	clone := gittest.Repo(t)
	clone.WriteCommit("docs/a.md", "# A\n", "a")
	w.p = w.repo(w.p, clone)
	got = w.check(tk)
	expect(t, got, readiness.Unchecked(readiness.WhyRepoChanged))
	files(t, got, "docs/a.md unchecked")
	// Ruling R-Q16: a changed repo setting is the one why with an action:
	// asking again pins the files from the repo as it is set now.
	if got.Action != readiness.ActionAskAgain || got.Epic != "" {
		t.Fatalf("repo changed: action %q epic %q, want ask again here", got.Action, got.Epic)
	}
	w.p = w.repo(w.p, r)
	expect(t, w.check(tk))
	r.Branch("trunk")
	trunk := "trunk"
	if _, err := w.c.UpdateProject(bg, w.owner.ID, w.p.ID, core.ProjectPatch{MainBranch: &trunk}); err != nil {
		t.Fatal(err)
	}
	expect(t, w.check(tk), readiness.Unchecked(readiness.WhyRepoChanged))

	empty := ""
	if _, err := w.c.UpdateProject(bg, w.owner.ID, w.p.ID, core.ProjectPatch{RepoPath: &empty}); err != nil {
		t.Fatal(err)
	}
	got = w.check(tk)
	expect(t, got, readiness.Unchecked(readiness.WhyNoRepo))
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
			if n == 10 { // the second ten are pinned at another commit
				r.WriteCommit("other.md", "o\n", "elsewhere")
			}
			tk := w.ticket(core.TicketInput{Title: fmt.Sprintf("T%d", n), Tier: "light"})
			w.ref(tk, fmt.Sprintf("docs/%d.md", n))
			w.approve(tk)
			ids = append(ids, tk.ID)
		}
		if i == 0 {
			// In both groups: two edited, two renamed, two renamed and
			// edited, one removed.
			for _, base := range []int{0, 10} {
				r.Write(fmt.Sprintf("docs/%d.md", base), "# changed\n")
				r.Write(fmt.Sprintf("docs/%d.md", base+1), "# changed\n")
				r.Rename(fmt.Sprintf("docs/%d.md", base+2), fmt.Sprintf("moved/%d.md", base+2))
				r.Rename(fmt.Sprintf("docs/%d.md", base+3), fmt.Sprintf("moved/%d.md", base+3))
				r.Rename(fmt.Sprintf("docs/%d.md", base+4), fmt.Sprintf("moved/%d.md", base+4))
				r.Rename(fmt.Sprintf("docs/%d.md", base+5), fmt.Sprintf("moved/%d.md", base+5))
				r.Remove(fmt.Sprintf("docs/%d.md", base+6))
			}
			r.Commit("move things")
			for _, n := range []int{4, 5, 14, 15} {
				r.Write(fmt.Sprintf("moved/%d.md", n), "# edited\n")
			}
			r.Commit("edit moved")
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
	// VAQ: 10 of 20 not ready (4 edited, 4 renamed and edited, 2 removed).
	if ready != 30 || all[ids[0]].Ready || !all[ids[2]].Ready || all[ids[4]].Ready || all[ids[16]].Ready {
		t.Fatalf("%d ready", ready)
	}
	if n := heads(); n > 2 {
		t.Fatalf("main's head read %d times for two projects: %q", n, rec.Calls())
	}
	// One history walk per distinct pinned commit with a pin off main: two,
	// both in VAQ.
	if n := rec.Count("log"); n != 2 {
		t.Fatalf("%d history walks, want 2", n)
	}
	// Per project: Open (2), main's head, one ls-tree; VAQ adds two walks
	// (each with its commit's rev-parse) and one ls-tree for the moved.
	if n := len(rec.Calls()); n > 13 {
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

// Review I2: a pin whose history can't be walked (its commit gone after a
// rewrite) is unchecked, and that answer is kept while main stays put: no
// walk per request.
func TestReadinessUncheckedIsCached(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	r.Git("checkout", "-q", "--orphan", "rewritten")
	r.Git("rm", "-q", "-r", "-f", ".")
	r.WriteCommit("other.md", "o\n", "a new history")
	r.Git("branch", "-M", "main")
	r.Git("reflog", "expire", "--expire=now", "--all")
	r.Git("gc", "-q", "--prune=now")
	rec := gittest.Record(w.c.Git)
	got := w.check(tk)
	expect(t, got, readiness.Unchecked(readiness.WhyUnreadable))
	files(t, got, "docs/a.md unchecked")
	rec.Reset()
	expect(t, w.check(tk), readiness.Unchecked(readiness.WhyUnreadable))
	for _, sub := range []string{"log", "ls-tree"} {
		if n := rec.Count(sub); n != 0 {
			t.Fatalf("an unchecked pin ran %d %s again at the same head", n, sub)
		}
	}
}

// Re-review N1: a transient git failure (here injected, as gitread's own
// timeout would be) leaves the pins unchecked for that call only: the next
// call, main unchanged, asks git again and the ticket is ready.
func TestReadinessTransientFailureNotCached(t *testing.T) {
	w, r := gitWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	r.Rename("docs/a.md", "docs/b.md")
	r.Commit("rename") // the path left main: the check walks its history
	var fail string    // the subcommand to fail once
	w.c.Git.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args []string, env []string, limit int) ([]byte, error) {
			if fail != "" && gitread.Subcommand(args) == fail {
				fail = ""
				return nil, &gitread.Error{Sub: "x", Code: -1, Line: "timed out"}
			}
			return next(ctx, dir, args, env, limit)
		}
	})
	for _, sub := range []string{"ls-tree", "log"} {
		fail = sub
		all, err := readiness.CheckAll(bg, w.d.RQ, w.files, []string{tk.ID})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, all[tk.ID], readiness.Unchecked(readiness.WhyUnreadable))
		all, err = readiness.CheckAll(bg, w.d.RQ, w.files, []string{tk.ID})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, all[tk.ID])
		files(t, all[tk.ID], "docs/a.md renamed docs/b.md")
		// Move main, so the next round starts uncached.
		r.WriteCommit("other.md", sub+"\n", "move main")
	}
}

// C2: in a partial clone, a pin whose rename can be seen only through a
// blob the clone lacks is unchecked, never ready, and checking it fetches
// nothing from the promisor remote.
func TestReadinessPartialCloneUnchecked(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("docs/a.md", strings.Repeat("line\n", 20), "a")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.PartialClone(t, bare)

	w := newWorld(t)
	w.c.Git = gitread.New()
	w.files = readiness.NewFiles(w.c.Git)
	w.p = w.repo(w.p, r)
	tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
	w.ref(tk, "docs/a.md")
	w.approve(tk)
	expect(t, w.check(tk))

	// The remote renames the file with an edit; the clone's main follows
	// without a checkout, so the new blob is never downloaded.
	seed.Rename("docs/a.md", "docs/b.md")
	seed.Write("docs/b.md", strings.Repeat("line\n", 20)+"more\n")
	seed.Commit("rename with an edit")
	seed.Git("push", "-q", "origin", "main")
	r.Branch("elsewhere")
	r.Git("fetch", "-q", "origin")
	r.Git("update-ref", "refs/heads/main", "refs/remotes/origin/main")
	packs := func() string { return r.Git("count-objects", "-v") }
	before := packs()

	got := w.check(tk)
	expect(t, got, readiness.Unchecked(readiness.WhyUnreadable))
	files(t, got, "docs/a.md unchecked")
	if after := packs(); after != before {
		t.Fatalf("checking fetched objects:\n%s\n%s", before, after)
	}
}

// I1: a pinned file renamed to a path with control characters (ESC ] 52,
// U+009B) is changed, and its new path is never shown.
func TestReadinessRenameToControlCharacters(t *testing.T) {
	for _, name := range []string{"docs/\x1b]52;c;aGk=\x07.md", "docs/\u009b2J.md"} {
		w, r := gitWorld(t)
		tk := w.ticket(core.TicketInput{Title: "Build", Tier: "light"})
		w.ref(tk, "docs/a.md")
		w.approve(tk)
		r.Git("mv", "docs/a.md", name)
		r.Commit("rename")
		got := w.check(tk)
		expect(t, got, readiness.Changed("docs/a.md"))
		files(t, got, "docs/a.md changed")
	}
}
