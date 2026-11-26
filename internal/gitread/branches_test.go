package gitread_test

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

func TestBranches(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Branch("zeta")
	r.Branch("feature/x")
	r.Checkout("main")
	r.Tag("zeta-tag")
	_, g := open(t, r)
	got, err := g.Branches(t.Context(), 50)
	if err != nil || !slices.Equal(got, []string{"feature/x", "main", "zeta"}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := g.Branches(t.Context(), 2); err != nil || !slices.Equal(got, []string{"feature/x", "main"}) {
		t.Fatalf("capped: %v %v", got, err)
	}
}

// A revision the repo doesn't have is ErrNoRev, apart from a missing file.
func TestShowNoRevision(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "one")
	_, g := open(t, r)
	if _, err := g.Show(t.Context(), "nobranch", "a.md"); !errors.Is(err, gitread.ErrNoRev) {
		t.Fatalf("%v", err)
	}
	if _, err := g.Show(t.Context(), "main", "b.md"); !errors.Is(err, gitread.ErrNoFile) || errors.Is(err, gitread.ErrNoRev) {
		t.Fatalf("%v", err)
	}
}

// Commit subjects and author names are untrusted one-line text: C0 and C1
// control characters never leave gitread (M5: no terminal escapes).
func TestCommitTextHasNoControlCharacters(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	r.Git("commit", "-q", "--allow-empty", "-m", "Copy \x1b]52;c;aGk=\x07this\ttab (VAQ-1)", "--author=Ev\u009bil\x1b[2J <e@example.com>")
	r.Git("commit", "-q", "--allow-empty", "-m", "Raw \x9b byte (VAQ-1)")
	_, g := open(t, r)
	cs, err := g.Mentions(t.Context(), "VAQ-1", 10)
	if err != nil || len(cs) != 2 {
		t.Fatalf("%+v %v", cs, err)
	}
	if cs[1].Subject != "Copy ]52;c;aGk=thistab (VAQ-1)" || cs[1].Author != "Evil[2J" {
		t.Fatalf("%q %q", cs[1].Subject, cs[1].Author)
	}
	for _, c := range cs {
		for _, s := range []string{c.Subject, c.Author} {
			for _, r := range s {
				if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
					t.Fatalf("control %U in %q", r, s)
				}
			}
		}
	}
}

// Branches reads no more refs than it needs (M2).
func TestBranchesReadsOnlyWhatItNeeds(t *testing.T) {
	r := gittest.Repo(t)
	r.WriteCommit("a", "1", "one")
	for _, b := range []string{"b1", "b2", "b3", "b4", "b5"} {
		r.Git("branch", b)
	}
	rs, g := open(t, r)
	rec := gittest.Record(rs)
	if got, err := g.Branches(t.Context(), 2); err != nil || !slices.Equal(got, []string{"b1", "b2"}) {
		t.Fatalf("%v %v", got, err)
	}
	if calls := rec.Calls(); len(calls) != 1 || !slices.ContainsFunc(calls[0], func(a string) bool { return strings.HasPrefix(a, "--count=") }) {
		t.Fatalf("%v", calls)
	}
}

// Blobs reads many files' blob ids in one ls-tree, leaving out what isn't
// a file there.
func TestBlobs(t *testing.T) {
	r := gittest.Repo(t)
	r.Write("docs/a.md", "a\n")
	r.Write("docs/b c.md", "b\n")
	r.Write("*.md", "star\n")
	head := r.Commit("docs")
	rs := gitread.New()
	rec := gittest.Record(rs)
	g, err := rs.Open(t.Context(), r.Dir, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	rec.Reset()
	got, err := g.Blobs(t.Context(), head, []string{"docs/a.md", "docs/b c.md", "docs", "nope.md", "docs/a.md", "*.md"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"docs/a.md": r.Git("rev-parse", "main:docs/a.md"), "docs/b c.md": r.Git("rev-parse", "main:docs/b c.md"), "*.md": r.Git("rev-parse", "main:*.md")}
	if !maps.Equal(got, want) {
		t.Fatalf("Blobs %v, want %v", got, want)
	}
	if n := len(rec.Calls()); n != 1 {
		t.Fatalf("%d commands", n)
	}
	if _, err := g.Blobs(t.Context(), "main", []string{"a.md"}); !errors.Is(err, gitread.ErrInvalid) {
		t.Fatalf("a branch name: %v", err)
	}
	if _, err := g.Blobs(t.Context(), head, []string{"../a.md"}); !errors.Is(err, gitread.ErrInvalid) {
		t.Fatalf("a bad path: %v", err)
	}
	if _, err := g.Blobs(t.Context(), strings.Repeat("0", 40), []string{"a.md"}); !errors.Is(err, gitread.ErrNoRev) {
		t.Fatalf("a missing commit: %v", err)
	}
}
