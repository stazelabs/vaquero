package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// The git reads behind the file view, a ticket's commits and the repo
// status (VAQ-136), shared by the pages and the API. Every input is checked
// before git runs; every call uses the request's context, so a closed tab
// or a dropped client stops it.

const (
	// maxTicketCommits is how many commits a ticket lists.
	maxTicketCommits = 50
	// maxStatusBranches is how many local branches the repo status reads,
	// and maxUnpushed how many unpushed ones it lists: each branch costs a
	// git call or two.
	maxStatusBranches = 100
	maxUnpushed       = 50
)

// openRepo opens p's repository, or says why it can't: no repository set,
// no git, or a repo that can't be read now (logged). All three are 409s.
func openRepo(ctx context.Context, d Deps, p gen.Project) (*gitread.Repo, *api.Failure) {
	if p.RepoPath == "" {
		return nil, api.Fail(http.StatusConflict, "no_repo", "No repository set for this project.")
	}
	noGit := api.Fail(http.StatusConflict, "no_git", "Git isn't installed.")
	if d.Git == nil || !d.Git.Available() {
		return nil, noGit
	}
	repo, err := d.Git.Open(ctx, p.RepoPath, p.MainBranch, p.Remote)
	if errors.Is(err, gitread.ErrNoGit) {
		return nil, noGit
	}
	if err != nil {
		d.Logger.WarnContext(ctx, "open repo", "project", p.Key, "err", err)
		return nil, api.Fail(http.StatusConflict, "repo_unreadable", "Can't read the repository at "+p.RepoPath+".")
	}
	return repo, nil
}

// filePath is the {path} wildcard of a files route, unescaped when the URL
// kept an escaped form (chi routes on RawPath then).
func filePath(r *http.Request) string {
	raw := chi.URLParam(r, "*")
	if r.URL.RawPath == "" {
		return raw
	}
	if v, err := url.PathUnescape(raw); err == nil {
		return v
	}
	return "\x00" // refused by ValidPath
}

// fileInput checks a file request's path and ?at= before any git runs: a
// 422 for either. at is "" for main.
func fileInput(r *http.Request) (p, at string, f *api.Failure) {
	p, at = filePath(r), r.URL.Query().Get("at")
	if !gitread.ValidPath(p) {
		return p, at, api.Fail(http.StatusUnprocessableEntity, "invalid", "That isn't a path in the repository.")
	}
	if _, given := r.URL.Query()["at"]; given && !gitread.ValidRev(at) {
		return p, at, api.Fail(http.StatusUnprocessableEntity, "invalid", "That isn't a revision: use a branch, a tag or a commit hash.")
	}
	return p, at, nil
}

// isMarkdown reports whether the file view renders name as Markdown.
func isMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

// readFile is name at at ("" for main) in p's repository.
func readFile(ctx context.Context, repo *gitread.Repo, name, at string) (api.File, *api.Failure, error) {
	rev, where := at, "at "+at
	if at == "" {
		rev, where = repo.Main, "on "+repo.Main
	}
	f, err := repo.Show(ctx, rev, name)
	switch {
	case errors.Is(err, gitread.ErrNoRev):
		return api.File{}, api.NotFound("No revision " + rev + " in the repository."), nil
	case errors.Is(err, gitread.ErrNoFile):
		return api.File{}, api.NotFound("No file " + name + " " + where + "."), nil
	case err != nil:
		return api.File{}, nil, err
	}
	out := api.File{Path: name, At: rev, Blob: f.Blob, Size: f.Size, TooLarge: f.TooLarge, Markdown: isMarkdown(name)}
	if !f.TooLarge {
		if strings.IndexByte(f.Text, 0) >= 0 || !utf8.ValidString(f.Text) {
			out.Binary = true
		} else {
			out.Text = &f.Text
		}
	}
	return out, nil, nil
}

// ticketKeys is t's key and every key it had before it moved, so commits
// made under an old key still count.
func ticketKeys(ctx context.Context, d Deps, t gen.Ticket, p gen.Project) ([]string, error) {
	keys := []string{ticketKey(p, t.Number)}
	old, err := d.R.ListTicketOldKeys(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	for _, k := range old {
		keys = append(keys, core.TicketKey{Project: k.ProjectKey, Number: k.Number}.String())
	}
	return keys, nil
}

// ticketCommits is the commits on main that name t, newest first, at most
// maxTicketCommits, with their diffstat and the first version tag that
// contains the newest.
func ticketCommits(ctx context.Context, d Deps, repo *gitread.Repo, t gen.Ticket, p gen.Project) (api.TicketCommits, error) {
	out := api.TicketCommits{Commits: []api.Commit{}, Diffstat: api.Diffstat{Files: []string{}}}
	keys, err := ticketKeys(ctx, d, t, p)
	if err != nil {
		return out, err
	}
	var all []gitread.Commit
	seen := map[string]bool{}
	for _, k := range keys {
		cs, err := repo.Mentions(ctx, k, maxTicketCommits)
		if err != nil {
			return out, err
		}
		for _, c := range cs {
			if !seen[c.Hash] {
				seen[c.Hash] = true
				all = append(all, c)
			}
		}
	}
	if len(keys) > 1 {
		slices.SortStableFunc(all, func(a, b gitread.Commit) int { return b.At.Compare(a.At) })
	}
	all = all[:min(len(all), maxTicketCommits)]
	if len(all) == 0 {
		return out, nil
	}
	hashes := make([]string, len(all))
	for i, c := range all {
		hashes[i] = c.Hash
		out.Commits = append(out.Commits, api.Commit{Hash: c.Hash, Short: c.Short, Subject: c.Subject, Author: c.Author, At: timefmt.Format(c.At)})
	}
	ds, err := repo.Diffstat(ctx, hashes)
	if err != nil {
		return out, err
	}
	out.Diffstat = api.Diffstat{Added: ds.Added, Removed: ds.Removed, Files: append([]string{}, ds.Files...)}
	if out.Released, err = repo.FirstTagContaining(ctx, all[0].Hash); err != nil {
		return out, err
	}
	return out, nil
}

// repoStatus is main against the remote and the local branches that
// aren't pushed, as of the last fetch: it never fetches. It reads at most
// maxStatusBranches branches (by name) and lists at most maxUnpushed.
func repoStatus(ctx context.Context, repo *gitread.Repo) (api.RepoStatus, error) {
	out := api.RepoStatus{UnpushedBranches: []api.BranchStatus{}}
	var err error
	if out.Main.Ahead, out.Main.Behind, out.Main.Tracked, err = repo.Compare(ctx, repo.Main, ""); err != nil {
		return out, err
	}
	branches, err := repo.Branches(ctx, maxStatusBranches)
	if err != nil {
		return out, err
	}
	for _, b := range branches {
		if b == repo.Main {
			continue
		}
		ahead, behind, tracked, err := repo.Compare(ctx, b, "")
		if err != nil {
			return out, err
		}
		if tracked && ahead == 0 {
			continue
		}
		out.UnpushedBranches = append(out.UnpushedBranches, api.BranchStatus{Name: b, Ahead: ahead, Behind: behind, Tracked: tracked})
		if len(out.UnpushedBranches) == maxUnpushed {
			break
		}
	}
	if at, ok := repo.LastFetch(); ok {
		out.LastFetchAt = new(timefmt.Format(at))
	}
	return out, nil
}

// APIFile is GET /api/v1/projects/{key}/files/*?at=REV: a repo file at
// main, or at a revision.
func APIFile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		name, at, f := fileInput(r)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		repo, f := openRepo(r.Context(), d, p)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		file, f, err := readFile(r.Context(), repo, name, at)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		api.WriteJSON(w, http.StatusOK, file)
	}
}

// APITicketCommits is GET /api/v1/tickets/{ticket}/commits.
func APITicketCommits(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		repo, f := openRepo(r.Context(), d, p)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		out, err := ticketCommits(r.Context(), d, repo, t, p)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIRepoStatus is GET /api/v1/projects/{key}/repo/status.
func APIRepoStatus(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		repo, f := openRepo(r.Context(), d, p)
		if f != nil {
			api.WriteFailure(w, f)
			return
		}
		out, err := repoStatus(r.Context(), repo)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// fileURL is the file view's address for name in project key, at at ("" for
// main).
func fileURL(key, name, at string) string {
	u := url.URL{Path: "/p/" + key + "/files/" + name}
	if at != "" {
		u.RawQuery = url.Values{"at": {at}}.Encode()
	}
	return u.String()
}
