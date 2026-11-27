// Package gittest makes real git repositories for tests, and records the
// git commands a gitread.Repos runs.
//
// Repo runs git init in t.TempDir() with a fixed author, committer and
// clock (each commit a minute after the last, from FixedTime), and with no
// global or system config, so its history is the same on every machine. A
// test skips when git isn't installed, unless VQ_GIT_REQUIRE is set (the
// gate workflow sets it), in which case it fails.
//
// This package is test support, inside internal/gitread so that
// TestOnlyGitreadRunsGit holds: nothing outside gitread runs git.
package gittest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/gitread"
)

// FixedTime is the first commit's author and committer date.
var FixedTime = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// Author is every commit's author and committer.
const (
	AuthorName  = "Test Author"
	AuthorEmail = "author@example.com"
)

// Need skips t when git isn't installed, or fails it when VQ_GIT_REQUIRE
// is set.
func Need(t testing.TB) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		if os.Getenv("VQ_GIT_REQUIRE") != "" {
			t.Fatalf("git is required (VQ_GIT_REQUIRE) but not found: %v", err)
		}
		t.Skip("git not installed")
	}
	return git
}

// R is a test repository.
type R struct {
	t    testing.TB
	git  string
	Dir  string
	tick int
	bare bool
}

// Repo makes a work tree with branch main and no commits.
func Repo(t testing.TB) *R {
	t.Helper()
	r := &R{t: t, git: Need(t), Dir: t.TempDir()}
	r.Git("init", "-q", "-b", "main")
	return r
}

// Bare makes a bare repository, to serve as a remote.
func Bare(t testing.TB) *R {
	t.Helper()
	r := &R{t: t, git: Need(t), Dir: t.TempDir(), bare: true}
	r.Git("init", "-q", "--bare", "-b", "main")
	return r
}

// Clone clones remote into a new work tree, with remote as origin.
func Clone(t testing.TB, remote *R) *R {
	t.Helper()
	r := &R{t: t, git: Need(t), Dir: t.TempDir()}
	cmd := exec.Command(r.git, "clone", "-q", remote.Dir, r.Dir)
	cmd.Env = r.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	r.tick = remote.tick + 100
	return r
}

// PartialClone clones remote with --filter=blob:none (over file://, which
// a local clone needs to honour the filter; remote is set to serve
// filters): a work tree with main's blobs and none of history's, whose
// origin is its promisor remote.
func PartialClone(t testing.TB, remote *R) *R {
	t.Helper()
	return PartialCloneFilter(t, remote, "blob:none")
}

// PartialCloneFilter is PartialClone with another filter, such as tree:0
// (commits only: no tree, so no file at all; give --no-checkout in extra so
// the checkout doesn't fetch main's trees).
func PartialCloneFilter(t testing.TB, remote *R, filter string, extra ...string) *R {
	t.Helper()
	remote.Git("config", "uploadpack.allowFilter", "true")
	r := &R{t: t, git: Need(t), Dir: t.TempDir()}
	args := append([]string{"clone", "-q", "--filter=" + filter}, extra...)
	cmd := exec.Command(r.git, append(args, "file://"+remote.Dir, r.Dir)...)
	cmd.Env = r.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone --filter: %v: %s", err, out)
	}
	r.tick = remote.tick + 100
	return r
}

// env is a fixed environment: no global or system config, no prompts, the
// fixed author and the commit clock.
func (r *R) env() []string {
	// git's internal date format: Unix seconds and a zone.
	at := fmt.Sprintf("%d +0000", FixedTime.Add(time.Duration(r.tick)*time.Minute).Unix())
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + r.Dir,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=" + AuthorName, "GIT_AUTHOR_EMAIL=" + AuthorEmail, "GIT_AUTHOR_DATE=" + at,
		"GIT_COMMITTER_NAME=" + AuthorName, "GIT_COMMITTER_EMAIL=" + AuthorEmail, "GIT_COMMITTER_DATE=" + at,
		"LC_ALL=C",
	}
	return env
}

// Git runs git in the repository, failing the test on an error, and
// returns its output with the trailing newline trimmed.
func (r *R) Git(args ...string) string {
	r.t.Helper()
	out, err := r.Try(args...)
	if err != nil {
		r.t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// Try runs git in the repository and returns its output and any error,
// with stderr in the error.
func (r *R) Try(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command(r.git, append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = r.env()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Write writes a file in the work tree, making its directories.
func (r *R) Write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// Read reads a file in the work tree.
func (r *R) Read(path string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Dir, filepath.FromSlash(path)))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

// Commit stages everything and commits it with msg, returning the commit's
// hash.
func (r *R) Commit(msg string) string {
	r.t.Helper()
	r.tick++
	r.Git("add", "-A")
	r.Git("commit", "-q", "--allow-empty", "--no-verify", "-m", msg)
	return r.Head()
}

// WriteCommit writes one file and commits it.
func (r *R) WriteCommit(path, content, msg string) string {
	r.t.Helper()
	r.Write(path, content)
	return r.Commit(msg)
}

// Rename moves a tracked file (git mv) without committing.
func (r *R) Rename(from, to string) {
	r.t.Helper()
	if dir := filepath.Dir(filepath.Join(r.Dir, filepath.FromSlash(to))); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			r.t.Fatal(err)
		}
	}
	r.Git("mv", from, to)
}

// Remove deletes a tracked file (git rm) without committing.
func (r *R) Remove(path string) {
	r.t.Helper()
	r.Git("rm", "-q", path)
}

// Tag makes a lightweight tag at HEAD.
func (r *R) Tag(name string) {
	r.t.Helper()
	r.Git("tag", name)
}

// Branch makes a branch at HEAD and checks it out.
func (r *R) Branch(name string) {
	r.t.Helper()
	r.Git("checkout", "-q", "-b", name)
}

// Checkout checks out an existing branch.
func (r *R) Checkout(name string) {
	r.t.Helper()
	r.Git("checkout", "-q", name)
}

// Merge merges branch into the checked-out branch with a merge commit.
func (r *R) Merge(branch, msg string) string {
	r.t.Helper()
	r.tick++
	r.Git("merge", "-q", "--no-ff", "--no-verify", "-m", msg, branch)
	return r.Head()
}

// Head is the checked-out commit's hash.
func (r *R) Head() string {
	r.t.Helper()
	return r.Git("rev-parse", "HEAD")
}

// Rev is a revision's commit hash.
func (r *R) Rev(rev string) string {
	r.t.Helper()
	return r.Git("rev-parse", "--verify", rev+"^{commit}")
}

// Refs lists every ref with its object, one "object refname" line each.
func (r *R) Refs() string {
	r.t.Helper()
	return r.Git("for-each-ref", "--format=%(objectname) %(refname)")
}

// Recorder records every git command a Repos runs, through Repos.Wrap: the
// real git still runs.
type Recorder struct {
	mu    sync.Mutex
	calls [][]string
}

// Record wraps r so every command it runs is recorded.
func Record(r *gitread.Repos) *Recorder {
	rec := &Recorder{}
	r.Wrap(func(next gitread.RunFunc) gitread.RunFunc {
		return func(ctx context.Context, dir string, args, env []string, limit int) ([]byte, error) {
			rec.mu.Lock()
			rec.calls = append(rec.calls, slices.Clone(args))
			rec.mu.Unlock()
			return next(ctx, dir, args, env, limit)
		}
	})
	return rec
}

// Calls is every command's arguments (after "git"), in order.
func (rec *Recorder) Calls() [][]string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([][]string, len(rec.calls))
	for i, c := range rec.calls {
		out[i] = slices.Clone(c)
	}
	return out
}

// Count is how many commands ran subcommand sub.
func (rec *Recorder) Count(sub string) int {
	n := 0
	for _, c := range rec.Calls() {
		if gitread.Subcommand(c) == sub {
			n++
		}
	}
	return n
}

// Reset forgets the calls recorded so far.
func (rec *Recorder) Reset() {
	rec.mu.Lock()
	rec.calls = nil
	rec.mu.Unlock()
}
