package gitread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Repo is one project's repo: its work tree, main branch and remote.
// Remote is resolved by Open: "" means origin when the repo has it, else
// none; a named remote the repo doesn't have is none too, so reads that
// don't need the remote still work (settings check the name against
// Remotes).
type Repo struct {
	Path, Main, Remote string

	repos  *Repos
	gitDir string
}

// Commit is one commit as the reads list it.
type Commit struct {
	Hash, Short, Subject, Author string
	At                           time.Time // author date, UTC
}

// File is a file at a revision. Text is empty when TooLarge (Size over
// MaxShow); Blob is its blob id either way.
type File struct {
	Blob     string
	Size     int64
	Text     string
	TooLarge bool
}

// Diffstat is the lines added and removed by a set of commits and the
// files they touched (sorted, distinct; a rename counts as its new path).
type Diffstat struct {
	Added, Removed int
	Files          []string
}

// maxDiffstat bounds the commits one Diffstat reads.
const maxDiffstat = 200

// Open checks path is a git work tree (or a directory in one) and returns
// its Repo. mainBranch must be a valid ref name and remote "" or a valid
// remote name (ErrInvalid otherwise); ErrNoGit without git, ErrOldGit
// with a git older than MinVersion; ErrNotRepo for anything git doesn't
// take as a work tree; ErrPartialClone for a partial clone (or a repo
// whose config names a promisor remote) under a git older than
// NoLazyFetchVersion.
func (r *Repos) Open(ctx context.Context, path, mainBranch, remote string) (*Repo, error) {
	if r.git == "" {
		return nil, ErrNoGit
	}
	if !r.Available() {
		return nil, ErrOldGit
	}
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
		return nil, fmt.Errorf("%w: the repo path must be absolute", ErrInvalid)
	}
	if !ValidRev(mainBranch) {
		return nil, fmt.Errorf("%w: main branch %q", ErrInvalid, mainBranch)
	}
	if remote != "" && !validRemote(remote) {
		return nil, fmt.Errorf("%w: remote %q", ErrInvalid, remote)
	}
	g := &Repo{Path: filepath.Clean(path), Main: mainBranch, repos: r}
	out, err := g.git(ctx, "rev-parse", "--is-inside-work-tree", "--absolute-git-dir", "--show-toplevel", "--show-prefix")
	if err != nil {
		if errors.Is(err, ErrNoGit) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrNotRepo, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "true" || lines[2] == "" {
		return nil, ErrNotRepo
	}
	g.gitDir = lines[1]
	g.Path = workTreeRoot(g.Path, lines[2], lines[3])
	if !r.atLeast(NoLazyFetchVersion) {
		partial, err := g.partialClone(ctx)
		if err != nil {
			return nil, err
		}
		if partial {
			return nil, ErrPartialClone
		}
	}
	remotes, err := g.Remotes(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case remote != "" && slices.Contains(remotes, remote):
		g.Remote = remote
	case remote == "" && slices.Contains(remotes, "origin"):
		g.Remote = "origin"
	}
	return g, nil
}

// partialClone reports whether the repo's config makes it a partial clone
// or names a promisor remote: extensions.partialclone, or any remote's
// promisor (unless false) or partialclonefilter. Exit 1 is no match.
func (g *Repo) partialClone(ctx context.Context) (bool, error) {
	out, err := g.git(ctx, "config", promisorRead...)
	if err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		key, value, _ := strings.Cut(entry, "\n")
		if key == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(key), ".promisor") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "false", "no", "off", "0":
				continue
			}
		}
		return true, nil
	}
	return false, nil
}

// workTreeRoot is the work tree's root for a path inside it: the path as
// given, less its prefix (rev-parse --show-prefix), so the root keeps the
// spelling the owner typed; else git's toplevel (symlinks resolved).
// Every read is relative to the root.
func workTreeRoot(path, toplevel, prefix string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return path
	}
	if root, ok := strings.CutSuffix(path, "/"+prefix); ok && root != "" {
		return root
	}
	return toplevel
}

// git runs a read in the repo.
func (g *Repo) git(ctx context.Context, sub string, args ...string) ([]byte, error) {
	return g.repos.call(ctx, g.Path, sub, args, MaxOutput)
}

// mainRef is the main branch's full ref name.
func (g *Repo) mainRef() string { return "refs/heads/" + g.Main }

// Head is main's commit.
func (g *Repo) Head(ctx context.Context) (string, error) {
	c, err := g.commit(ctx, g.mainRef())
	if err != nil {
		return "", fmt.Errorf("main branch %s: %w", g.Main, err)
	}
	return c, nil
}

// commit resolves rev (checked by the caller) to a commit hash.
func (g *Repo) commit(ctx context.Context, rev string) (string, error) {
	out, err := g.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		if exitCode(err) == 1 {
			return "", fmt.Errorf("%w: %s", ErrNoRev, rev)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// object is the id and type of path at rev; ErrNoFile when absent.
func (g *Repo) object(ctx context.Context, rev, path string) (id, kind string, err error) {
	if !ValidRev(rev) || !ValidPath(path) {
		return "", "", ErrInvalid
	}
	c, err := g.commit(ctx, rev)
	if err != nil {
		return "", "", err
	}
	out, err := g.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", c+":"+path)
	if err != nil {
		if exitCode(err) == 1 {
			return "", "", ErrNoFile
		}
		return "", "", err
	}
	id = strings.TrimSpace(string(out))
	out, err = g.git(ctx, "cat-file", "-t", id)
	if err != nil {
		return "", "", err
	}
	return id, strings.TrimSpace(string(out)), nil
}

// Blob is path's blob id at rev: "" and ErrNoFile when rev has no such
// file (a directory isn't one).
func (g *Repo) Blob(ctx context.Context, rev, path string) (string, error) {
	id, kind, err := g.object(ctx, rev, path)
	if err != nil {
		return "", err
	}
	if kind != "blob" {
		return "", ErrNoFile
	}
	return id, nil
}

// maxBlobs is how many paths one ls-tree reads.
const maxBlobs = 200

// Blobs is the blob id of each of paths at commit (a hash, as Head
// answers) that is a file there, in as few commands as the paths allow
// (one ls-tree per 200): a path that isn't a file at commit (absent, a
// directory, a submodule) is left out.
func (g *Repo) Blobs(ctx context.Context, commit string, paths []string) (map[string]string, error) {
	if !isHash(commit) {
		return nil, ErrInvalid
	}
	want := map[string]bool{}
	var list []string
	for _, p := range paths {
		if !ValidPath(p) {
			return nil, ErrInvalid
		}
		if !want[p] {
			want[p] = true
			list = append(list, p)
		}
	}
	out := map[string]string{}
	for len(list) > 0 {
		n := min(len(list), maxBlobs)
		args := append([]string{"-z", "--full-tree", "--end-of-options", commit, "--"}, list[:n]...)
		list = list[n:]
		raw, err := g.git(ctx, "ls-tree", args...)
		if err != nil {
			if exitCode(err) == 128 {
				return nil, fmt.Errorf("%w: %s", ErrNoRev, commit)
			}
			return nil, err
		}
		// Records: "mode type id\tpath\0".
		for rec := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\x00"), "\x00") {
			meta, path, ok := strings.Cut(rec, "\t")
			f := strings.Fields(meta)
			if !ok || len(f) != 3 || f[1] != "blob" || !want[path] || !isHash(f[2]) {
				continue
			}
			out[path] = f[2]
		}
	}
	return out, nil
}

// Show is path at rev. Past MaxShow the text is left out and TooLarge set.
func (g *Repo) Show(ctx context.Context, rev, path string) (File, error) {
	id, err := g.Blob(ctx, rev, path)
	if err != nil {
		return File{}, err
	}
	out, err := g.git(ctx, "cat-file", "-s", id)
	if err != nil {
		return File{}, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return File{}, fmt.Errorf("gitread: size of %s: %w", path, err)
	}
	f := File{Blob: id, Size: size}
	if size > MaxShow {
		f.TooLarge = true
		return f, nil
	}
	text, err := g.repos.call(ctx, g.Path, "cat-file", []string{"blob", id}, MaxShow)
	if err != nil {
		if errors.Is(err, ErrOutputLimit) {
			f.TooLarge = true
			return f, nil
		}
		return File{}, err
	}
	f.Text = string(text)
	return f, nil
}

// Follow is where path, as it was at since, is on main now: renames on
// main's first-parent history since then are followed in order. gone is
// true when main has no file there.
func (g *Repo) Follow(ctx context.Context, path, since string) (current string, gone bool, err error) {
	if current, err = g.follow(ctx, path, since, g.mainRef()); err != nil {
		return "", false, err
	}
	if _, err := g.Blob(ctx, g.mainRef(), current); err != nil {
		if errors.Is(err, ErrNoFile) {
			return current, true, nil
		}
		return "", false, err
	}
	return current, false, nil
}

// Rename is one rename on main's first-parent history.
type Rename struct{ From, To string }

// Renames is every rename on head's first-parent history since since, in
// order (head a commit hash on main, as Head answers): one history walk
// for every path pinned at since. FollowRenames applies them to a path.
func (g *Repo) Renames(ctx context.Context, since, head string) ([]Rename, error) {
	if !isHash(head) {
		return nil, ErrInvalid
	}
	return g.renames(ctx, since, head)
}

// FollowRenames is where path is after renames, applied in order.
func FollowRenames(path string, renames []Rename) string {
	for _, r := range renames {
		if r.From == path {
			path = r.To
		}
	}
	return path
}

// follow is path's renames on tip's first-parent history since since.
func (g *Repo) follow(ctx context.Context, path, since, tip string) (string, error) {
	if !ValidPath(path) {
		return "", ErrInvalid
	}
	list, err := g.renames(ctx, since, tip)
	if err != nil {
		return "", err
	}
	return FollowRenames(path, list), nil
}

// renames is the renames on tip's first-parent history since since.
func (g *Repo) renames(ctx context.Context, since, tip string) ([]Rename, error) {
	if !ValidRev(since) {
		return nil, ErrInvalid
	}
	from, err := g.commit(ctx, since)
	if err != nil {
		return nil, err
	}
	out, err := g.git(ctx, "log", "-z", "--reverse", "--first-parent", "--diff-merges=first-parent", "--format=",
		"--name-status", "-M", "--diff-filter=RD", "--no-textconv", "--no-ext-diff", "--no-show-signature",
		"--end-of-options", "^"+from, tip)
	if err != nil {
		return nil, err
	}
	var list []Rename
	f := bytes.Split(bytes.TrimSuffix(out, []byte{0}), []byte{0})
	for i := 0; i < len(f); {
		status := strings.TrimLeft(string(f[i]), "\n")
		switch {
		case status == "":
			i++
		case strings.HasPrefix(status, "R") && i+2 < len(f):
			list = append(list, Rename{From: string(f[i+1]), To: string(f[i+2])})
			i += 3
		case i+1 < len(f):
			i += 2
		default:
			i++
		}
	}
	return list, nil
}

// logFormat is a commit as the reads parse it: hash, short hash, subject,
// author and author time, separated by NUL (and each commit ended by one,
// under -z).
const logFormat = "--format=%H%x00%h%x00%s%x00%an%x00%at"

// commits parses logFormat output.
func commits(out []byte) ([]Commit, error) {
	f := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(f) == 1 && f[0] == "" {
		return nil, nil
	}
	if len(f)%5 != 0 {
		return nil, fmt.Errorf("gitread: unexpected log output")
	}
	var list []Commit
	for i := 0; i < len(f); i += 5 {
		at, err := strconv.ParseInt(f[i+4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("gitread: commit time %q", f[i+4])
		}
		list = append(list, Commit{Hash: strings.TrimPrefix(f[i], "\n"), Short: f[i+1], Subject: oneLine(f[i+2]), Author: oneLine(f[i+3]), At: time.Unix(at, 0).UTC()})
	}
	return list, nil
}

// oneLine is s without C0 or C1 control characters (tabs and DEL too):
// commit subjects and author names are untrusted, and a terminal would
// act on an escape sequence in them. Invalid UTF-8 becomes U+FFFD.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// log runs git log with logFormat, at most n commits (1 to 1000).
func (g *Repo) log(ctx context.Context, n int, args ...string) ([]Commit, error) {
	n = min(max(n, 1), 1000)
	base := []string{"-z", logFormat, "--max-count=" + strconv.Itoa(n), "--no-textconv", "--no-ext-diff", "--no-show-signature"}
	out, err := g.git(ctx, "log", append(base, args...)...)
	if err != nil {
		return nil, err
	}
	return commits(out)
}

// PathLog is the commits on main that changed path since since (all of
// main's history when since is ""), newest first, following renames.
func (g *Repo) PathLog(ctx context.Context, path, since string, max int) ([]Commit, error) {
	if !ValidPath(path) || (since != "" && !ValidRev(since)) {
		return nil, ErrInvalid
	}
	revs := []string{g.mainRef()}
	if since != "" {
		from, err := g.commit(ctx, since)
		if err != nil {
			return nil, err
		}
		revs = append(revs, "^"+from)
	}
	args := append([]string{"--follow", "--end-of-options"}, revs...)
	return g.log(ctx, max, append(args, "--", path)...)
}

// Mentions is the commits on main whose message contains "(KEY)", the
// repo's commit format, newest first: (VAQ-12) and not (VAQ-123).
func (g *Repo) Mentions(ctx context.Context, key string, max int) ([]Commit, error) {
	if !validKey(key) {
		return nil, ErrInvalid
	}
	return g.log(ctx, max, "--fixed-strings", "--grep=("+key+")", "--end-of-options", g.mainRef())
}

// BranchCommits is branch's commits not on main (main..branch), newest
// first.
func (g *Repo) BranchCommits(ctx context.Context, branch string, max int) ([]Commit, error) {
	if !ValidRev(branch) {
		return nil, ErrInvalid
	}
	if _, err := g.commit(ctx, branch); err != nil {
		return nil, err
	}
	return g.log(ctx, max, "--end-of-options", "^"+g.mainRef(), branch)
}

// Merged reports whether branch is merged into main (its commit is an
// ancestor of main's).
func (g *Repo) Merged(ctx context.Context, branch string) (bool, error) {
	if !ValidRev(branch) {
		return false, ErrInvalid
	}
	c, err := g.commit(ctx, branch)
	if err != nil {
		return false, err
	}
	head, err := g.Head(ctx)
	if err != nil {
		return false, err
	}
	_, err = g.git(ctx, "merge-base", "--is-ancestor", c, head)
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, err
}

// TagExists reports whether the repo has the tag locally.
func (g *Repo) TagExists(ctx context.Context, tag string) (bool, error) {
	if !ValidRev(tag) {
		return false, ErrInvalid
	}
	return g.refExists(ctx, "refs/tags/"+tag)
}

// refExists reports whether a full ref name exists.
func (g *Repo) refExists(ctx context.Context, ref string) (bool, error) {
	_, err := g.git(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", ref)
	switch {
	case err == nil:
		return true, nil
	case exitCode(err) == 1:
		return false, nil
	}
	return false, err
}

// Diffstat is the lines added and removed by commits (each against its
// first parent) and the files they touched. At most 200 commits.
func (g *Repo) Diffstat(ctx context.Context, list []string) (Diffstat, error) {
	d := Diffstat{Files: []string{}}
	if len(list) == 0 {
		return d, nil
	}
	if len(list) > maxDiffstat {
		return d, fmt.Errorf("%w: more than %d commits", ErrInvalid, maxDiffstat)
	}
	for _, c := range list {
		if !isHash(c) {
			return d, ErrInvalid
		}
	}
	args := append([]string{"-z", "--numstat", "--format=", "-M", "--diff-merges=first-parent",
		"--no-textconv", "--no-ext-diff", "--no-show-signature", "--end-of-options"}, list...)
	out, err := g.git(ctx, "show", args...)
	if err != nil {
		return d, err
	}
	// Records: "added\tremoved\tpath\0", or for a rename
	// "added\tremoved\t\0old\0new\0". Binary files have "-" counts.
	f := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	seen := map[string]bool{}
	for i := 0; i < len(f); i++ {
		rec := strings.TrimLeft(f[i], "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		d.Added += added
		d.Removed += removed
		path := parts[2]
		if path == "" && i+2 < len(f) {
			path = f[i+2]
			i += 2
		}
		path = oneLine(path) // shown as is: no escape sequence leaves
		if path != "" && !seen[path] {
			seen[path] = true
			d.Files = append(d.Files, path)
		}
	}
	slices.Sort(d.Files)
	return d, nil
}

// FirstTagContaining is the lowest version tag (git's v:refname order,
// so v0.1.10 follows v0.1.9) whose history contains commit; "" when none.
func (g *Repo) FirstTagContaining(ctx context.Context, commit string) (string, error) {
	if !ValidRev(commit) {
		return "", ErrInvalid
	}
	c, err := g.commit(ctx, commit)
	if err != nil {
		return "", err
	}
	out, err := g.git(ctx, "tag", "--list", "--contains", c, "--sort=v:refname")
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(first), nil
}

// Compare counts the commits local (a local branch) has that remoteRef (a
// remote-tracking ref such as origin/main; "" for the repo's remote's
// branch of the same name) hasn't, and the reverse, as of the last fetch.
// ok is false when there is no such remote-tracking ref (never pushed or
// never fetched, or no remote).
func (g *Repo) Compare(ctx context.Context, local, remoteRef string) (ahead, behind int, ok bool, err error) {
	if remoteRef == "" {
		if g.Remote == "" {
			return 0, 0, false, nil
		}
		remoteRef = g.Remote + "/" + local
	}
	if !ValidRev(local) || !ValidRev(remoteRef) {
		return 0, 0, false, ErrInvalid
	}
	there, err := g.refExists(ctx, "refs/remotes/"+remoteRef)
	if err != nil || !there {
		return 0, 0, false, err
	}
	out, err := g.git(ctx, "rev-list", "--count", "--left-right", "--end-of-options", "refs/heads/"+local+"...refs/remotes/"+remoteRef)
	if err != nil {
		return 0, 0, false, err
	}
	a, b, found := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if !found {
		return 0, 0, false, fmt.Errorf("gitread: unexpected rev-list output")
	}
	if ahead, err = strconv.Atoi(a); err == nil {
		behind, err = strconv.Atoi(b)
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("gitread: unexpected rev-list output")
	}
	return ahead, behind, true, nil
}

// remoteTags is where Fetch puts the remote's tags.
func remoteTags(remote string) string { return "refs/vaquero/remote-tags/" + remote + "/" }

// RemoteTag reports whether the remote has tag, as of the last fetch
// Vaquero ran (Fetch puts the remote's tags under
// refs/vaquero/remote-tags/{remote}/). known is false before Vaquero has
// fetched: no ref there and no fetch by this process (a remote with no
// tags at all reads as unknown after a restart, until the next fetch).
func (g *Repo) RemoteTag(ctx context.Context, tag string) (on bool, known bool, err error) {
	if !ValidRev(tag) {
		return false, false, ErrInvalid
	}
	if g.Remote == "" {
		return false, false, nil
	}
	out, err := g.git(ctx, "for-each-ref", "--count=1", "--format=%(refname)", remoteTags(g.Remote))
	if err != nil {
		return false, false, err
	}
	known = len(bytes.TrimSpace(out)) > 0 || g.repos.hasFetched(g.Path, g.Remote)
	if !known {
		return false, false, nil
	}
	on, err = g.refExists(ctx, remoteTags(g.Remote)+tag)
	return on, true, err
}

// Branches is the repo's local branches, sorted by name, at most limit (1 to
// 1000). A branch whose name ValidRev refuses is left out, so every name
// returned can be passed back to the other reads.
func (g *Repo) Branches(ctx context.Context, limit int) ([]string, error) {
	limit = min(max(limit, 1), 1000)
	// Twice the limit, allowing for names ValidRev drops, and never every
	// ref of a repo with thousands of branches.
	out, err := g.git(ctx, "for-each-ref", "--sort=refname", "--count="+strconv.Itoa(2*limit), "--format=%(refname)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var names []string
	for line := range strings.SplitSeq(string(out), "\n") {
		name, ok := strings.CutPrefix(strings.TrimSpace(line), "refs/heads/")
		if !ok || !ValidRev(name) {
			continue
		}
		names = append(names, name)
		if len(names) == limit {
			break
		}
	}
	return names, nil
}

// Remotes is the repo's remotes, sorted.
func (g *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := g.git(ctx, "remote")
	if err != nil {
		return nil, err
	}
	var list []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			list = append(list, line)
		}
	}
	slices.Sort(list)
	return list, nil
}

// remoteURL is the remote's fetch URL as git resolves it, credentials and
// all; "" without a remote.
func (g *Repo) remoteURL(ctx context.Context) (string, error) {
	if g.Remote == "" {
		return "", nil
	}
	out, err := g.git(ctx, "remote", "get-url", g.Remote)
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(first), nil
}

// RemoteURL is the remote's URL as git reports it, with any credentials
// cut out (DisplayURL); "" without a remote.
func (g *Repo) RemoteURL(ctx context.Context) (string, error) {
	u, err := g.remoteURL(ctx)
	return DisplayURL(u), err
}

// LastFetch is when the repo last fetched (FETCH_HEAD's modification
// time), by Vaquero or anyone; false when it never has.
func (g *Repo) LastFetch() (time.Time, bool) {
	if g.gitDir == "" {
		return time.Time{}, false
	}
	fi, err := os.Stat(filepath.Join(g.gitDir, "FETCH_HEAD"))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime().UTC(), true
}
