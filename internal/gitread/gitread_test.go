package gitread_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func TestValidPath(t *testing.T) {
	for _, p := range []string{"docs/a.md", "a b/c.md", "README.md", "docs/superpowers/specs/x.md", "a.git/b", ".github/workflows/ci.yml"} {
		if !gitread.ValidPath(p) {
			t.Errorf("ValidPath(%q) = false", p)
		}
	}
	for _, p := range []string{"", "/etc/passwd", "../x", "a/../b", "-rf", "a//b", "a\x00b", ".git/config", "a/.git/config", ".GIT/config",
		"a/", "./a", "a/./b", ":(exclude)a", "a\\b", "a\nb", "a\x7fb", "\xff"} {
		if gitread.ValidPath(p) {
			t.Errorf("ValidPath(%q) = true", p)
		}
	}
}

func TestValidRev(t *testing.T) {
	for _, s := range []string{strings.Repeat("a1", 20), "abc1234", "main", "v0.1.6", "run-o", "origin/main", "feature/x_y", "HEAD"} {
		if !gitread.ValidRev(s) {
			t.Errorf("ValidRev(%q) = false", s)
		}
	}
	for _, s := range []string{"", "-x", "a..b", "HEAD@{1}", "main~1", "main^", ":x", "a b", " main", "main.lock", "a/b.lock", "a/.b", ".a", "a/", "a.", "a//b",
		"main:docs", "a*", "a?", "a[b", "a\\b", "@", "--all", strings.Repeat("a", 256)} {
		if gitread.ValidRev(s) {
			t.Errorf("ValidRev(%q) = true", s)
		}
	}
}

func TestDisplayURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:tok@github.com/o/r.git":   "https://github.com/o/r.git",
		"https://tok@github.com/o/r.git":     "https://github.com/o/r.git",
		"https://github.com/o/r.git":         "https://github.com/o/r.git",
		"ssh://git@example.com:22/o/r.git":   "ssh://example.com:22/o/r.git",
		"git@github.com:o/r.git":             "git@github.com:o/r.git",
		"/srv/git/r.git":                     "/srv/git/r.git",
		"https://u:p@w@host/a@b":             "https://host/a@b",
		"https://u:tok@github.com?x=1":       "https://github.com?x=1",
		"https://user:pa%40ss@host.test/r":   "https://host.test/r",
		"file:///srv/git/r.git":              "file:///srv/git/r.git",
		"https://example.com/path@with/at@s": "https://example.com/path@with/at@s",
	} {
		if got := gitread.DisplayURL(in); got != want {
			t.Errorf("DisplayURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// open makes a Repos and opens r with main and the default remote.
func open(t *testing.T, r *gittest.R) (*gitread.Repos, *gitread.Repo) {
	t.Helper()
	rs := gitread.New()
	if !rs.Available() {
		t.Fatal("git found by gittest but not by gitread")
	}
	g, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	return rs, g
}

func TestOpen(t *testing.T) {
	gittest.Need(t)
	rs := gitread.New()
	ctx := t.Context()
	if _, err := rs.Open(ctx, t.TempDir(), "main", ""); !errors.Is(err, gitread.ErrNotRepo) {
		t.Errorf("not a repo: %v", err)
	}
	if _, err := rs.Open(ctx, "relative/path", "main", ""); err == nil {
		t.Error("a relative path opened")
	}
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "one")
	if _, err := rs.Open(ctx, r.Dir, "main~1", ""); err == nil {
		t.Error("a bad main branch opened")
	}
	if _, err := rs.Open(ctx, r.Dir, "main", "-x"); err == nil {
		t.Error("a bad remote opened")
	}
	g, err := rs.Open(ctx, r.Dir, "main", "")
	if err != nil || g.Remote != "" || g.Main != "main" || g.Path != r.Dir {
		t.Fatalf("%+v %v", g, err)
	}
	r.Git("remote", "add", "origin", "https://example.com/o/r.git")
	r.Git("remote", "add", "upstream", "https://example.com/u/r.git")
	if g, err = rs.Open(ctx, r.Dir, "main", ""); err != nil || g.Remote != "origin" {
		t.Fatalf("default remote %+v %v", g, err)
	}
	if g, err = rs.Open(ctx, r.Dir, "main", "upstream"); err != nil || g.Remote != "upstream" {
		t.Fatalf("named remote %+v %v", g, err)
	}
	if g, err = rs.Open(ctx, r.Dir, "main", "gone"); err != nil || g.Remote != "" {
		t.Fatalf("a missing remote %+v %v", g, err)
	}
	remotes, err := g.Remotes(ctx)
	if err != nil || !slices.Equal(remotes, []string{"origin", "upstream"}) {
		t.Fatalf("remotes %v %v", remotes, err)
	}
}

// A repo path inside a work tree opens at the root, and every read takes
// paths from there.
func TestOpenSubdirectory(t *testing.T) {
	r := gittest.Repo(t)
	r.Write("sub/deep/x.md", "x\n")
	c := r.WriteCommit("docs/a.md", "a\n", "one")
	r.WriteCommit("docs/a.md", "a2\n", "two")
	rs := gitread.New()
	ctx := t.Context()
	for _, dir := range []string{r.Dir + "/sub", r.Dir + "/sub/deep", r.Dir + "/sub/", r.Dir} {
		g, err := rs.Open(ctx, dir, "main", "")
		if err != nil || g.Path != r.Dir {
			t.Fatalf("Open(%s) = %+v, %v; want the root %s", dir, g, err, r.Dir)
		}
		f, err := g.Show(ctx, "main", "docs/a.md")
		if err != nil || f.Text != "a2\n" {
			t.Fatalf("%s: Show %+v %v", dir, f, err)
		}
		blob, err := g.Blob(ctx, "main", "docs/a.md")
		if err != nil || blob != f.Blob {
			t.Fatalf("%s: Blob %q %v", dir, blob, err)
		}
		log, err := g.PathLog(ctx, "docs/a.md", c, 10)
		if err != nil || len(log) != 1 || log[0].Subject != "two" {
			t.Fatalf("%s: PathLog %+v %v", dir, log, err)
		}
		if cur, gone, err := g.Follow(ctx, "sub/deep/x.md", c); err != nil || gone || cur != "sub/deep/x.md" {
			t.Fatalf("%s: Follow %q %v %v", dir, cur, gone, err)
		}
	}
	// diff.relative in the repo's config doesn't make paths relative to
	// the directory opened.
	r.Git("config", "diff.relative", "true")
	r.Rename("sub/deep/x.md", "sub/deep/y.md")
	r.Commit("rename x")
	g, err := rs.Open(ctx, r.Dir+"/sub", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if cur, gone, err := g.Follow(ctx, "sub/deep/x.md", c); err != nil || gone || cur != "sub/deep/y.md" {
		t.Fatalf("Follow under diff.relative: %q %v %v", cur, gone, err)
	}
}

func TestNoGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rs := gitread.New()
	if rs.Available() {
		t.Fatal("git available with an empty PATH")
	}
	if _, err := rs.Open(t.Context(), "/tmp", "main", ""); !errors.Is(err, gitread.ErrNoGit) {
		t.Fatalf("%v", err)
	}
}

func TestShowAndBlob(t *testing.T) {
	r := gittest.Repo(t)
	c := r.WriteCommit("docs/a.md", "# Hello\n", "one")
	r.WriteCommit("docs/a.md", "# Hello again\n", "two")
	_, g := open(t, r)
	ctx := t.Context()

	f, err := g.Show(ctx, "main", "docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if f.Text != "# Hello again\n" || f.Size != 14 || f.TooLarge || f.Blob != r.Git("rev-parse", "main:docs/a.md") {
		t.Fatalf("%+v", f)
	}
	f, err = g.Show(ctx, c, "docs/a.md")
	if err != nil || f.Text != "# Hello\n" {
		t.Fatalf("at %s: %+v %v", c, f, err)
	}
	blob, err := g.Blob(ctx, c, "docs/a.md")
	if err != nil || blob != r.Git("rev-parse", c+":docs/a.md") {
		t.Fatalf("blob %q %v", blob, err)
	}
	for _, path := range []string{"docs/missing.md", "docs"} {
		if _, err := g.Show(ctx, "main", path); !errors.Is(err, gitread.ErrNoFile) {
			t.Errorf("Show %s: %v", path, err)
		}
		if b, err := g.Blob(ctx, "main", path); b != "" || !errors.Is(err, gitread.ErrNoFile) {
			t.Errorf("Blob %s: %q %v", path, b, err)
		}
	}
	if _, err := g.Show(ctx, "nobranch", "docs/a.md"); err == nil {
		t.Error("a missing revision shown")
	}
	for _, bad := range [][2]string{{"main", "../x"}, {"main~1", "docs/a.md"}, {"-x", "docs/a.md"}, {"main", "-x"}} {
		if _, err := g.Show(ctx, bad[0], bad[1]); !errors.Is(err, gitread.ErrInvalid) {
			t.Errorf("Show(%q, %q): %v", bad[0], bad[1], err)
		}
	}
	head, err := g.Head(ctx)
	if err != nil || head != r.Head() {
		t.Fatalf("head %q %v", head, err)
	}
}

func TestShowTooLarge(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("big.md", strings.Repeat("x", 600<<10), "big")
	_, g := open(t, r)
	f, err := g.Show(t.Context(), "main", "big.md")
	if err != nil {
		t.Fatal(err)
	}
	if !f.TooLarge || f.Text != "" || f.Size != 600<<10 || f.Blob == "" {
		t.Fatalf("TooLarge %v, %d bytes of text, size %d, blob %q", f.TooLarge, len(f.Text), f.Size, f.Blob)
	}
	r.WriteCommit("edge.md", strings.Repeat("y", gitread.MaxShow), "edge")
	if f, err = g.Show(t.Context(), "main", "edge.md"); err != nil || f.TooLarge || len(f.Text) != gitread.MaxShow {
		t.Fatalf("a file of exactly MaxShow: %v %d %v", f.TooLarge, len(f.Text), err)
	}
}

func TestFollowRename(t *testing.T) {
	r := gittest.Repo(t)
	r.Write("a.md", "alpha\nbeta\ngamma\n")
	r.Write("keep.md", "kept\n")
	c := r.WriteCommit("d.md", "doomed\n", "start")
	r.Rename("a.md", "b.md")
	r.Commit("rename a to b")
	r.WriteCommit("other.md", "o\n", "unrelated")
	r.Rename("b.md", "docs/c.md")
	r.Commit("move b into docs")
	r.Remove("d.md")
	r.Commit("delete d")
	_, g := open(t, r)
	ctx := t.Context()

	for path, want := range map[string]string{"a.md": "docs/c.md", "keep.md": "keep.md"} {
		cur, gone, err := g.Follow(ctx, path, c)
		if err != nil || gone || cur != want {
			t.Errorf("Follow(%s) = %q, %v, %v; want %s", path, cur, gone, err, want)
		}
	}
	if cur, gone, err := g.Follow(ctx, "d.md", c); err != nil || !gone {
		t.Errorf("Follow(d.md) = %q, %v, %v; want gone", cur, gone, err)
	}
	if _, _, err := g.Follow(ctx, "a.md", "nosuch"); err == nil {
		t.Error("followed from a missing revision")
	}

	log, err := g.PathLog(ctx, "docs/c.md", c, 10)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, cm := range log {
		subjects = append(subjects, cm.Subject)
	}
	if !slices.Equal(subjects, []string{"move b into docs", "rename a to b"}) {
		t.Fatalf("PathLog %v", subjects)
	}
	if log, err = g.PathLog(ctx, "docs/c.md", c, 1); err != nil || len(log) != 1 {
		t.Fatalf("PathLog max 1: %d %v", len(log), err)
	}
	if log, err = g.PathLog(ctx, "docs/c.md", "", 10); err != nil || len(log) != 3 {
		t.Fatalf("PathLog since the start: %d %v", len(log), err)
	}
}

func TestMentions(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "Core: first (VAQ-12)")
	r.WriteCommit("a", "2", "Core: other (VAQ-123)")
	r.WriteCommit("a", "3", "Core: names VAQ-12 bare")
	r.WriteCommit("a", "4", "Core: earlier ticket (VAQ-1)")
	r.WriteCommit("a", "5", "Web: second (VAQ-12, VAQ-13)\n\nBody (VAQ-12)")
	r.WriteCommit("a", "6", "Web: body only\n\nSee (VAQ-12)")
	r.Branch("side")
	r.WriteCommit("a", "7", "Side: not on main (VAQ-12)")
	_, g := open(t, r)

	got, err := g.Mentions(t.Context(), "VAQ-12", 10)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range got {
		subjects = append(subjects, c.Subject)
	}
	if !slices.Equal(subjects, []string{"Web: body only", "Web: second (VAQ-12, VAQ-13)", "Core: first (VAQ-12)"}) {
		t.Fatalf("mentions %v", subjects)
	}
	got, err = g.Mentions(t.Context(), "VAQ-13", 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("(VAQ-12, VAQ-13) is not (VAQ-13): %v %v", got, err)
	}
	// A message naming (KEY) anywhere matches.
	got, _ = g.Mentions(t.Context(), "VAQ-1", 10)
	if len(got) != 1 {
		t.Fatalf("VAQ-1: %v", got)
	}
	c := got[0]
	if c.Author != gittest.AuthorName || len(c.Hash) != 40 || !strings.HasPrefix(c.Hash, c.Short) || c.At.IsZero() || c.At.Location() != time.UTC {
		t.Fatalf("%+v", c)
	}
	for _, bad := range []string{"", "VAQ", "-x", "VAQ-", "(VAQ-1)", "VAQ-1 x"} {
		if _, err := g.Mentions(t.Context(), bad, 10); !errors.Is(err, gitread.ErrInvalid) {
			t.Errorf("Mentions(%q): %v", bad, err)
		}
	}
}

func TestDiffstat(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("base.md", "base\n", "base")
	c1 := r.WriteCommit("a.md", "1\n2\n3\n", "add a")
	r.Write("a.md", "1\ntwo\n3\n")
	r.Write("b.md", "x\ny\n")
	c2 := r.Commit("change a, add b")
	r.Write("bin.dat", "\x00\x01\x02")
	c3 := r.Commit("binary")
	_, g := open(t, r)

	d, err := g.Diffstat(t.Context(), []string{c1, c2, c3})
	if err != nil {
		t.Fatal(err)
	}
	if d.Added != 6 || d.Removed != 1 || !slices.Equal(d.Files, []string{"a.md", "b.md", "bin.dat"}) {
		t.Fatalf("%+v", d)
	}
	if d, err = g.Diffstat(t.Context(), nil); err != nil || d.Added != 0 || len(d.Files) != 0 {
		t.Fatalf("no commits: %+v %v", d, err)
	}
	if _, err = g.Diffstat(t.Context(), []string{"main~1"}); !errors.Is(err, gitread.ErrInvalid) {
		t.Fatalf("bad commit: %v", err)
	}
}

func TestFirstTagContaining(t *testing.T) {
	r := gittest.Repo(t)
	c1 := r.WriteCommit("a", "1", "one")
	r.Tag("v0.1.9")
	c2 := r.WriteCommit("a", "2", "two")
	r.Tag("v0.1.10")
	c3 := r.WriteCommit("a", "3", "three")
	r.Tag("v0.2.0")
	// A repo that lays out listings in columns still gives one tag.
	r.Git("config", "column.ui", "always")
	r.Git("config", "color.ui", "always")
	c4 := r.WriteCommit("a", "4", "four")
	_, g := open(t, r)
	for c, want := range map[string]string{c1: "v0.1.9", c2: "v0.1.10", c3: "v0.2.0", c4: ""} {
		got, err := g.FirstTagContaining(t.Context(), c)
		if err != nil || got != want {
			t.Errorf("FirstTagContaining(%s) = %q, %v; want %q", c[:7], got, err, want)
		}
	}
}

func TestMerged(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Branch("feat")
	r.WriteCommit("b", "1", "feat one")
	r.WriteCommit("b", "2", "feat two")
	r.Checkout("main")
	_, g := open(t, r)
	ctx := t.Context()

	commits, err := g.BranchCommits(ctx, "feat", 10)
	if err != nil || len(commits) != 2 || commits[0].Subject != "feat two" {
		t.Fatalf("branch commits %+v %v", commits, err)
	}
	if merged, err := g.Merged(ctx, "feat"); err != nil || merged {
		t.Fatalf("before the merge: %v %v", merged, err)
	}
	r.Merge("feat", "Merge feat")
	if merged, err := g.Merged(ctx, "feat"); err != nil || !merged {
		t.Fatalf("after the merge: %v %v", merged, err)
	}
	if commits, err = g.BranchCommits(ctx, "feat", 10); err != nil || len(commits) != 0 {
		t.Fatalf("merged branch commits %+v %v", commits, err)
	}
	if _, err := g.Merged(ctx, "nosuch"); err == nil {
		t.Error("a missing branch is neither merged nor not")
	}
	if _, err := g.Merged(ctx, "feat~1"); !errors.Is(err, gitread.ErrInvalid) {
		t.Errorf("bad branch: %v", err)
	}
}

func TestTagExists(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Tag("v1")
	r.Git("tag", "-a", "-m", "annotated", "v2")
	_, g := open(t, r)
	for tag, want := range map[string]bool{"v1": true, "v2": true, "v3": false, "main": false} {
		got, err := g.TagExists(t.Context(), tag)
		if err != nil || got != want {
			t.Errorf("TagExists(%s) = %v, %v", tag, got, err)
		}
	}
}

func TestCompare(t *testing.T) {
	bare := gittest.Bare(t)
	r := gittest.Repo(t)
	r.Git("remote", "add", "origin", bare.Dir)
	r.WriteCommit("a", "1", "one")
	r.Git("push", "-q", "origin", "main")
	r.WriteCommit("a", "2", "two")
	r.WriteCommit("a", "3", "three")
	_, g := open(t, r)
	ctx := t.Context()

	ahead, behind, ok, err := g.Compare(ctx, "main", "origin/main")
	if err != nil || !ok || ahead != 2 || behind != 0 {
		t.Fatalf("ahead %d behind %d ok %v %v", ahead, behind, ok, err)
	}
	if ahead, behind, ok, err = g.Compare(ctx, "main", ""); err != nil || !ok || ahead != 2 || behind != 0 {
		t.Fatalf("the default remote: ahead %d behind %d ok %v %v", ahead, behind, ok, err)
	}
	r.Branch("solo")
	if _, _, ok, err = g.Compare(ctx, "solo", "origin/solo"); err != nil || ok {
		t.Fatalf("never pushed: ok %v %v", ok, err)
	}
	if _, _, _, err = g.Compare(ctx, "main", "origin/main~1"); !errors.Is(err, gitread.ErrInvalid) {
		t.Fatalf("bad ref: %v", err)
	}
}

func TestRemoteURL(t *testing.T) {
	r := gittest.Repo(t)
	r.Git("remote", "add", "origin", "https://u:secret@example.com/o/r.git")
	_, g := open(t, r)
	got, err := g.RemoteURL(t.Context())
	if err != nil || got != "https://example.com/o/r.git" {
		t.Fatalf("%q %v", got, err)
	}
	r2 := gittest.Repo(t)
	_, g2 := open(t, r2)
	if got, err := g2.RemoteURL(t.Context()); err != nil || got != "" {
		t.Fatalf("no remote: %q %v", got, err)
	}
}

func TestLastFetch(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.Clone(t, bare)
	// A fresh clone has no FETCH_HEAD yet.
	_, g := open(t, r)
	if at, ok := g.LastFetch(); ok {
		t.Fatalf("fetched at %v before any fetch", at)
	}
	before := time.Now().Add(-2 * time.Second)
	if err := g.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	at, ok := g.LastFetch()
	if !ok || at.Before(before) {
		t.Fatalf("after a fetch: %v %v", at, ok)
	}
}

func TestFetchUpdatesOnlyRemoteRefs(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a.md", "1\n", "one")
	seed.Git("push", "-q", "origin", "main")

	r := gittest.Clone(t, bare)
	r.Tag("local-only")
	r.Branch("work")
	r.WriteCommit("w.md", "local\n", "local work")
	r.Checkout("main")
	r.Write("a.md", "uncommitted edit\n")
	r.Write("new.md", "untracked\n")

	// The remote moves on: main, a new branch and a tag.
	seed.WriteCommit("a.md", "2\n", "two")
	seed.Tag("v1")
	seed.Git("push", "-q", "origin", "main", "v1")
	seed.Branch("feature")
	seed.WriteCommit("f.md", "f\n", "feature")
	seed.Git("push", "-q", "origin", "feature")

	_, g := open(t, r)
	ctx := t.Context()
	if on, known, err := g.RemoteTag(ctx, "v1"); err != nil || known || on {
		t.Fatalf("before a fetch: on %v known %v %v", on, known, err)
	}

	local := func() string {
		var keep []string
		for line := range strings.SplitSeq(r.Refs(), "\n") {
			if !strings.Contains(line, " refs/remotes/") && !strings.Contains(line, " refs/vaquero/") {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n") + "\n" + r.Git("rev-parse", "HEAD") + "\n" + r.Git("symbolic-ref", "HEAD") + "\n" +
			r.Read("a.md") + r.Read("new.md") + r.Git("status", "--porcelain")
	}
	before := local()
	if !strings.Contains(before, "refs/tags/local-only") || !strings.Contains(before, "refs/heads/work") {
		t.Fatalf("setup: %s", before)
	}

	if err := g.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if after := local(); after != before {
		t.Fatalf("local refs or work tree changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got, want := r.Rev("refs/remotes/origin/main"), seed.Rev("main"); got != want {
		t.Errorf("origin/main %s, want %s", got, want)
	}
	if got, want := r.Rev("refs/remotes/origin/feature"), seed.Rev("feature"); got != want {
		t.Errorf("origin/feature %s, want %s", got, want)
	}
	if got, want := r.Rev("refs/vaquero/remote-tags/origin/v1"), seed.Rev("v1"); got != want {
		t.Errorf("remote tag v1 %s, want %s", got, want)
	}
	if _, err := r.Try("rev-parse", "--verify", "--quiet", "refs/tags/v1"); err == nil {
		t.Error("the remote's tag v1 landed in refs/tags")
	}
	if on, known, err := g.RemoteTag(ctx, "v1"); err != nil || !known || !on {
		t.Fatalf("pushed tag after a fetch: on %v known %v %v", on, known, err)
	}
	if on, known, err := g.RemoteTag(ctx, "local-only"); err != nil || !known || on {
		t.Fatalf("local-only tag after a fetch: on %v known %v %v", on, known, err)
	}
	if _, _, err := g.RemoteTag(ctx, "v1~1"); !errors.Is(err, gitread.ErrInvalid) {
		t.Fatalf("bad tag: %v", err)
	}

	// A tag deleted on the remote leaves Vaquero's namespace on the next fetch.
	seed.Git("push", "-q", "origin", ":refs/tags/v1")
	if err := g.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if on, known, err := g.RemoteTag(ctx, "v1"); err != nil || !known || on {
		t.Fatalf("deleted remote tag: on %v known %v %v", on, known, err)
	}
	if after := local(); after != before {
		t.Fatal("the second fetch changed local refs or the work tree")
	}
}

func TestFetchNeverPrompts(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Git("remote", "add", "origin", "https://127.0.0.1:1/x.git")
	_, g := open(t, r)
	start := time.Now()
	err := g.Fetch(t.Context())
	if err == nil {
		t.Fatal("fetched from nowhere")
	}
	if took := time.Since(start); took > gitread.FetchTimeout {
		t.Fatalf("took %v", took)
	}
	if msg := err.Error(); msg == "" || len(msg) > 300 || strings.Contains(msg, "\n") {
		t.Fatalf("message %q", msg)
	}
}

func TestFetchNeedsRemote(t *testing.T) {
	r := gittest.Repo(t)
	_, g := open(t, r)
	if err := g.Fetch(t.Context()); !errors.Is(err, gitread.ErrNoRemote) {
		t.Fatalf("%v", err)
	}
}

// gate holds every fetch command until release is closed, saying when the
// first one has started.
func gate(rs *gitread.Repos) (started <-chan struct{}, release func()) {
	st, rel := make(chan struct{}), make(chan struct{})
	var once sync.Once
	rs.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args, env []string, limit int) ([]byte, error) {
			if gitread.Subcommand(args) == "fetch" {
				once.Do(func() { close(st) })
				<-rel
			}
			return next(ctx, dir, args, env, limit)
		}
	})
	var o sync.Once
	return st, func() { o.Do(func() { close(rel) }) }
}

func fetchWorld(t *testing.T) (*gitread.Repos, *gitread.Repo) {
	t.Helper()
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.Clone(t, bare)
	return open(t, r)
}

func TestFetchSingleflight(t *testing.T) {
	rs, g := fetchWorld(t)
	rec := gittest.Record(rs)
	started, release := gate(rs)
	defer release()
	errs := make(chan error, 4)
	go func() { errs <- g.Fetch(context.Background()) }()
	<-started
	for range 3 {
		go func() { errs <- g.Fetch(context.Background()) }()
	}
	for g.Waiting() < 3 {
		time.Sleep(time.Millisecond)
	}
	release()
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if n := rec.Count("fetch"); n != 1 {
		t.Fatalf("%d fetches for four callers", n)
	}
	if err := g.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := rec.Count("fetch"); n != 2 {
		t.Fatalf("a later call ran %d fetches in all", n)
	}
}

// A caller that gives up neither fails the fetch for the others nor stops it.
func TestFetchSurvivesACancelledCaller(t *testing.T) {
	rs, g := fetchWorld(t)
	rec := gittest.Record(rs)
	started, release := gate(rs)
	defer release()
	first, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() { firstErr <- g.Fetch(first) }()
	<-started
	second := make(chan error, 1)
	go func() { second <- g.Fetch(context.Background()) }()
	for g.Waiting() < 1 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that gave up got %v", err)
	}
	release()
	if err := <-second; err != nil {
		t.Fatalf("a waiter failed with the first caller's cancel: %v", err)
	}
	if n := rec.Count("fetch"); n != 1 {
		t.Fatalf("%d fetches", n)
	}
	if _, ok := g.LastFetch(); !ok {
		t.Fatal("the fetch didn't finish")
	}
}

func TestLastFetchError(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	missing := t.TempDir() + "/nowhere.git"
	r.Git("remote", "add", "origin", missing)
	_, g := open(t, r)
	if _, ok := g.LastFetchError(); ok {
		t.Fatal("an error before any fetch")
	}
	if err := g.Fetch(t.Context()); err == nil {
		t.Fatal("fetched from nowhere")
	}
	f, ok := g.LastFetchError()
	if !ok || f.Line == "" || strings.Contains(f.Line, "\n") || len([]rune(f.Line)) > 200 || time.Since(f.At) > time.Minute {
		t.Fatalf("%+v %v", f, ok)
	}
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a", "1", "one")
	seed.Git("push", "-q", "origin", "main")
	r.Git("remote", "set-url", "origin", bare.Dir)
	if err := g.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.LastFetchError(); ok {
		t.Fatal("the error stayed after a good fetch")
	}
}

func TestErrorCarriesGitsFirstLine(t *testing.T) {
	gittest.Need(t)
	_, err := gitread.New().Open(t.Context(), t.TempDir(), "main", "")
	var ge *gitread.Error
	if !errors.Is(err, gitread.ErrNotRepo) || !errors.As(err, &ge) {
		t.Fatalf("%T %v", err, err)
	}
	if ge.Line == "" || len([]rune(ge.Line)) > 200 || strings.Contains(ge.Line, "\n") || !strings.Contains(err.Error(), ge.Line) {
		t.Fatalf("line %q in %q", ge.Line, err)
	}
}
