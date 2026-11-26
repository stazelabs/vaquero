package readiness

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
)

// FileState is one repo file of a ticket against its approval's pins
// (core.FileState): unchanged, changed, renamed (Now is the new path),
// gone (not on main, or no longer referenced), added (referenced since)
// or unchecked.
type FileState = core.FileState

// Files compares an approval's pinned files with main (rulings P2-R3 and
// P2-R5). Check answers one state per pin, in order, for pins all of
// project; a repo it can't read makes them unchecked, never unchanged. It
// is called once per project per CheckAll, never inside a transaction.
type Files interface {
	Check(ctx context.Context, project gen.Project, pins []core.FilePin) ([]FileState, error)
}

// maxCached bounds the states NewFiles keeps; past it the cache starts over.
const maxCached = 10000

// NewFiles is the checker over r: each Check opens the project's repo and
// reads main's head once, and compares only the pins it hasn't already
// compared at that head (by repo, head and pin), so a list read again
// while main hasn't moved reads no blobs. A nil r checks nothing.
func NewFiles(r *gitread.Repos) Files {
	return &gitFiles{repos: r, cache: map[pinKey]FileState{}}
}

type gitFiles struct {
	repos *gitread.Repos
	mu    sync.Mutex
	cache map[pinKey]FileState
}

type pinKey struct{ repo, head, path, commit, blob string }

func (g *gitFiles) Check(ctx context.Context, p gen.Project, pins []core.FilePin) ([]FileState, error) {
	out := unchecked(pins)
	if p.RepoPath == "" || g.repos == nil {
		return out, nil
	}
	repo, err := g.repos.Open(ctx, p.RepoPath, p.MainBranch, p.Remote)
	if err != nil {
		return out, ctx.Err()
	}
	head, err := repo.Head(ctx)
	if err != nil {
		return out, ctx.Err()
	}
	key := func(f core.FilePin) pinKey { return pinKey{repo.Path, head, f.Path, f.Commit, f.Blob} }
	var todo []core.FilePin
	var at []int
	g.mu.Lock()
	for i, f := range pins {
		if st, ok := g.cache[key(f)]; ok {
			out[i] = st
			continue
		}
		todo = append(todo, f)
		at = append(at, i)
	}
	g.mu.Unlock()
	if len(todo) == 0 {
		return out, nil
	}
	states, err := core.CheckFilePins(ctx, repo, head, todo)
	if err != nil {
		return out, ctx.Err()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.cache)+len(todo) > maxCached {
		clear(g.cache)
	}
	for j, st := range states {
		out[at[j]] = st
		if st.State != core.FileUnchecked {
			g.cache[key(todo[j])] = st
		}
	}
	return out, nil
}

// unchecked is every pin unchecked.
func unchecked(pins []core.FilePin) []FileState {
	out := make([]FileState, len(pins))
	for i, f := range pins {
		out[i] = FileState{Path: f.Path, State: core.FileUnchecked}
	}
	return out
}

// fileStates is, for each ticket CheckAll read, its repo files against
// its latest answered plan approval: when that approved, each pin's state
// (a pin whose reference was removed is gone) and each repo-path
// reference not pinned (added, ruling P2-R4); otherwise its repo-path
// references, all added. One read of every reference, then one Check per
// project with anything pinned.
func fileStates(ctx context.Context, q *gen.Queries, files Files, tickets map[string]gen.ListReadinessTicketsRow, latest map[string]answer, every []string) (map[string][]FileState, error) {
	rows, err := q.ListReadinessRefs(ctx, every)
	if err != nil {
		return nil, fmt.Errorf("readiness references: %w", err)
	}
	refs := map[string][]gen.TicketRef{}
	for _, r := range rows {
		refs[r.TicketID] = append(refs[r.TicketID], gen.TicketRef{ID: r.ID, TicketID: r.TicketID, Target: r.Target})
	}
	type slot struct {
		ticket string
		i      int
	}
	pinsOf := map[string][]core.FilePin{} // project id -> pins to check
	slots := map[string][]slot{}
	out := map[string][]FileState{}
	for _, id := range every {
		t := tickets[id]
		now := core.RefPaths(refs[id])
		a := latest[id]
		if !a.approved {
			for _, f := range now {
				out[id] = append(out[id], FileState{Path: f.Path, State: core.FileAdded})
			}
			continue
		}
		var states []FileState
		for _, f := range a.pins.Files {
			if !slices.ContainsFunc(now, func(n core.FilePin) bool { return n.Path == f.Path }) {
				states = append(states, FileState{Path: f.Path, State: core.FileGone})
				continue
			}
			states = append(states, FileState{Path: f.Path, State: core.FileUnchecked})
			pinsOf[t.ProjectID] = append(pinsOf[t.ProjectID], f)
			slots[t.ProjectID] = append(slots[t.ProjectID], slot{id, len(states) - 1})
		}
		for _, f := range now {
			if !slices.ContainsFunc(a.pins.Files, func(p core.FilePin) bool { return p.Path == f.Path }) {
				states = append(states, FileState{Path: f.Path, State: core.FileAdded})
			}
		}
		out[id] = states
	}
	projects := make([]string, 0, len(pinsOf))
	for id := range pinsOf {
		projects = append(projects, id)
	}
	slices.Sort(projects)
	// No checker: the pins stay unchecked.
	var repos []gen.Project
	if files != nil && len(projects) > 0 {
		if repos, err = q.ListReadinessProjects(ctx, projects); err != nil {
			return nil, fmt.Errorf("readiness projects: %w", err)
		}
	}
	for _, p := range repos {
		pid := p.ID
		got, err := files.Check(ctx, p, pinsOf[pid])
		if err != nil {
			return nil, err
		}
		for k, sl := range slots[pid] {
			if k < len(got) {
				out[sl.ticket][sl.i] = got[k]
			}
		}
	}
	for id := range out {
		slices.SortFunc(out[id], func(a, b FileState) int { return strings.Compare(a.Path, b.Path) })
	}
	return out, nil
}

// markFiles adds changed (FilesChanged or EpicFilesChanged) to found
// when a file changed, was removed or was added, and FilesUnchecked when
// one couldn't be checked.
func markFiles(found map[string]bool, files []FileState, changed string) {
	for _, f := range files {
		switch f.State {
		case core.FileUnchanged, core.FileRenamed:
		case core.FileUnchecked:
			found[FilesUnchecked] = true
		default:
			found[changed] = true
		}
	}
}
