package handlers_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// fetchWorld is gitWorld with a clone of a bare remote as the repo.
func fetchWorld(t *testing.T) (*webtest.Env, *webtest.Client, string, *gittest.R, *gittest.R) {
	t.Helper()
	e, c, tok := apiWorld(t)
	bare := gittest.Bare(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("README.md", "# Readme\n", "first")
	seed.Git("push", "-q", "origin", "main")
	repo := gittest.Clone(t, bare)
	setRepo(t, e, repo.Dir)
	return e, c, tok, repo, bare
}

func TestFetchNowAnswers(t *testing.T) {
	e, c, tok, _, _ := fetchWorld(t)
	rec := gittest.Record(e.Deps.Git)
	var out api.FetchResult
	r := c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	decode(t, r.Body, &out)
	if r.Status != 200 || out.LastFetchAt == nil || out.Error != "" || rec.Count("fetch") != 1 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var st api.RepoStatus
	decode(t, c.API("GET", "/api/v1/projects/VAQ/repo/status", tok, nil).Body, &st)
	if st.LastFetchAt == nil || st.LastFetchError != "" {
		t.Fatalf("%+v", st)
	}
}

func TestFetchNowNeedsARepo(t *testing.T) {
	e, c, tok := apiWorld(t)
	r := c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	if r.Status != 409 || !strings.Contains(r.Body, `"code": "no_repo"`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	repo := gittest.Repo(t)
	repo.WriteCommit("a", "1", "one")
	setRepo(t, e, repo.Dir)
	r = c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	if r.Status != 409 || !strings.Contains(r.Body, `"code": "no_remote"`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	e.Deps.Git = nil
	e.Rebuild()
	r = c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	if r.Status != 409 || !strings.Contains(r.Body, `"code": "no_git"`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// Two requests at once run one git fetch.
func TestFetchNowSingleflight(t *testing.T) {
	e, c, tok, _, _ := fetchWorld(t)
	rec := gittest.Record(e.Deps.Git)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	e.Deps.Git.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args, env []string, limit int) ([]byte, error) {
			if gitread.Subcommand(args) == "fetch" {
				once.Do(func() { close(started) })
				<-release
			}
			return next(ctx, dir, args, env, limit)
		}
	})
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Go(func() { ; codes[0] = c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil).Status })
	<-started
	wg.Go(func() { ; codes[1] = c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil).Status })
	// The second request reaches the shared fetch before the first ends.
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	g, err := e.Deps.Git.Open(bg, p.RepoPath, p.MainBranch, p.Remote)
	if err != nil {
		t.Fatal(err)
	}
	for g.Waiting() < 1 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if codes[0] != 200 || codes[1] != 200 || rec.Count("fetch") != 1 {
		t.Fatalf("%v, %d fetches", codes, rec.Count("fetch"))
	}
}

func TestFetchFailureShown(t *testing.T) {
	e, c, tok := apiWorld(t)
	repo := gittest.Repo(t)
	repo.WriteCommit("README.md", "# Readme\n", "first")
	repo.Git("remote", "add", "origin", t.TempDir()+"/nowhere.git")
	setRepo(t, e, repo.Dir)

	var out api.FetchResult
	r := c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	decode(t, r.Body, &out)
	if r.Status != 200 || out.Error == "" || strings.Contains(out.Error, "\n") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var st api.RepoStatus
	decode(t, c.API("GET", "/api/v1/projects/VAQ/repo/status", tok, nil).Body, &st)
	if st.LastFetchError != out.Error || st.LastFetchErrorAt == nil {
		t.Fatalf("%+v", st)
	}
	var p api.Project
	decode(t, c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body, &p)
	if p.Repo == nil || p.Repo.LastFetchError != out.Error {
		t.Fatalf("%+v", p.Repo)
	}
	// The settings page says why, and reads keep working.
	body := c.Get("/p/VAQ/settings").Body
	contains(t, body, "Last fetch failed", "nowhere.git")
	if r := c.API("GET", "/api/v1/projects/VAQ/files/README.md", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.Get("/t/VAQ-1"); r.Status != 200 {
		t.Fatal(r.Status)
	}
}

func TestFetchNowFromTheSettingsPage(t *testing.T) {
	_, c, _, repo, bare := fetchWorld(t)
	seed := gittest.Clone(t, bare)
	seed.WriteCommit("b", "1", "two")
	seed.Git("push", "-q", "origin", "main")
	r := c.Post("/p/VAQ/repo/fetch", url.Values{})
	if r.Status != 303 && r.Status != 302 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got := repo.Git("rev-parse", "origin/main"); got != seed.Rev("main") {
		t.Fatalf("origin/main %q, want %q", got, seed.Rev("main"))
	}
	contains(t, c.Get("/p/VAQ/settings").Body, "Fetch now", "Last fetched")
}

func TestFetchWritesNoEvent(t *testing.T) {
	e, c, tok, _, _ := fetchWorld(t)
	before := eventCount(t, e)
	c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok, nil)
	c.Post("/p/VAQ/repo/fetch", url.Values{})
	if after := eventCount(t, e); after != before {
		t.Fatalf("events %d -> %d", before, after)
	}
}

// A token written into the remote's URL appears on no surface.
func TestFetchFailureHidesTheRemotesCredential(t *testing.T) {
	const tok = "SENTINELTOKEN123"
	e, c, tok2 := apiWorld(t)
	repo := gittest.Repo(t)
	repo.WriteCommit("README.md", "# Readme\n", "first")
	repo.Git("remote", "add", "origin", "https://user:"+tok+"@127.0.0.1:1/x.git")
	setRepo(t, e, repo.Dir)
	var logs strings.Builder
	e.Deps.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e.Rebuild()

	r := c.API("POST", "/api/v1/projects/VAQ/repo/fetch", tok2, nil)
	if r.Status != 200 || !strings.Contains(r.Body, `"error"`) || strings.Contains(r.Body, tok) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	for _, path := range []string{"/api/v1/projects/VAQ/repo/status", "/api/v1/projects/VAQ"} {
		if b := c.API("GET", path, tok2, nil).Body; strings.Contains(b, tok) {
			t.Errorf("%s shows the token", path)
		}
	}
	post := c.Post("/p/VAQ/repo/fetch", url.Values{})
	if strings.Contains(post.Body, tok) || strings.Contains(fmt.Sprint(post.Header), tok) {
		t.Error("the toast carries the token")
	}
	page := c.Follow(post)
	if strings.Contains(page.Body, tok) || !strings.Contains(page.Body, "Last fetch failed") {
		t.Errorf("settings page: %d", page.Status)
	}
	if strings.Contains(c.Get("/p/VAQ/settings").Body, tok) || strings.Contains(logs.String(), tok) {
		t.Error("the token is on the page or in the log")
	}
}
