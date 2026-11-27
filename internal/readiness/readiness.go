// Package readiness decides whether a ticket passes the agent workflow's
// gate, why not, and what to do about it. It is asked only about tickets
// whose project has the workflow on; with it off, nothing here runs and the
// ready queue is the board's rule alone.
//
// The one rule: a ticket is ready when it is a todo the board would call
// ready and its plan is approved. Readiness is asked of todos; any other
// ticket is not ready, with no reason (the board already shows why). The
// tier gates nothing: an empty tier counts as standard.
//
// Which approval counts (approval(t)): when t has a plan of its own, or a
// plan approval was answered naming t itself, the latest answered plan
// approval naming t. Otherwise the latest answered plan approval of the
// nearest ancestor with a plan or with an answered plan approval naming
// it (ruling R-Q12), which must list t among the descendants it covered
// (core's childPins: every descendant without a plan of its own, reached
// through descendants without one). With no such ancestor, t's own
// approval is asked for. A cancelled ticket between t and that
// ancestor, or the ancestor itself cancelled, covers nothing; a done one
// still covers. Only decisions of the ticket's own project count, and a
// move to another project deletes the moved tickets' approvals (core), so
// a ticket moved away and back needs approving again.
//
// The approval is current when it answered Approve and nothing it pinned
// changed since: the ticket's requirement (title and description,
// core.RequirementHash), its plan revision, its parent, and its repo files
// (files.go); for a covered descendant also its own requirement and
// parent as pinned. A covered descendant's own repo-path references were
// never asked about, so it needs its own approval.
//
// One reason at most, as an action:
//   - "Ask for plan approval." (Action ask; Epic names the ancestor to ask
//     on, when it is one);
//   - "Changed since approval: {what}. Ask again." (ask_again);
//   - "Its epic changed since approval: {what}. Ask again." (ask_again,
//     Epic set);
//   - "Can't check its files: {why}." (never a pass; ask_again when {why}
//     is that the repository settings changed, Epic set when the files are
//     the epic's, else no action).
//
// {what} is the first that changed of: the description, the plan, its
// parent, a file path.
//
// The board's conditions (unassigned, unclaimed, no open blocker or
// sub-ticket) are not reasons here: the board query decides them, and the
// ready queue needs both.
package readiness

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// The reasons. AskForApproval is given as it is; the others through
// Changed, EpicChanged and Unchecked.
const (
	AskForApproval = "Ask for plan approval."
)

// Changed is the reason for a ticket whose approval is stale: what changed
// since (WhatDescription, WhatPlan, WhatParent or a file's path).
func Changed(what string) string { return "Changed since approval: " + what + ". Ask again." }

// EpicChanged is the reason for a ticket whose epic's approval is stale.
func EpicChanged(what string) string {
	return "Its epic changed since approval: " + what + ". Ask again."
}

// Unchecked is the reason for an approval whose files can't be checked.
func Unchecked(why string) string { return "Can't check its files: " + why + "." }

// Reasons is the four forms a reason takes.
var Reasons = []string{AskForApproval, Changed("{what}"), EpicChanged("{what}"), Unchecked("{why}")}

// What changed since an approval, besides a file's path.
const (
	WhatDescription = "the description"
	WhatPlan        = "the plan"
	WhatParent      = "its parent"
)

// Why files can't be checked.
const (
	WhyNoGit       = "git isn't installed"
	WhyOldGit      = "git 2.31 or later is needed"
	WhyGitVersion  = "git's version couldn't be read"
	WhyNoRepo      = "no repository is set"
	WhyRepoChanged = "the repository settings changed since approval"
	WhyUnreadable  = "the repository can't be read"
)

// The actions a reason asks for (Readiness.Action).
const (
	ActionAsk      = "ask"       // ask for a plan approval
	ActionAskAgain = "ask_again" // ask for a plan approval again
)

// Readiness is one ticket's answer. Reasons is never nil and holds at most
// one reason; a ready ticket has none, and neither does a ticket that
// isn't a todo (not ready all the same). Action is what to do (ActionAsk
// or ActionAskAgain; "" when ready, or when the files can't be checked
// for any reason but a changed repo setting),
// and Epic the key of the ancestor to ask on, when that is where to ask
// ("" for the ticket itself). Files is how the ticket's own repo files
// stand against its approval's pins, by path (nil when it has none).
type Readiness struct {
	Ready   bool
	Reasons []string
	Action  string
	Epic    string
	Files   []FileState
}

// Check is t's readiness, read through q (the reader), its pinned files
// through files (nil: they can't be checked).
func Check(ctx context.Context, q *gen.Queries, files Files, t gen.Ticket) (Readiness, error) {
	all, err := CheckAll(ctx, q, files, []string{t.ID})
	if err != nil {
		return Readiness{}, err
	}
	r, ok := all[t.ID]
	if !ok {
		return Readiness{}, fmt.Errorf("readiness: no ticket %s", t.ID)
	}
	return r, nil
}

// MaxDepth bounds the ancestor chain CheckAll reads, one read a level. A
// ticket whose nearest ancestor with a plan is deeper has none it can
// check, and is asked about itself. Parents never loop (core refuses a
// cycle), so the bound only caps the reads.
const MaxDepth = 16

// CheckAll is the readiness of each ticket named in ids that exists. It
// reads the tickets, their ancestors a level at a time (at most MaxDepth
// levels, however many tickets), then the plans, the plan approvals and
// the references of all of them: never a read per ticket. The approvals'
// pinned files go to files once per project (nil: pins unchecked); a
// project with nothing pinned runs no git.
func CheckAll(ctx context.Context, q *gen.Queries, files Files, ids []string) (map[string]Readiness, error) {
	out := map[string]Readiness{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListReadinessTickets(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("readiness tickets: %w", err)
	}
	tickets := map[string]gen.ListReadinessTicketsRow{}
	for _, r := range rows {
		tickets[r.ID] = r
	}
	missing := func(level []gen.ListReadinessTicketsRow) []string {
		var ids []string
		seen := map[string]bool{}
		for _, r := range level {
			if p := r.ParentID.String; r.ParentID.Valid && !seen[p] {
				seen[p] = true
				if _, ok := tickets[p]; !ok {
					ids = append(ids, p)
				}
			}
		}
		return ids
	}
	level := rows
	for range MaxDepth {
		parents := missing(level)
		if len(parents) == 0 {
			break
		}
		if level, err = q.ListReadinessTickets(ctx, parents); err != nil {
			return nil, fmt.Errorf("readiness ancestors: %w", err)
		}
		for _, r := range level {
			tickets[r.ID] = r
		}
	}
	every := make([]string, 0, len(tickets))
	for id := range tickets {
		every = append(every, id)
	}
	slices.Sort(every)
	planRows, err := q.ListPlanRevisionsForTickets(ctx, every)
	if err != nil {
		return nil, fmt.Errorf("readiness plans: %w", err)
	}
	plans := map[string]int64{}
	for _, p := range planRows {
		plans[p.TicketID] = p.Revision
	}
	answerRows, err := q.ListPlanApprovalAnswers(ctx, every)
	if err != nil {
		return nil, fmt.Errorf("readiness approvals: %w", err)
	}
	latest := map[string]answer{}
	for _, a := range answerRows {
		if _, seen := latest[a.TicketID]; seen { // latest answer first
			continue
		}
		an := answer{approved: a.Approved, hash: a.RequirementHash, rev: a.PlanRevision}
		if a.Approved {
			// Pins that don't parse cover no descendant, and hold under
			// no parent but none.
			_ = json.Unmarshal([]byte(a.Pins), &an.pins)
		}
		latest[a.TicketID] = an
	}
	s := state{tickets: tickets, plans: plans, latest: latest}
	if s.files, s.why, err = fileStates(ctx, q, files, tickets, latest, every); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = s.check(r)
	}
	return out, nil
}

// answer is a ticket's latest answered plan approval: whether it approved,
// and then what its approval row pinned.
type answer struct {
	approved bool
	hash     string
	rev      int64
	pins     core.ApprovalPins
}

// state is what CheckAll read.
type state struct {
	tickets map[string]gen.ListReadinessTicketsRow
	plans   map[string]int64       // ticket id -> current plan revision
	latest  map[string]answer      // ticket id -> latest answered plan approval
	files   map[string][]FileState // ticket id -> its files against its latest approval
	why     map[string]string      // ticket id -> why its pinned files can't be checked
}

func (s state) check(t gen.ListReadinessTicketsRow) Readiness {
	out := Readiness{Reasons: []string{}, Files: s.files[t.ID]}
	if t.StatusCategory != "todo" {
		return out
	}
	_, ownPlan := s.plans[t.ID]
	if _, answered := s.latest[t.ID]; !ownPlan && !answered {
		if a, ok := s.approver(t); ok {
			return s.covered(t, a, out)
		}
		return out.ask("")
	}
	own := s.latest[t.ID]
	if !own.approved {
		return out.ask("")
	}
	if what := s.stale(t, own); what != "" {
		return out.again(Changed(what), "")
	}
	if why := s.why[t.ID]; why != "" {
		return out.unchecked(why, "")
	}
	out.Ready = true
	return out
}

// approver is the nearest ancestor of t with a plan or an answered plan
// approval naming it (ruling R-Q12: the own-route condition, applied to
// ancestors), when the chain to it was read and no ticket on it, the
// ancestor included, is cancelled.
func (s state) approver(t gen.ListReadinessTicketsRow) (gen.ListReadinessTicketsRow, bool) {
	for depth := 1; t.ParentID.Valid && depth <= MaxDepth; depth++ {
		p, ok := s.tickets[t.ParentID.String]
		if !ok || p.StatusCategory == "cancelled" {
			return p, false
		}
		_, plan := s.plans[p.ID]
		if _, answered := s.latest[p.ID]; plan || answered {
			return p, true
		}
		t = p
	}
	return t, false
}

// covered is t's readiness on its epic e's approval, which must list t,
// and every ticket between them, each as pinned. approver read the whole
// chain from t up to e, so the walk reads nothing.
func (s state) covered(t, e gen.ListReadinessTicketsRow, out Readiness) Readiness {
	epic := core.TicketKey{Project: e.ProjectKey, Number: e.Number}.String()
	if len(out.Files) > 0 {
		return out.ask("") // its own repo files need an approval of its own
	}
	a, ok := s.latest[e.ID]
	if !ok || !a.approved {
		return out.ask(epic)
	}
	i := slices.IndexFunc(a.pins.Children, func(c core.PinnedChild) bool { return c.ID == t.ID })
	if i < 0 {
		return out.ask(epic) // not among the descendants it was asked about
	}
	pin := a.pins.Children[i]
	parent := pin.Parent
	if parent == "" {
		parent = e.ID // pinned before parents were: the epic's own sub-ticket
	}
	switch {
	case pin.RequirementHash != core.RequirementHash(t.Title, t.Body):
		return out.again(Changed(WhatDescription), epic)
	case parent != t.ParentID.String:
		return out.again(Changed(WhatParent), epic)
	}
	// Every covered ticket between t and e must be as pinned too (R-Q4:
	// only this chain, not covered tickets elsewhere under e).
	for m := s.tickets[t.ParentID.String]; m.ID != e.ID; m = s.tickets[m.ParentID.String] {
		key := core.TicketKey{Project: m.ProjectKey, Number: m.Number}.String()
		j := slices.IndexFunc(a.pins.Children, func(c core.PinnedChild) bool { return c.ID == m.ID })
		if j < 0 {
			return out.ask(epic) // not among the descendants it was asked about
		}
		mp := a.pins.Children[j]
		if mp.Parent == "" {
			mp.Parent = e.ID
		}
		switch {
		case mp.RequirementHash != core.RequirementHash(m.Title, m.Body):
			return out.again(EpicChanged(key+"'s description"), epic)
		case mp.Parent != m.ParentID.String:
			return out.again(EpicChanged(key+"'s parent"), epic)
		}
	}
	if what := s.stale(e, a); what != "" {
		return out.again(EpicChanged(what), epic)
	}
	if why := s.why[e.ID]; why != "" {
		return out.unchecked(why, epic)
	}
	out.Ready = true
	return out
}

// stale is the first thing approval a pinned of t that changed since: its
// description, its plan, its parent, then a file's path; "" for none.
func (s state) stale(t gen.ListReadinessTicketsRow, a answer) string {
	switch {
	case a.hash != core.RequirementHash(t.Title, t.Body):
		return WhatDescription
	case a.rev != s.plans[t.ID]:
		return WhatPlan
	case a.pins.Parent != t.ParentID.String:
		return WhatParent
	}
	for _, f := range s.files[t.ID] {
		switch f.State {
		case core.FileUnchanged, core.FileRenamed, core.FileUnchecked:
		default:
			return f.Path
		}
	}
	return ""
}

func (r Readiness) ask(epic string) Readiness {
	r.Reasons, r.Action, r.Epic = []string{AskForApproval}, ActionAsk, epic
	return r
}

func (r Readiness) again(reason, epic string) Readiness {
	r.Reasons, r.Action, r.Epic = []string{reason}, ActionAskAgain, epic
	return r
}

// unchecked is "Can't check its files: {why}.", with no action but for a
// changed repo setting, where asking again (on epic, when the files are
// its) pins the files as the repo is set now (ruling R-Q16).
func (r Readiness) unchecked(why, epic string) Readiness {
	r.Reasons = []string{Unchecked(why)}
	if why == WhyRepoChanged {
		r.Action, r.Epic = ActionAskAgain, epic
	}
	return r
}
