package gitread_test

import (
	"errors"
	"slices"
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
