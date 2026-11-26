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
//     and it leans on its parent's own current approval too (the epic's
//     requirement and plan revision must match), so editing the epic makes
//     it not ready; without one, "Epic's plan not approved.". Without a
//     parent its own approval, pinning plan revision 0, is enough: a small
//     ticket is approved without a plan;
//   - neither: its parent's own current approval alone ("Plan not
//     approved." without one).
//
// An epic's approval covers only the sub-tickets it had when approved (its
// pins' children, ruling L-R8): a sub-ticket made or moved under it later
// leans on no approval until the epic, or the sub-ticket, is approved.
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

// CheckAll is the readiness of each ticket named in ids that exists, in
// four reads whatever the count: the tickets, their parents, the plans and
// the plan approvals of both.
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
	var parents []string
	for _, r := range rows {
		if p := r.ParentID.String; r.ParentID.Valid && !slices.Contains(parents, p) {
			if _, ok := tickets[p]; !ok {
				parents = append(parents, p)
			}
		}
	}
	if len(parents) > 0 {
		prows, err := q.ListReadinessTickets(ctx, parents)
		if err != nil {
			return nil, fmt.Errorf("readiness parents: %w", err)
		}
		for _, r := range prows {
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
	latest := map[string]gen.ListPlanApprovalAnswersRow{}
	for _, a := range answerRows {
		if _, seen := latest[a.TicketID]; !seen { // latest answer first
			latest[a.TicketID] = a
		}
	}
	s := state{tickets: tickets, plans: plans, latest: latest}
	for _, r := range rows {
		out[r.ID] = s.check(r)
	}
	return out, nil
}

// state is what CheckAll read.
type state struct {
	tickets map[string]gen.ListReadinessTicketsRow
	plans   map[string]int64                          // ticket id -> current plan revision
	latest  map[string]gen.ListPlanApprovalAnswersRow // ticket id -> latest answered plan approval
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
	parent, hasParent := s.tickets[t.ParentID.String]
	hasParent = hasParent && t.ParentID.Valid
	_, ownPlan := s.plans[t.ID]
	switch {
	case ownPlan || answered || !hasParent:
		// Its own approval counts.
		s.current(t, own, answered, found, PlanNotApproved, RequirementChanged, PlanChanged)
		if !ownPlan && answered && hasParent {
			// No plan of its own: it leans on the epic's approval too.
			s.epic(t, parent, found, EpicPlanNotApproved)
		}
	default:
		// No plan and no approval of its own: the epic's alone.
		s.epic(t, parent, found, PlanNotApproved)
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

// epic marks in found why parent's latest answer is not a current
// approval that covers child: notApproved when it isn't an approval or
// child wasn't one of its sub-tickets then (ruling L-R8), else the epic's
// changed requirement or plan.
func (s state) epic(child, parent gen.ListReadinessTicketsRow, found map[string]bool, notApproved string) {
	a, answered := s.latest[parent.ID]
	if !answered || !a.Approved || !covers(a.Pins, child.ID) {
		found[notApproved] = true
		return
	}
	s.current(parent, a, true, found, notApproved, EpicRequirementChanged, EpicPlanChanged)
}

// covers reports whether an approval's pins name id among its children.
// Pins that don't parse, or name no children, cover none.
func covers(pins, id string) bool {
	var p core.ApprovalPins
	if err := json.Unmarshal([]byte(pins), &p); err != nil {
		return false
	}
	return slices.Contains(p.Children, id)
}

// current marks in found why t's latest answer a is not a current approval
// of it: notApproved when it isn't one, else its requirement or plan
// changed since (named by reqChanged and planChanged).
func (s state) current(t gen.ListReadinessTicketsRow, a gen.ListPlanApprovalAnswersRow, answered bool, found map[string]bool, notApproved, reqChanged, planChanged string) {
	if !answered || !a.Approved {
		found[notApproved] = true
		return
	}
	if a.RequirementHash != core.RequirementHash(t.Title, t.Body) {
		found[reqChanged] = true
	}
	if a.PlanRevision != s.plans[t.ID] {
		found[planChanged] = true
	}
}
