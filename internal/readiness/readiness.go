// Package readiness decides whether a ticket passes the agent workflow's
// gate, and why not. It is asked only about tickets whose project has the
// workflow on; with it off, nothing here runs and the ready queue is the
// board's rule alone.
//
// A ticket passes when:
//   - its status is a todo;
//   - it has a tier (a design ticket can pass: runs leave it out, the plan
//     prompt works it);
//   - it has a current plan approval.
//
// The approval that counts for a ticket is the approval row written by the
// latest answered plan approval naming it (decision_tickets). Answered
// otherwise than Approve ("Request changes"), that decision wrote no row,
// and the ticket's plan is not approved: a later answer takes back an
// earlier approval. An approval is current when the ticket's
// RequirementHash and its plan revision still match what it pinned.
//
// Which approvals a ticket needs:
//   - a plan of its own: its own current approval, and nothing of its epic;
//   - no plan of its own but an approval: its own requirement must match,
//     and its parent's own approval must be current too, though it need not
//     list the ticket: editing the epic makes it not ready, and with no
//     approved epic the reason is "Epic's plan not approved.". Without a
//     parent its own approval, pinning plan revision 0, is enough: a small
//     ticket is approved without a plan;
//   - neither: its parent's own current approval, which must list it among
//     the sub-tickets the owner was asked about (ruling L-R8; else "Plan
//     not approved."), with its requirement as pinned there (L-R9; else
//     "Requirement changed since approval."). A sub-ticket made or moved
//     under the epic later leans on no approval until the epic, or the
//     sub-ticket, is approved; one rewritten since, or moved away, edited
//     and moved back, is not ready.
//
// An approval counts only if its decision was asked after the ticket last
// arrived in its project (rulings N-R5 and N-R7: its newest
// ticket_old_keys.moved_at, whatever its parents are now): a ticket moved
// to another project and back, or carried there and back by its epic,
// needs approving again, even once detached, and so does an epic a
// sub-ticket leans on. Only decisions of the
// ticket's own project count at all (N-R1).
//
// An approval holds only under the parent it was asked under (its pins'
// parent, L-R11): a ticket moved to another epic, out of one, or under one
// from having none is not ready ("Epic's plan not approved."). Leaning is
// recursive (L-R10): a parent with no plan of its own must lean on its
// parent in turn, up to the first ancestor with a plan, and a broken link
// further up gives that link's reason ("Epic's requirement changed since
// approval.", "Epic's plan changed since approval.", "Epic's plan not
// approved.").
//
// An approval pins the ticket's repo files (core.FilePin, P2-R2), and
// holds while each one's content on main, renames followed, is what was
// pinned (P2-R3) and the ticket's repo-path references name the same files
// (P2-R4); else "Files changed since approval.", or for an epic leaned on
// "Epic's files changed since approval.". A ticket leaning on its epic
// with repo-path references of its own has them added: its epic's
// approval wasn't asked about them. Files that can't be checked (no repo
// path, no git, a read error) are "Files can't be checked.", never a pass.
// Files (files.go) runs git outside any transaction, once per project per
// CheckAll; a project with nothing pinned runs none.
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

// The reasons, as the API gives them, in the order they are listed.
const (
	NotATodo               = "Not a todo."
	NoTier                 = "No tier."
	PlanNotApproved        = "Plan not approved."
	EpicPlanNotApproved    = "Epic's plan not approved."
	RequirementChanged     = "Requirement changed since approval."
	PlanChanged            = "Plan changed since approval."
	EpicRequirementChanged = "Epic's requirement changed since approval."
	EpicPlanChanged        = "Epic's plan changed since approval."
	FilesChanged           = "Files changed since approval."
	EpicFilesChanged       = "Epic's files changed since approval."
	FilesUnchecked         = "Files can't be checked."
)

// Reasons is every reason, in the order an answer lists them.
var Reasons = []string{NotATodo, NoTier, PlanNotApproved, EpicPlanNotApproved, RequirementChanged, PlanChanged,
	EpicRequirementChanged, EpicPlanChanged, FilesChanged, EpicFilesChanged, FilesUnchecked}

// Readiness is one ticket's answer. Reasons is never nil, and Ready is
// true exactly when it is empty. Files is how the ticket's own repo files
// stand against its approval's pins, by path (nil when it has none).
type Readiness struct {
	Ready   bool
	Reasons []string
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
// ticket whose chain goes deeper leans on nothing it can check, and is not
// ready ("Epic's plan not approved."). Parents never loop (core refuses a
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
		for _, r := range level {
			if p := r.ParentID.String; r.ParentID.Valid && !slices.Contains(ids, p) {
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
	arrived, err := arrivals(ctx, q, every)
	if err != nil {
		return nil, err
	}
	latest := map[string]answer{}
	for _, a := range answerRows {
		if _, seen := latest[a.TicketID]; seen { // latest answer first
			continue
		}
		if a.AskedAt <= arrived[a.TicketID] {
			continue // asked before the ticket last arrived in its project (N-R5)
		}
		an := answer{approved: a.Approved, hash: a.RequirementHash, rev: a.PlanRevision, askedAt: a.AskedAt}
		if a.Approved {
			// Pins that don't parse cover no child, and hold under no
			// parent but none.
			_ = json.Unmarshal([]byte(a.Pins), &an.pins)
		}
		latest[a.TicketID] = an
	}
	s := state{tickets: tickets, plans: plans, latest: latest, arrived: arrived}
	if s.files, err = fileStates(ctx, q, files, tickets, latest, every); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = s.check(r)
	}
	return out, nil
}

// arrivals is when each ticket last arrived in its project, by id: its
// newest ticket_old_keys.moved_at, written for every ticket a move
// re-keyed (the one moved and each sub-ticket it carried), whatever its
// parents are now (ruling N-R7). A ticket that never moved is absent (""
// is before every instant). One read for all of them.
func arrivals(ctx context.Context, q *gen.Queries, ids []string) (map[string]string, error) {
	rows, err := q.ListTicketArrivals(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("readiness arrivals: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.TicketID] = r.ArrivedAt
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
	askedAt  string // when its decision was asked
}

// state is what CheckAll read.
type state struct {
	tickets map[string]gen.ListReadinessTicketsRow
	plans   map[string]int64       // ticket id -> current plan revision
	latest  map[string]answer      // ticket id -> latest answered plan approval
	arrived map[string]string      // ticket id -> when it last arrived in its project
	files   map[string][]FileState // ticket id -> its files against its latest approval
}

func (s state) check(t gen.ListReadinessTicketsRow) Readiness {
	found := map[string]bool{}
	if t.StatusCategory != "todo" {
		found[NotATodo] = true
	}
	if t.Tier == "" {
		found[NoTier] = true
	}
	var files []FileState // its own, when its readiness rests on them
	own, answered := s.latest[t.ID]
	_, ownPlan := s.plans[t.ID]
	hasParent := t.ParentID.Valid
	if ownPlan || answered || !hasParent {
		// Its own approval counts.
		if !answered || !own.approved {
			found[PlanNotApproved] = true
		} else {
			if own.hash != core.RequirementHash(t.Title, t.Body) {
				found[RequirementChanged] = true
			}
			if own.rev != s.plans[t.ID] {
				found[PlanChanged] = true
			}
			files = s.files[t.ID]
			markFiles(found, files, FilesChanged)
			if own.pins.Parent != t.ParentID.String {
				found[EpicPlanNotApproved] = true // moved since (L-R11)
			} else if !ownPlan && hasParent {
				// No plan of its own: its epic's approval must be current
				// too, though it need not list it.
				s.lean(t, 1, false, found, []string{t.ID})
			}
		}
	} else {
		// No plan and no approval of its own: its epic's approval, which
		// must list it. Its own repo files were never asked about: each
		// is added (P2-R4) until it is approved itself.
		s.lean(t, 1, true, found, []string{t.ID})
		files = s.files[t.ID]
		markFiles(found, files, FilesChanged)
	}
	out := Readiness{Reasons: []string{}, Files: files}
	for _, r := range Reasons {
		if found[r] {
			out.Reasons = append(out.Reasons, r)
		}
	}
	out.Ready = len(out.Reasons) == 0
	return out
}

// lean marks in found why t's lean on its parent fails (ruling L-R10).
// The parent's own approval must be current: approved, its requirement and
// plan revision as pinned, and still under the parent it was asked under.
// With listed (t has no approval of its own) it must list t, with t's
// requirement as pinned, and a failure at this first link reads as t's own
// ("Plan not approved.", "Requirement changed since approval."). A parent
// with no plan of its own leans on its parent in turn, up to the first
// ancestor with a plan, each link failing with the epic's reasons.
//
// chain is the tickets from the one leaning up to t. The parent's approval
// counts only if its decision was asked after every ticket in the chain,
// and the parent, last arrived in its project (ruling N-R8): any of them
// moving after the approval was asked voids it for the leaning ticket
// ("Plan not approved."), whatever the parents were then. Each is one map
// lookup; the chain is already loaded.
func (s state) lean(t gen.ListReadinessTicketsRow, depth int, listed bool, found map[string]bool, chain []string) {
	notApproved := EpicPlanNotApproved
	if listed {
		notApproved = PlanNotApproved
	}
	p, ok := s.tickets[t.ParentID.String]
	if !ok || depth > MaxDepth {
		found[EpicPlanNotApproved] = true // beyond the chain read
		return
	}
	a, answered := s.latest[p.ID]
	if !answered || !a.approved {
		found[notApproved] = true
		return
	}
	for _, id := range append(slices.Clone(chain), p.ID) {
		if s.arrived[id] >= a.askedAt {
			found[PlanNotApproved] = true // moved since it was asked (N-R8)
			return
		}
	}
	if listed {
		i := slices.IndexFunc(a.pins.Children, func(c core.PinnedChild) bool { return c.ID == t.ID })
		if i < 0 {
			found[PlanNotApproved] = true // not among the children asked about (L-R8)
			return
		}
		if a.pins.Children[i].RequirementHash != core.RequirementHash(t.Title, t.Body) {
			found[RequirementChanged] = true // L-R9
		}
	}
	if a.hash != core.RequirementHash(p.Title, p.Body) {
		found[EpicRequirementChanged] = true
	}
	if a.rev != s.plans[p.ID] {
		found[EpicPlanChanged] = true
	}
	markFiles(found, s.files[p.ID], EpicFilesChanged)
	if a.pins.Parent != p.ParentID.String {
		found[EpicPlanNotApproved] = true // the epic moved since (L-R11)
		return
	}
	if _, plan := s.plans[p.ID]; !plan && p.ParentID.Valid {
		s.lean(p, depth+1, false, found, append(slices.Clone(chain), p.ID))
	}
}
