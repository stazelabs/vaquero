package gitread_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// objects lists a repo's object store: every pack and loose object file.
func objects(t *testing.T, gitDir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(gitDir, "objects"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !strings.HasSuffix(p, ".lock") {
			out = append(out, strings.TrimPrefix(p, gitDir))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// partialRepo is a blob:none clone whose history holds blobs it doesn't
// have: a.md's first version, and old.md before it was renamed (with an
// edit, so finding the rename needs both blobs) to new.md.
func partialRepo(t *testing.T) (bare, clone *gittest.R, first string) {
	t.Helper()
	bare = gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	first = seed.WriteCommit("a.md", "first\n", "one")
	seed.WriteCommit("old.md", strings.Repeat("line\n", 20), "old")
	seed.WriteCommit("a.md", "second\n", "two")
	seed.Rename("old.md", "new.md")
	seed.Write("new.md", strings.Repeat("line\n", 20)+"more\n")
	seed.Commit("rename with an edit")
	seed.Git("push", "-q", "origin", "main")
	return bare, gittest.PartialClone(t, bare), first
}

// C2: in a partial clone, no read fetches a missing blob from the
// promisor remote (GIT_NO_LAZY_FETCH=1): it fails, and the object store
// is as it was.
func TestPartialCloneReadsNeverFetch(t *testing.T) {
	bare, r, first := partialRepo(t)
	gitDir := filepath.Join(r.Dir, ".git")
	before, remote := objects(t, gitDir), objects(t, bare.Dir)
	_, g := open(t, r)
	ctx := t.Context()
	if _, err := g.Show(ctx, first, "a.md"); err == nil {
		t.Error("Show of a missing blob succeeded")
	}
	if _, err := g.Renames(ctx, first, r.Head()); err == nil {
		t.Error("Renames over a missing blob succeeded")
	}
	if _, _, err := g.Follow(ctx, "old.md", first); err == nil {
		t.Error("Follow over a missing blob succeeded")
	}
	if _, err := g.Diffstat(ctx, []string{first}); err == nil {
		t.Error("Diffstat of a commit with a missing blob succeeded")
	}
	if after := objects(t, gitDir); !slices.Equal(after, before) {
		t.Fatalf("a read wrote objects:\nbefore %q\nafter  %q", before, after)
	}
	if after := objects(t, bare.Dir); !slices.Equal(after, remote) {
		t.Fatalf("the promisor remote changed: %q", after)
	}
	// main's own blobs are there: reading them works.
	if f, err := g.Show(ctx, "main", "a.md"); err != nil || f.Text != "second\n" {
		t.Fatalf("Show at main: %+v %v", f, err)
	}
}

// Before git 2.44, GIT_NO_LAZY_FETCH does nothing, so Open refuses a
// partial clone, and any repo whose config names a promisor remote (a
// full clone's config can name one too, and git would fetch from it).
func TestPartialCloneNeedsGit244(t *testing.T) {
	_, partial, _ := partialRepo(t)
	full := gittest.Repo(t)
	full.WriteCommit("a.md", "a\n", "one")
	ctx := t.Context()

	old := gitread.NewWithVersion("git version 2.39.5 (Apple Git-154)")
	if !old.Available() {
		t.Fatal("2.39 not available")
	}
	rec := gittest.Record(old)
	if _, err := old.Open(ctx, partial.Dir, "main", ""); !errors.Is(err, gitread.ErrPartialClone) {
		t.Fatalf("partial clone under 2.39: %v", err)
	}
	if _, err := old.Open(ctx, full.Dir, "main", ""); err != nil {
		t.Fatalf("full clone under 2.39: %v", err)
	}
	for _, cfg := range [][]string{
		{"remote.evil.promisor", "true"},
		{"remote.evil.partialclonefilter", "blob:none"},
		{"extensions.partialclone", "evil"},
	} {
		r := gittest.Repo(t)
		r.WriteCommit("a.md", "a\n", "one")
		r.Git("config", "core.repositoryformatversion", "1")
		r.Git("config", cfg[0], cfg[1])
		if _, err := old.Open(ctx, r.Dir, "main", ""); !errors.Is(err, gitread.ErrPartialClone) {
			t.Errorf("%s under 2.39: %v", cfg[0], err)
		}
	}
	off := gittest.Repo(t)
	off.WriteCommit("a.md", "a\n", "one")
	off.Git("config", "remote.origin.promisor", "false")
	if _, err := old.Open(ctx, off.Dir, "main", ""); err != nil {
		t.Errorf("promisor false under 2.39: %v", err)
	}
	if rec.Count("config") == 0 {
		t.Fatal("no config read")
	}

	// From 2.44 the partial clone opens, with no config read.
	cur := gitread.NewWithVersion("git version 2.44.0")
	rec = gittest.Record(cur)
	if _, err := cur.Open(ctx, partial.Dir, "main", ""); err != nil {
		t.Fatalf("partial clone under 2.44: %v", err)
	}
	if n := rec.Count("config"); n != 0 {
		t.Fatalf("%d config reads under 2.44", n)
	}
}

// gitread needs git 2.31 or later: an older git is found but not
// available, and says so.
func TestMinimumGitVersion(t *testing.T) {
	gittest.Need(t)
	for v, ok := range map[string]bool{
		"git version 2.31.0":                 true,
		"git version 2.39.5 (Apple Git-154)": true,
		"git version 2.45.2.windows.1":       true,
		"git version 3.0.0":                  true,
		"git version 2.30.9":                 false,
		"git version 1.9.1":                  false,
		"git version":                        false,
		"nonsense":                           false,
	} {
		rs := gitread.NewWithVersion(v)
		if rs.Available() != ok {
			t.Errorf("%q: Available %v", v, rs.Available())
		}
		want := ""
		if !ok {
			want = "Git 2.31 or later is needed."
		}
		if got := rs.Missing(); got != want {
			t.Errorf("%q: Missing %q", v, got)
		}
		if !ok {
			if _, err := rs.Open(t.Context(), "/tmp", "main", ""); !errors.Is(err, gitread.ErrOldGit) {
				t.Errorf("%q: Open %v", v, err)
			}
		}
	}
	// The real git, read by New, passes (the gate's git is new enough).
	if rs := gitread.New(); !rs.Available() || rs.Missing() != "" {
		t.Fatalf("installed git: %v %q", rs.Available(), rs.Missing())
	}
	t.Setenv("PATH", t.TempDir())
	if got := gitread.New().Missing(); got != "Git isn't installed." {
		t.Fatalf("no git: %q", got)
	}
}

// VAQ-166: in a tree:0 clone (commits, no trees) a read that needs a
// missing object says so, not "no such file" or "no such revision".
func TestTreeLessCloneSaysPartial(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("docs/a.md", "a\n", "one")
	seed.Git("push", "-q", "origin", "main")
	r := gittest.PartialCloneFilter(t, bare, "tree:0", "--no-checkout")
	_, g := open(t, r)
	ctx := t.Context()
	_, errShow := g.Show(ctx, "main", "docs/a.md")
	_, errBlobs := g.Blobs(ctx, r.Head(), []string{"docs/a.md"})
	for name, err := range map[string]error{"Show": errShow, "Blobs": errBlobs} {
		if !errors.Is(err, gitread.ErrPartialRead) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A file that really isn't there, in a clone that has its trees, stays a missing file.
	full := gittest.PartialClone(t, bare)
	_, fg := open(t, full)
	if _, err := fg.Show(ctx, "main", "docs/nope.md"); !errors.Is(err, gitread.ErrNoFile) {
		t.Errorf("blob:none clone, absent file: %v", err)
	}
}

// VAQ-166: a failed git version read is asked again, and isn't called old.
func TestVersionReadRetried(t *testing.T) {
	gittest.Need(t)
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a\n", "one")
	reads, fail := 0, true
	rs := gitread.NewWithVersionFunc(func() string {
		reads++
		if fail {
			return ""
		}
		return "git version 2.45.0"
	})
	if rs.Available() || rs.Missing() != "Couldn't read git's version." {
		t.Fatalf("unreadable: %v %q", rs.Available(), rs.Missing())
	}
	if _, err := rs.Open(t.Context(), r.Dir, "main", ""); !errors.Is(err, gitread.ErrVersionUnreadable) {
		t.Fatalf("open: %v", err)
	}
	fail = false
	if !rs.Available() || rs.Missing() != "" {
		t.Fatalf("after the retry: %v %q", rs.Available(), rs.Missing())
	}
	if _, err := rs.Open(t.Context(), r.Dir, "main", ""); err != nil {
		t.Fatal(err)
	}
	n := reads
	rs.Available()
	if reads != n {
		t.Fatalf("an answered version was read again")
	}
}

// VAQ-193: a file that isn't there, in a full clone, costs no promisor
// check beyond the first for the repo: the answer is kept per repo, so the
// file view's 404 runs one git command, not two.
func TestMissingFileInFullCloneRunsNoConfigRead(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("docs/a.md", "a\n", "one")
	rs := gitread.New()
	rec := gittest.Record(rs)
	g, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := g.Show(t.Context(), "main", "docs/nope.md"); !errors.Is(err, gitread.ErrNoFile) {
			t.Fatalf("show: %v", err)
		}
	}
	if n := rec.Count("config"); n > 1 {
		t.Fatalf("%d config reads for 3 missing files", n)
	}
	// A second Open of the same repo doesn't ask again either.
	g2, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	before := rec.Count("config")
	if _, err := g2.Show(t.Context(), "main", "docs/nope.md"); !errors.Is(err, gitread.ErrNoFile) {
		t.Fatalf("show: %v", err)
	}
	if rec.Count("config") != before {
		t.Fatal("a reopened repo asked again")
	}
}
