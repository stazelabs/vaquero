package gitread

import (
	"context"
	"fmt"
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
// must be one fetchable accepts. One fetch per repo runs at a time; a
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
