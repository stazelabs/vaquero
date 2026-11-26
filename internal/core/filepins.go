package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
)

// FilePin is a repo file a plan approval pins (ruling P2-R2): the
// reference that named it, its path, main's commit when asked and the
// file's blob id there, and the repository (the project's repo path, its
// work tree's root) and main branch it was read from. One per path: two
// references to one file (with different anchors) are one pin, named by
// the first. A pin is checked only against the repository and branch it
// was taken from: with either setting changed since, it is unchecked
// (PinMatches), never compared with some other tree that happens to hold
// the same content.
type FilePin struct {
	RefID      string `json:"ref_id"`
	Path       string `json:"path"`
	Commit     string `json:"commit"`
	Blob       string `json:"blob"`
	RepoPath   string `json:"repo_path"`
	MainBranch string `json:"main_branch"`
}

// PinMatches reports whether pin was taken against p's repository and main
// branch as they are set now.
func PinMatches(pin FilePin, p gen.Project) bool {
	return pin.RepoPath != "" && pin.RepoPath == p.RepoPath && pin.MainBranch == p.MainBranch
}

// The states of a pinned file against main, and of a reference against
// the pins (FileState.State).
const (
	FileUnchanged = "unchanged" // its content on main is what was pinned
	FileChanged   = "changed"   // its content changed (renamed or not)
	FileRenamed   = "renamed"   // moved on main, its content unchanged
	FileGone      = "gone"      // not on main any more, or no longer referenced
	FileAdded     = "added"     // referenced, but not pinned
	FileUnchecked = "unchecked" // the repo couldn't be read
)

// FileState is one file against an approval's pins: its pinned path, its
// State, and Now, the path it has on main when that differs (renamed).
type FileState struct {
	Path  string `json:"path"`
	State string `json:"state"`
	Now   string `json:"now,omitempty"`
}

// IsRepoPathRef reports whether a stored reference target is a repo path
// rather than a URL: only repo paths are pinned.
func IsRepoPathRef(target string) bool { return !refScheme.MatchString(target) }

// RefPaths is the repo paths among refs, each once, sorted, with the first
// reference naming it.
func RefPaths(refs []gen.TicketRef) []FilePin {
	var out []FilePin
	for _, r := range refs {
		if !IsRepoPathRef(r.Target) || slices.ContainsFunc(out, func(p FilePin) bool { return p.Path == r.Target }) {
			continue
		}
		out = append(out, FilePin{RefID: r.ID, Path: r.Target})
	}
	slices.SortFunc(out, func(a, b FilePin) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// CheckFilePins compares each pin with main at head (P2-R3), by content:
// a pin whose path is still on main is unchanged when its blob there is
// the pinned one, and changed otherwise, whatever was renamed meanwhile
// (the reference names that path, so its content is what a reader gets).
// Only a pin whose path has left main has its renames followed from the
// pinned commit: found with the pinned content it is renamed, with other
// content changed (Now the new path either way), not found gone. The
// renames are walked once per distinct pinned commit (Renames), and the
// files read in two ls-trees, however many pins. A pin whose history can't
// be walked is unchecked; an error is the repo not readable at all.
// Never inside a transaction.
func CheckFilePins(ctx context.Context, g *gitread.Repo, head string, pins []FilePin) ([]FileState, error) {
	out := make([]FileState, len(pins))
	if len(pins) == 0 {
		return out, nil
	}
	paths := make([]string, len(pins))
	for i, p := range pins {
		paths[i] = p.Path
	}
	blobs, err := g.Blobs(ctx, head, paths)
	if err != nil {
		return nil, err
	}
	walks := map[string][]gitread.Rename{} // pinned commit -> its renames
	failed := map[string]bool{}
	current := map[int]string{}
	var moved []string
	for i, p := range pins {
		out[i] = FileState{Path: p.Path, State: FileUnchanged}
		if blob, there := blobs[p.Path]; there {
			if blob != p.Blob {
				out[i].State = FileChanged
			}
			continue
		}
		list, walked := walks[p.Commit]
		if !walked && !failed[p.Commit] {
			list, err = g.Renames(ctx, p.Commit, head)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err != nil {
				failed[p.Commit] = true
			} else {
				walks[p.Commit] = list
			}
		}
		if failed[p.Commit] {
			out[i].State = FileUnchecked
			continue
		}
		cur := gitread.FollowRenames(p.Path, list)
		if cur == p.Path {
			out[i].State = FileGone
			continue
		}
		current[i] = cur
		moved = append(moved, cur)
	}
	if len(current) == 0 {
		return out, nil
	}
	now, err := g.Blobs(ctx, head, moved)
	if err != nil {
		return nil, err
	}
	for i, cur := range current {
		blob, there := now[cur]
		switch {
		case !there:
			out[i].State = FileGone
		case blob != pins[i].Blob:
			out[i].State, out[i].Now = FileChanged, cur
		default:
			out[i].State, out[i].Now = FileRenamed, cur
		}
	}
	return out, nil
}

// repoReason is why a repo can't be read, for "Can't read the repo: …".
func repoReason(err error) string {
	switch {
	case errors.Is(err, gitread.ErrNoGit):
		return "git isn't installed."
	case errors.Is(err, gitread.ErrNotRepo):
		return "it isn't a git work tree."
	case errors.Is(err, gitread.ErrNoRev):
		return "the main branch has no commit."
	}
	return err.Error() + "."
}

// openProjectRepo is p's repo and main's head, or why they can't be read.
func (c *Core) openProjectRepo(ctx context.Context, p gen.Project) (*gitread.Repo, string, error) {
	if p.RepoPath == "" {
		return nil, "", errors.New("no repository is set for this project")
	}
	if c.Git == nil {
		return nil, "", gitread.ErrNoGit
	}
	g, err := c.Git.Open(ctx, p.RepoPath, p.MainBranch, p.Remote)
	if err != nil {
		return nil, "", err
	}
	head, err := g.Head(ctx)
	if err != nil {
		return nil, "", err
	}
	return g, head, nil
}

// askedFiles is what a plan approval pins of its tickets' repo files,
// read before its transaction (git never runs inside one), with the repo
// settings it read them under, which the transaction checks again.
type askedFiles struct {
	repoPath, main string
	pins           map[string][]FilePin // ticket id -> its pins, by path
}

// pinFiles reads, outside any transaction, the repo files a plan approval
// of in pins: every repo-path reference of each ticket at main's head
// (ruling P2-R2). A ticket or project that doesn't exist is left to the
// transaction to refuse. No repo-path references, no git.
func (c *Core) pinFiles(ctx context.Context, in DecisionInput) (askedFiles, error) {
	q := gen.New(c.DB)
	a := askedFiles{pins: map[string][]FilePin{}}
	p, err := q.GetProject(ctx, in.ProjectID)
	if err != nil {
		return a, nil
	}
	a.repoPath, a.main = p.RepoPath, p.MainBranch
	var all []*FilePin
	for _, id := range in.TicketIDs {
		if _, seen := a.pins[id]; seen {
			continue
		}
		refs, err := q.ListTicketRefs(ctx, id)
		if err != nil {
			return a, fmt.Errorf("list references: %w", err)
		}
		a.pins[id] = RefPaths(refs)
		for i := range a.pins[id] {
			all = append(all, &a.pins[id][i])
		}
	}
	if len(all) == 0 {
		return a, nil
	}
	g, head, err := c.openProjectRepo(ctx, p)
	if err != nil {
		return a, invalid("tickets", "Can't read the repo: "+repoReason(err))
	}
	paths := make([]string, len(all))
	for i, f := range all {
		paths[i] = f.Path
	}
	blobs, err := g.Blobs(ctx, head, paths)
	if err != nil {
		return a, invalid("tickets", "Can't read the repo: "+repoReason(err))
	}
	for _, f := range all {
		blob, ok := blobs[f.Path]
		if !ok {
			return a, invalid("tickets", f.Path+" isn't on main; merge it before asking for approval.")
		}
		f.Commit, f.Blob, f.RepoPath, f.MainBranch = head, blob, p.RepoPath, p.MainBranch
	}
	return a, nil
}

// samePaths reports whether a ticket's references name the files pinned.
func samePaths(pins []FilePin, refs []gen.TicketRef) bool {
	now := RefPaths(refs)
	return slices.EqualFunc(pins, now, func(a, b FilePin) bool { return a.Path == b.Path })
}

// filesJSON is pins as stored, never null.
func filesJSON(pins []FilePin) (string, error) {
	if pins == nil {
		pins = []FilePin{}
	}
	b, err := json.Marshal(pins)
	if err != nil {
		return "", fmt.Errorf("file pins: %w", err)
	}
	return string(b), nil
}

// fileVerdict is, read before an Approve's transaction, which of a plan
// approval's tickets have a pinned file that changed since it was asked,
// or why the files couldn't be checked.
type fileVerdict struct {
	repoPath, main string
	err            error           // the repo couldn't be read
	changed        map[string]bool // ticket id -> a pinned file changed
}

// checkAskedFiles compares the pins decisionID asked with main, outside
// any transaction. Nothing pinned, no git.
func (c *Core) checkAskedFiles(ctx context.Context, decisionID string) fileVerdict {
	q := gen.New(c.DB)
	v := fileVerdict{changed: map[string]bool{}}
	d, err := q.GetDecision(ctx, decisionID)
	if err != nil || d.Kind != DecisionApproval || d.Subject != "plan" {
		return v
	}
	p, err := q.GetProject(ctx, d.ProjectID)
	if err != nil {
		return v
	}
	v.repoPath, v.main = p.RepoPath, p.MainBranch
	rows, err := q.ListDecisionTicketPins(ctx, decisionID)
	if err != nil {
		v.err = err
		return v
	}
	var pins []FilePin
	var owner []string
	for _, r := range rows {
		var fs []FilePin
		if err := json.Unmarshal([]byte(r.Files), &fs); err != nil {
			v.err = fmt.Errorf("asked files: %w", err)
			return v
		}
		for _, f := range fs {
			if !PinMatches(f, p) {
				v.changed[r.TicketID] = true // asked against other repo settings
				continue
			}
			pins = append(pins, f)
			owner = append(owner, r.TicketID)
		}
	}
	if len(pins) == 0 {
		return v
	}
	g, head, err := c.openProjectRepo(ctx, p)
	if err != nil {
		v.err = err
		return v
	}
	states, err := CheckFilePins(ctx, g, head, pins)
	if err != nil {
		v.err = err
		return v
	}
	for i, s := range states {
		switch s.State {
		case FileUnchanged, FileRenamed:
		case FileUnchecked:
			v.err = errors.New("a file couldn't be followed")
		default:
			v.changed[owner[i]] = true
		}
	}
	return v
}

// filesUnchanged refuses an Approve of p's ticket when its references or
// pinned files changed since the ask, or the files couldn't be checked.
func filesUnchanged(ctx context.Context, q *gen.Queries, proj gen.Project, p gen.ListDecisionTicketPinsRow, v fileVerdict) error {
	var pins []FilePin
	if err := json.Unmarshal([]byte(p.Files), &pins); err != nil {
		return fmt.Errorf("asked files: %w", err)
	}
	refs, err := q.ListTicketRefs(ctx, p.TicketID)
	if err != nil {
		return fmt.Errorf("list references: %w", err)
	}
	key := TicketKey{Project: p.ProjectKey, Number: p.Number}.String()
	changed := invalid("answer", "A file "+key+" references changed since this was asked; ask again.")
	if !samePaths(pins, refs) {
		return changed
	}
	if len(pins) == 0 {
		return nil
	}
	if proj.RepoPath != v.repoPath || proj.MainBranch != v.main {
		return changed
	}
	if v.err != nil {
		return invalid("answer", "Can't read the repo: "+repoReason(v.err))
	}
	if v.changed[p.TicketID] {
		return changed
	}
	return nil
}
