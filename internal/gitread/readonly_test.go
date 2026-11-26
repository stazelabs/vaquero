package gitread_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// The runner refuses every subcommand but the read-only ones, and every
// form of them that writes; fetch runs only from Fetch.
func TestGitreadCommandsAreReadOnly(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a\n", "one")
	r.Tag("v1")
	r.Git("remote", "add", "origin", "https://example.com/o/r.git")
	rs := gitread.New()
	rec := gittest.Record(rs)
	ctx := t.Context()
	before := r.Refs()

	if !slices.Equal(gitread.Allowed(), []string{"rev-parse", "show", "cat-file", "log", "merge-base", "tag", "rev-list", "for-each-ref", "remote", "diff", "fetch"}) {
		t.Fatalf("allow-list %v", gitread.Allowed())
	}
	refused := [][]string{
		{"fetch", "origin"},
		{"push", "origin", "main"},
		{"config", "core.fsmonitor", "x"},
		{"commit", "-m", "x"},
		{"checkout", "-b", "x"},
		{"update-ref", "refs/heads/x", "HEAD"},
		{"gc"},
		{"pull"},
		{"clone", "x"},
		{"ls-remote", "origin"},
		{"archive", "HEAD"},
		{"submodule", "update"},
		{"tag", "v2"},
		{"tag", "-d", "v1"},
		{"tag", "-a", "-m", "x", "v2"},
		{"tag", "-f", "v1"},
		{"remote", "add", "x", "y"},
		{"remote", "set-url", "origin", "y"},
		{"remote", "remove", "origin"},
		{"remote", "prune", "origin"},
		{"remote", "update"},
		{"remote", "-v"},
		{"log", "--output=/tmp/x"},
		{"log", "--output", "/tmp/x"},
		{"diff", "--output=x", "HEAD"},
		{"diff", "--no-index", "/etc/passwd", "/dev/null"},
		{"show", "--output=x"},
		{"rev-list", "--output=x", "HEAD"},
		{"log", "-o", "x"},
		{"cat-file", "--filters", "HEAD:a.md"},
		{"cat-file", "--textconv", "HEAD:a.md"},
		{"show", "--textconv", "HEAD:a.md"},
		{"show", "--ext-diff", "HEAD"},
		{"-c", "core.pager=x"},
		{"--exec-path=/tmp"},
		{""},
	}
	for _, args := range refused {
		if _, err := gitread.RunCommand(rs, ctx, r.Dir, args[0], args[1:]...); !errors.Is(err, gitread.ErrRefused) {
			t.Errorf("git %q: %v", args, err)
		}
	}
	// A whole argument list without the guards is refused too.
	if _, err := gitread.RunRaw(rs, ctx, r.Dir, []string{"-C", r.Dir, "log"}); !errors.Is(err, gitread.ErrRefused) {
		t.Errorf("unguarded log: %v", err)
	}
	// Guards first, then a second -c slipped in before the subcommand.
	argv := append(gitread.Prefix(rs, r.Dir), "-c", "core.pager=x", "log")
	if _, err := gitread.RunRaw(rs, ctx, r.Dir, argv); !errors.Is(err, gitread.ErrRefused) {
		t.Errorf("extra -c: %v", err)
	}
	if n := len(rec.Calls()); n != 0 {
		t.Fatalf("%d refused commands reached git: %q", n, rec.Calls())
	}

	for _, args := range [][]string{
		{"rev-parse", "--is-inside-work-tree"},
		{"tag", "--list"},
		{"remote"},
		{"remote", "get-url", "origin"},
		{"log", "-1", "--format=%H"},
	} {
		if _, err := gitread.RunCommand(rs, ctx, r.Dir, args[0], args[1:]...); err != nil {
			t.Errorf("git %q: %v", args, err)
		}
	}
	if after := r.Refs(); after != before {
		t.Fatalf("refs changed:\n%s\n%s", before, after)
	}
}

// O-R6: Fetch is the only network command; no other method runs fetch.
func TestFetchIsTheOnlyNetwork(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	c := seed.WriteCommit("a.md", "a\n", "one (VAQ-1)")
	seed.Tag("v1")
	seed.Git("push", "-q", "origin", "main", "v1")
	r := gittest.Clone(t, bare)
	rs := gitread.New()
	rec := gittest.Record(rs)
	ctx := t.Context()
	g, err := rs.Open(ctx, r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	g.Head(ctx)
	g.Show(ctx, "main", "a.md")
	g.Blob(ctx, "main", "a.md")
	g.Follow(ctx, "a.md", c)
	g.PathLog(ctx, "a.md", "", 5)
	g.Mentions(ctx, "VAQ-1", 5)
	g.BranchCommits(ctx, "main", 5)
	g.Merged(ctx, "main")
	g.TagExists(ctx, "v1")
	g.Diffstat(ctx, []string{c})
	g.FirstTagContaining(ctx, c)
	g.Compare(ctx, "main", "")
	g.RemoteTag(ctx, "v1")
	g.Remotes(ctx)
	g.RemoteURL(ctx)
	g.LastFetch()
	if len(rec.Calls()) == 0 {
		t.Fatal("nothing recorded")
	}
	for _, call := range rec.Calls() {
		if sub := gitread.Subcommand(call); sub == "fetch" || sub == "ls-remote" || sub == "pull" || sub == "push" {
			t.Fatalf("a read ran git %s", sub)
		}
	}
	if err := g.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if n := rec.Count("fetch"); n != 1 {
		t.Fatalf("Fetch ran %d fetches", n)
	}
}
