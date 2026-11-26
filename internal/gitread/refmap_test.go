package gitread_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// C1: the repo's own remote.<name>.fetch never applies. A mirror config
// (+refs/*:refs/*, as git remote add --mirror=fetch writes) or one that
// maps the remote's main onto a local branch would otherwise be forced
// onto local branches and tags by every fetch (git's opportunistic
// remote-tracking update); --refmap= leaves only Vaquero's two refspecs.
func TestFetchIgnoresTheReposFetchRefspecs(t *testing.T) {
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("a.md", "1\n", "one")
	seed.Tag("v1")
	seed.Branch("feature")
	seed.WriteCommit("f.md", "remote\n", "remote feature")
	seed.Checkout("main")
	seed.Git("push", "-q", "origin", "main", "feature", "v1")

	r := gittest.Clone(t, bare)
	r.Git("config", "--replace-all", "remote.origin.fetch", "+refs/*:refs/*")
	r.Git("config", "--add", "remote.origin.fetch", "+refs/heads/main:refs/heads/keep")
	// Local work the remote doesn't have, on branches and a tag the remote
	// also has.
	r.Branch("feature")
	r.WriteCommit("f.md", "local\n", "unpushed feature")
	r.Branch("keep")
	r.WriteCommit("k.md", "keep\n", "unpushed keep")
	r.Checkout("main")
	r.WriteCommit("m.md", "local\n", "unpushed main")
	r.Git("tag", "-f", "v1")
	r.Branch("work") // main isn't checked out, so git could move it

	seed.WriteCommit("a.md", "2\n", "two")
	seed.Git("tag", "-f", "v1")
	seed.Git("push", "-q", "-f", "origin", "main", "v1")

	local := func() string {
		var keep []string
		for line := range strings.SplitSeq(r.Refs(), "\n") {
			if !strings.Contains(line, " refs/remotes/origin/") && !strings.Contains(line, " refs/vaquero/remote-tags/origin/") {
				keep = append(keep, line)
			}
		}
		return strings.Join(keep, "\n")
	}
	before := local()
	_, g := open(t, r)
	if err := g.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after := local(); after != before {
		t.Fatalf("refs outside Vaquero's view changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got, want := r.Rev("refs/remotes/origin/main"), seed.Rev("main"); got != want {
		t.Errorf("origin/main %s, want %s", got, want)
	}
	if got, want := r.Rev("refs/vaquero/remote-tags/origin/v1"), seed.Rev("v1"); got != want {
		t.Errorf("remote tag v1 %s, want %s", got, want)
	}
}
