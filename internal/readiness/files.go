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

// maxCached bounds the states NewFiles keeps for one repo's head; past it
// that repo's cache starts over.
const maxCached = 10000

// NewFiles is the checker over r. Each Check opens the project's repo and
// reads main's head once; every pin it has compared at that head (unchecked
// ones too when the answer will repeat: a pinned commit no longer in the
// repo) is answered from its cache (by pin) for as long as main stays
// there, so a list read again reads no blobs and walks no history; a
// transient failure (a timeout, a process error) is never kept. A
// new head starts that repo's cache over. Uncached pins go to
// core.CheckFilePins together: two ls-trees and one history walk per
// distinct pinned commit. A nil r checks nothing.
func NewFiles(r *gitread.Repos) Files {
	return &gitFiles{repos: r, cache: map[string]*headCache{}}
}

type gitFiles struct {
	repos *gitread.Repos
	mu    sync.Mutex
	cache map[string]*headCache // repo path -> its states at one head
}

type headCache struct {
	head   string
	states map[pinKey]FileState
}

type pinKey struct{ path, commit, blob string }

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
	key := func(f core.FilePin) pinKey { return pinKey{f.Path, f.Commit, f.Blob} }
	var todo []core.FilePin
	var at []int
	g.mu.Lock()
	hc := g.cache[repo.Path]
	if hc == nil || hc.head != head {
		hc = &headCache{head: head, states: map[pinKey]FileState{}}
		g.cache[repo.Path] = hc
	}
	for i, f := range pins {
		if st, ok := hc.states[key(f)]; ok {
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
	if ctx.Err() != nil {
		return out, ctx.Err() // nothing kept from a cancelled read
	}
	if err != nil {
		// A transient failure (gitread's timeout, a process error): the
		// pins are unchecked this time and asked again next time; only
		// CheckFilePins' answers, which hold while main stays put, are kept.
		return out, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(hc.states)+len(todo) > maxCached {
		clear(hc.states)
	}
	for j, st := range states {
		out[at[j]] = st
		hc.states[key(todo[j])] = st
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
		// A pin taken against other repo settings stays unchecked (M3).
		var pins []core.FilePin
		var at []slot
		for k, f := range pinsOf[p.ID] {
			if core.PinMatches(f, p) {
				pins = append(pins, f)
				at = append(at, slots[p.ID][k])
			}
		}
		if len(pins) == 0 {
			continue
		}
		got, err := files.Check(ctx, p, pins)
		if err != nil {
			return nil, err
		}
		for k, sl := range at {
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
