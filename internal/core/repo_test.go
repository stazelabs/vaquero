package core_test

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
)

// repoFixture is a core with git, a project, and a repo with a commit on
// main and a remote named upstream.
func repoFixture(t *testing.T) (*core.Core, fixture, *gittest.R) {
	t.Helper()
	c, d, f := newFixture(t)
	c.Git = gitread.New()
	// Git never runs while the writer's one connection is in a transaction.
	c.Git.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args []string, env []string, limit int) ([]byte, error) {
			if n := d.W.Stats().InUse; n != 0 {
				t.Errorf("git %v ran with %d writer connection(s) in use", args, n)
			}
			return next(ctx, dir, args, env, limit)
		}
	})
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "first")
	r.Git("remote", "add", "upstream", "https://user:secret@example.com/x.git")
	return c, f, r
}

func TestUpdateProjectRepoPath(t *testing.T) {
	c, f, r := repoFixture(t)
	set := func(path, main, remote string) error {
		_, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{RepoPath: &path, MainBranch: &main, Remote: &remote})
		return err
	}
	for _, tc := range []struct{ path, main, remote, want string }{
		{"relative/dir", "main", "", "Use an absolute path."},
		{t.TempDir(), "main", "", "That isn't a git work tree."},
		{r.Dir, "trunk", "", "The repo has no branch trunk."},
		{r.Dir, "main", "nope", "The repo has no remote nope."},
	} {
		if got := invalidMsg(set(tc.path, tc.main, tc.remote)); got != tc.want {
			t.Errorf("%q %q %q: %q, want %q", tc.path, tc.main, tc.remote, got, tc.want)
		}
	}
	before := lastEventSeq(t, c)
	if err := set(r.Dir, "main", "upstream"); err != nil {
		t.Fatal(err)
	}
	ch, _ := lastPayload(t, c)["changes"].(map[string]any)
	if _, ok := ch["repo_path"]; !ok || ch["remote"] == nil || lastEventSeq(t, c) != before+1 {
		t.Fatalf("changes %v", ch)
	}
	// Saving the same settings writes nothing.
	if err := set(r.Dir, "main", "upstream"); err != nil || lastEventSeq(t, c) != before+1 {
		t.Fatalf("no-op wrote: %v", err)
	}
	// A nil Git refuses a path.
	c.Git = nil
	if got := invalidMsg(set(r.Dir+"/", "main", "")); got != "Git isn't installed, so repository features are off." {
		t.Fatalf("%q", got)
	}
	// Git older than 2.31 refuses one too.
	c.Git = gitread.NewWithVersion("git version 2.30.2")
	if got := invalidMsg(set(r.Dir+"/", "main", "")); got != "Git 2.31 or later is needed, so repository features are off." {
		t.Fatalf("%q", got)
	}
	// A partial clone under a git older than 2.44 is refused (C2).
	c.Git = gitread.NewWithVersion("git version 2.40.0")
	r.Git("config", "remote.origin.promisor", "true")
	if got := invalidMsg(set(r.Dir, "main", "")); got != "Partial clones need git 2.44 or later." {
		t.Fatalf("%q", got)
	}
	r.Git("config", "--unset", "remote.origin.promisor")
	// Clearing the path needs no git.
	if err := set("", "main", ""); err != nil {
		t.Fatal(err)
	}
}

func lastEventSeq(t *testing.T, c *core.Core) int64 {
	t.Helper()
	var n int64
	if err := c.DB.QueryRow(`SELECT max(seq) FROM events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRepoSettingsOwnerOnly(t *testing.T) {
	c, f, r := repoFixture(t)
	a := agent(t, c, f.Owner, "builder")
	const refusal = "Only the owner changes the repo settings."
	path, main, remote, five := r.Dir, "main", "upstream", int64(15)
	for name, p := range map[string]core.ProjectPatch{
		"path":   {RepoPath: &path},
		"main":   {MainBranch: new("dev")},
		"remote": {Remote: &remote},
	} {
		if got := invalidMsg(update(c, a.ID, f.Project.ID, p)); got != refusal {
			t.Errorf("%s: %q", name, got)
		}
	}
	_ = main
	if err := update(c, f.Owner.ID, f.Project.ID, core.ProjectPatch{RepoPath: &path}); err != nil {
		t.Fatal(err)
	}
	if err := update(c, a.ID, f.Project.ID, core.ProjectPatch{FetchMinutes: &five}); err != nil {
		t.Fatalf("an agent's fetch_minutes: %v", err)
	}
	// An agent saving the settings as they are changes nothing and is not refused.
	if err := update(c, a.ID, f.Project.ID, core.ProjectPatch{RepoPath: &path}); err != nil {
		t.Fatalf("unchanged: %v", err)
	}
}

func update(c *core.Core, by, id string, p core.ProjectPatch) error {
	_, err := c.UpdateProject(bg, by, id, p)
	return err
}

func TestFetchMinutesBounds(t *testing.T) {
	c, _, f := newFixture(t)
	for _, n := range []int64{0, 5, 1440} {
		if err := update(c, f.Owner.ID, f.Project.ID, core.ProjectPatch{FetchMinutes: &n}); err != nil {
			t.Errorf("%d: %v", n, err)
		}
	}
	for _, n := range []int64{4, 1441, -1} {
		if got := invalidMsg(update(c, f.Owner.ID, f.Project.ID, core.ProjectPatch{FetchMinutes: &n})); got != "Fetch every 5 to 1440 minutes, or 0 for never." {
			t.Errorf("%d: %q", n, got)
		}
	}
}
