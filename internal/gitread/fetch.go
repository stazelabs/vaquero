package gitread

import (
	"context"
	"fmt"
	"strings"
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
// user's helpers, read with git config --system --get-all
// credential.helper and git config --global --get-all credential.helper
// (from /, never --local, so no repo config or include is read), as
// -c credential.helper=<value> after the reset, in git's own order (system,
// then global; an empty value resets the list, as in git); with
// credential.interactive=never, GIT_TERMINAL_PROMPT=0 and an empty askpass
// nothing ever prompts. So a private https remote fetches with the user's
// keychain helper, and a repo can't add a program of its own.
// One fetch per repo runs at a time; a
// second caller waits for it and gets its answer. ErrNoRemote when the repo
// has none.
func (g *Repo) Fetch(ctx context.Context) error {
	if g.Remote == "" {
		return ErrNoRemote
	}
	r := g.repos
	key := g.Path + "\x00" + g.Remote
	r.mu.Lock()
	if f, ok := r.fetching[key]; ok {
		r.mu.Unlock()
		select {
		case <-f.done:
			return f.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f := &flight{done: make(chan struct{})}
	r.fetching[key] = f
	r.mu.Unlock()

	f.err = g.fetch(ctx)

	r.mu.Lock()
	delete(r.fetching, key)
	if f.err == nil {
		r.fetched[key] = true
	}
	r.mu.Unlock()
	close(f.done)
	return f.err
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

// userHelpers is the user's credential.helper values in git's order:
// system config, then global, each in its file's order, an empty value
// resetting the list. A read that fails adds none (no helper is safe: the
// fetch then just can't authenticate).
func (r *Repos) userHelpers(ctx context.Context) []string {
	var out []string
	for _, read := range [][]string{helperReads[1], helperReads[0]} {
		b, err := r.run(ctx, "/", read, maxHelpers)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(strings.TrimSuffix(string(b), "\n"), "\n") {
			if line == "" {
				out = nil
				continue
			}
			out = append(out, line)
		}
	}
	return out
}

// maxHelpers bounds the helper config read.
const maxHelpers = 64 << 10
