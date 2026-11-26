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
// arrived in its project (ruling N-R5): a ticket moved to another project
// and back, or carried there and back by its epic, needs approving again,
// and so does an epic a sub-ticket leans on. Only decisions of the
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
// The board's conditions (unassigned, unclaimed, no open blocker or
// sub-ticket) are not reasons here: the board query decides them, and the
// ready queue needs both.
package readiness

import (
	"context"
	"database/sql"
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
)

// Reasons is every reason, in the order an answer lists them.
var Reasons = []string{NotATodo, NoTier, PlanNotApproved, EpicPlanNotApproved, RequirementChanged, PlanChanged, EpicRequirementChanged, EpicPlanChanged}

// Readiness is one ticket's answer. Reasons is never nil, and Ready is
// true exactly when it is empty.
type Readiness struct {
	Ready   bool
	Reasons []string
}

// Check is t's readiness, read through q (the reader).
func Check(ctx context.Context, q *gen.Queries, t gen.Ticket) (Readiness, error) {
	all, err := CheckAll(ctx, q, []string{t.ID})
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
// levels, however many tickets), then the plans and the plan approvals of
// all of them: never a read per ticket.
func CheckAll(ctx context.Context, q *gen.Queries, ids []string) (map[string]Readiness, error) {
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
	arrived, err := arrivals(ctx, q, tickets, every)
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
		an := answer{approved: a.Approved, hash: a.RequirementHash, rev: a.PlanRevision}
		if a.Approved {
			// Pins that don't parse cover no child, and hold under no
			// parent but none.
			_ = json.Unmarshal([]byte(a.Pins), &an.pins)
		}
		latest[a.TicketID] = an
	}
	s := state{tickets: tickets, plans: plans, latest: latest}
	for _, r := range rows {
		out[r.ID] = s.check(r)
	}
	return out, nil
}

// arrivals is when each ticket last arrived in its project, by id: the
// newest ticket.moved event on the ticket itself, or on an ancestor whose
// move carried it along (its payload.ticket_ids lists the ticket). A
// ticket that never moved is absent ("", before every instant). One read
// for all of them, through events_ticket (ruling N-R5).
func arrivals(ctx context.Context, q *gen.Queries, tickets map[string]gen.ListReadinessTicketsRow, ids []string) (map[string]string, error) {
	out := map[string]string{}
	keys := make([]sql.NullString, len(ids))
	for i, id := range ids {
		keys[i] = sql.NullString{String: id, Valid: true}
	}
	moves, err := q.ListTicketMoves(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("readiness moves: %w", err)
	}
	if len(moves) == 0 {
		return out, nil
	}
	type move struct {
		at      string
		carried []string
	}
	byTicket := map[string][]move{}
	for _, m := range moves {
		mv := move{at: m.At}
		// A payload that doesn't parse carried nothing.
		_ = json.Unmarshal([]byte(m.Carried), &mv.carried)
		byTicket[m.TicketID.String] = append(byTicket[m.TicketID.String], mv)
	}
	later := func(id, at string) {
		if at > out[id] {
			out[id] = at
		}
	}
	for id, t := range tickets {
		for _, m := range byTicket[id] {
			later(id, m.at)
		}
		// Up the chain this read knows (at most MaxDepth levels).
		seen := map[string]bool{id: true}
		for p := t.ParentID; p.Valid && !seen[p.String]; {
			seen[p.String] = true
			for _, m := range byTicket[p.String] {
				if slices.Contains(m.carried, id) {
					later(id, m.at)
				}
			}
			up, ok := tickets[p.String]
			if !ok {
				break
			}
			p = up.ParentID
		}
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
	plans   map[string]int64  // ticket id -> current plan revision
	latest  map[string]answer // ticket id -> latest answered plan approval
}

func (s state) check(t gen.ListReadinessTicketsRow) Readiness {
	found := map[string]bool{}
	if t.StatusCategory != "todo" {
		found[NotATodo] = true
	}
	if t.Tier == "" {
		found[NoTier] = true
	}
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
			if own.pins.Parent != t.ParentID.String {
				found[EpicPlanNotApproved] = true // moved since (L-R11)
			} else if !ownPlan && hasParent {
				// No plan of its own: its epic's approval must be current
				// too, though it need not list it.
				s.lean(t, 1, false, found)
			}
		}
	} else {
		// No plan and no approval of its own: its epic's approval, which
		// must list it.
		s.lean(t, 1, true, found)
	}
	out := Readiness{Reasons: []string{}}
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
func (s state) lean(t gen.ListReadinessTicketsRow, depth int, listed bool, found map[string]bool) {
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
	if a.pins.Parent != p.ParentID.String {
		found[EpicPlanNotApproved] = true // the epic moved since (L-R11)
		return
	}
	if _, plan := s.plans[p.ID]; !plan && p.ParentID.Valid {
		s.lean(p, depth+1, false, found)
	}
}
