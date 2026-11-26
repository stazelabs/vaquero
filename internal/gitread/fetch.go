package gitread

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Fetch updates the remote's branches and tags in Vaquero's view, the one
// command that touches the network and the one that writes:
//
//	git fetch --prune --no-tags {remote} +refs/heads/*:refs/remotes/{remote}/* +refs/tags/*:refs/vaquero/remote-tags/{remote}/*
//
// with the guards every command has, plus --no-recurse-submodules,
// --no-write-commit-graph, --no-auto-maintenance and
// --upload-pack=git-upload-pack (a repo's remote.*.uploadpack can't pick the
// program a local remote runs). Local branches, local tags and the work tree
// are never touched: the remote's tags land in refs/vaquero/remote-tags/,
// so RemoteTag is exact as of the last fetch Vaquero ran. The remote's URL
// must be one fetchable accepts.
//
// Credentials: the repo's own credential.helper never runs (every command
// resets the list with -c credential.helper=). Fetch then re-adds the
// user's helpers, credential.helper and every credential.<url>.helper (so
// gh auth setup-git's credential.https://github.com.helper works), read
// with git config --system and --global -z --get-regexp
// '^credential\.(.+\.)?helper$' (from /, never --local, so no repo config
// or include is read), as -c key=value after the reset, in git's own
// order (system, then global, each in file order; an empty value resets
// the list, as in git); with
// credential.interactive=never, GIT_TERMINAL_PROMPT=0 and an empty askpass
// nothing ever prompts. So a private https remote fetches with the user's
// keychain helper, and a repo can't add a program of its own.
// One fetch per repo runs at a time; a
// second caller waits for it and gets its answer. The fetch runs under a
// context of its own (FetchTimeout), so a caller that gives up (a closed
// request) stops waiting with its own ctx.Err() and leaves the fetch to
// finish for the others. A failure is kept for LastFetchError. ErrNoRemote
// when the repo has none.
func (g *Repo) Fetch(ctx context.Context) error {
	return g.fetchShared(context.WithoutCancel(ctx), ctx, false)
}

// FetchOwned is Fetch for a caller that owns the fetch's lifetime (the
// sweeper): the fetch it starts runs under owner and is killed when owner
// ends, and the call returns only after git has exited. A fetch someone
// else started is waited for only until owner ends.
func (g *Repo) FetchOwned(owner context.Context) error {
	return g.fetchShared(owner, owner, true)
}

// fetchShared joins the repo's running fetch or starts one under run.
// Callers wait on wait; with owned, one that started the fetch waits for
// it to finish whatever wait says.
func (g *Repo) fetchShared(run, wait context.Context, owned bool) error {
	if g.Remote == "" {
		return ErrNoRemote
	}
	r := g.repos
	key := g.Path + "\x00" + g.Remote
	r.mu.Lock()
	f, running := r.fetching[key]
	if running {
		f.waiters++
	} else {
		f = &flight{done: make(chan struct{})}
		r.fetching[key] = f
		// The fetch belongs to no one caller: it runs under a context of
		// its own, so a request that gives up doesn't fail it for the
		// others, and each caller stops waiting on its own context.
		go g.runFetch(run, key, f)
	}
	r.mu.Unlock()
	if owned && !running {
		<-f.done
		return f.err
	}
	select {
	case <-f.done:
		return f.err
	case <-wait.Done():
		return wait.Err()
	}
}

// runFetch is the one fetch behind a flight: it records the outcome, then
// releases every waiter.
func (g *Repo) runFetch(ctx context.Context, key string, f *flight) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	err := g.fetch(ctx)
	r := g.repos
	r.mu.Lock()
	delete(r.fetching, key)
	if err == nil {
		r.fetched[key] = true
		delete(r.failed, key)
	} else if !errors.Is(ctx.Err(), context.Canceled) { // an owner that stopped isn't a failed remote
		r.failed[key] = FetchFailure{Line: failureLine(err), At: time.Now().UTC()}
	}
	r.mu.Unlock()
	f.err = err
	close(f.done)
}

// failureLine is git's first stderr line for err, else its message, on
// one line and cut to 200 characters.
func failureLine(err error) string {
	line := err.Error()
	var ge *Error
	if errors.As(err, &ge) && ge.Line != "" {
		line = ge.Line
	}
	line, _, _ = strings.Cut(scrubURLs(line), "\n")
	if r := []rune(line); len(r) > 200 {
		line = string(r[:200])
	}
	return line
}

// Waiting is how many callers wait on the repo's running fetch besides the
// one that started it; 0 when none runs (for tests that need two callers to
// share one).
func (g *Repo) Waiting() int {
	r := g.repos
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.fetching[g.Path+"\x00"+g.Remote]; f != nil {
		return f.waiters
	}
	return 0
}

// LastFetchError is why the repo's last fetch by this process failed; false
// when there was none or a later one succeeded. It is kept in memory, so a
// restart forgets it.
func (g *Repo) LastFetchError() (FetchFailure, bool) {
	r := g.repos
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.failed[g.Path+"\x00"+g.Remote]
	return f, ok
}

func (g *Repo) fetch(ctx context.Context) error {
	u, err := g.remoteURL(ctx)
	if err != nil {
		return err
	}
	if !fetchable(u) {
		return fmt.Errorf("gitread: Vaquero doesn't fetch from %s", DisplayURL(u))
	}
	rm := g.Remote
	ctx = context.WithValue(ctx, fetchKey{}, true)
	if helpers := g.repos.userHelpers(ctx); len(helpers) > 0 {
		ctx = context.WithValue(ctx, helpersKey{}, helpers)
	}
	_, err = g.repos.call(ctx, g.Path, "fetch", []string{
		"--prune", "--no-tags", "--quiet",
		"--no-recurse-submodules", "--no-write-commit-graph", "--no-auto-maintenance",
		"--upload-pack=git-upload-pack",
		rm,
		"+refs/heads/*:refs/remotes/" + rm + "/*",
		"+refs/tags/*:" + remoteTags(rm) + "*",
	}, MaxOutput)
	return err
}

// hasFetched reports whether this process has fetched the repo's remote.
func (r *Repos) hasFetched(path, remote string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fetched[path+"\x00"+remote]
}

// userHelpers is the user's credential helpers as -c values
// ("credential.helper=…" and "credential.<url>.helper=…"), in git's own
// order: system config, then global, each in its file's order, empty
// values included (git treats them as a reset, as in the user's own git).
// A read that fails adds none (no helper is safe: the fetch then just
// can't authenticate).
func (r *Repos) userHelpers(ctx context.Context) []string {
	var out []string
	for _, read := range helperReads {
		b, err := r.run(ctx, "/", read, maxHelpers)
		if err != nil {
			continue
		}
		for entry := range strings.SplitSeq(strings.TrimSuffix(string(b), "\x00"), "\x00") {
			key, value, ok := strings.Cut(entry, "\n")
			if ok && helperKey(key) {
				out = append(out, key+"="+value)
			}
		}
	}
	return out
}

// helperKey is a credential helper's config key: credential.helper or
// credential.<url>.helper, with no "=" (a -c value couldn't carry it) or
// control character.
func helperKey(k string) bool {
	l := strings.ToLower(k)
	if !strings.HasPrefix(l, "credential.") || !strings.HasSuffix(l, "helper") || len(k) > 1024 || strings.Contains(k, "=") {
		return false
	}
	if l != "credential.helper" && !strings.HasSuffix(l, ".helper") {
		return false
	}
	for _, r := range k {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// maxHelpers bounds the helper config read.
const maxHelpers = 64 << 10
