package readiness_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
)

var bg = context.Background()

// world is a workflow project on a fresh database whose clock moves a
// second each time it is read, so answers are ordered.
type world struct {
	t     *testing.T
	c     *core.Core
	d     dbtest.DB
	owner gen.Actor
	p     gen.Project
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d := dbtest.New(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	c := core.New(d.W, func() time.Time { now = now.Add(time.Second); return now })
	owner, err := c.EnsureOwner(bg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if p, err = c.UpdateProject(bg, owner.ID, p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
	return &world{t: t, c: c, d: d, owner: owner, p: p}
}

func (w *world) ticket(in core.TicketInput) gen.Ticket {
	w.t.Helper()
	tk, err := w.c.CreateTicket(bg, w.owner.ID, w.p.ID, in)
	if err != nil {
		w.t.Fatal(err)
	}
	return tk
}

func (w *world) plan(tk gen.Ticket, body string, base int64) {
	w.t.Helper()
	if _, err := w.c.SavePlan(bg, w.owner.ID, tk.ID, body, base); err != nil {
		w.t.Fatal(err)
	}
}

// answer asks a plan approval of the tickets and answers it with option.
func (w *world) answer(option string, tks ...gen.Ticket) {
	w.t.Helper()
	ids := make([]string, len(tks))
	for i, tk := range tks {
		ids[i] = tk.ID
	}
	d, err := w.c.AskDecision(bg, w.owner.ID, core.DecisionInput{
		ProjectID: w.p.ID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?",
	})
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.c.AnswerDecision(bg, w.owner.ID, d.ID, core.Answer{Option: option}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) approve(tks ...gen.Ticket) { w.t.Helper(); w.answer(core.ApproveLabel, tks...) }

func (w *world) edit(tk gen.Ticket, patch core.TicketPatch) {
	w.t.Helper()
	if _, err := w.c.PatchTicket(bg, w.owner.ID, tk.ID, patch, 0); err != nil {
		w.t.Fatal(err)
	}
}

// check is tk's readiness, through Check and through CheckAll, which must
// agree.
func (w *world) check(tk gen.Ticket) readiness.Readiness {
	w.t.Helper()
	cur, err := w.d.RQ.GetTicket(bg, tk.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	one, err := readiness.Check(bg, w.d.RQ, cur)
	if err != nil {
		w.t.Fatal(err)
	}
	all, err := readiness.CheckAll(bg, w.d.RQ, []string{tk.ID})
	if err != nil {
		w.t.Fatal(err)
	}
	if got := all[tk.ID]; got.Ready != one.Ready || !slices.Equal(got.Reasons, one.Reasons) {
		w.t.Fatalf("Check %+v, CheckAll %+v", one, got)
	}
	return one
}

func expect(t *testing.T, got readiness.Readiness, reasons ...string) {
	t.Helper()
	if got.Ready != (len(reasons) == 0) || !slices.Equal(got.Reasons, reasons) || got.Reasons == nil {
		t.Fatalf("got %+v, want reasons %q", got, reasons)
	}
}

func TestReadyWithApprovedPlanAndTier(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns", Body: "Show columns.", Tier: "standard"})
	w.plan(tk, "1. Columns.", 0)
	expect(t, w.check(tk), readiness.PlanNotApproved)
	w.approve(tk)
	expect(t, w.check(tk))
	// A move, a claim and other changes outside the requirement keep it.
	w.edit(tk, core.TicketPatch{Priority: new(int64(2)), Tags: []string{"ui"}, SetTags: true})
	expect(t, w.check(tk))
}

func TestNoTierNotReady(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns"})
	w.plan(tk, "1. Columns.", 0)
	w.approve(tk)
	expect(t, w.check(tk), readiness.NoTier)
	w.edit(tk, core.TicketPatch{Tier: new("design")})
	expect(t, w.check(tk)) // a design ticket can be ready
}

func TestNotATodoNotReady(t *testing.T) {
	w := newWorld(t)
	st, err := w.d.RQ.FirstStatusInCategory(bg, gen.FirstStatusInCategoryParams{ProjectID: w.p.ID, Category: "backlog"})
	if err != nil {
		t.Fatal(err)
	}
	tk := w.ticket(core.TicketInput{Title: "Columns", StatusID: st.ID})
	// Every reason, in order.
	expect(t, w.check(tk), readiness.NotATodo, readiness.NoTier, readiness.PlanNotApproved)
}

func TestEditedRequirementNotReady(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns", Body: "Show columns.", Tier: "light"})
	w.plan(tk, "1. Columns.", 0)
	w.approve(tk)
	w.edit(tk, core.TicketPatch{Body: new("Show columns and rails.")})
	expect(t, w.check(tk), readiness.RequirementChanged)
	w.edit(tk, core.TicketPatch{Body: new("Show columns.")})
	expect(t, w.check(tk)) // the requirement as approved again
	w.edit(tk, core.TicketPatch{Title: new("Board columns")})
	expect(t, w.check(tk), readiness.RequirementChanged)
	w.approve(tk)
	expect(t, w.check(tk))
}

func TestRevisedPlanNotReady(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns", Tier: "light"})
	w.plan(tk, "1. Columns.", 0)
	w.approve(tk)
	w.plan(tk, "1. Columns.\n2. Rails.", 1)
	expect(t, w.check(tk), readiness.PlanChanged)
	w.edit(tk, core.TicketPatch{Body: new("Changed.")})
	expect(t, w.check(tk), readiness.RequirementChanged, readiness.PlanChanged)
}

// Ruling L-R4: the latest answered plan approval counts, so "Request
// changes" after an approval takes it back.
func TestRequestChangesAfterApprovalNotReady(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns", Tier: "light"})
	w.plan(tk, "1. Columns.", 0)
	w.approve(tk)
	expect(t, w.check(tk))
	w.answer("Request changes", tk)
	expect(t, w.check(tk), readiness.PlanNotApproved)
	w.approve(tk)
	expect(t, w.check(tk))
}

// Ruling L-R5, no plan and no approval of its own: the epic's approval
// covers the child.
func TestChildInheritsEpicApproval(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	child := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	w.plan(epic, "1. Columns.\n2. Rails.", 0)
	expect(t, w.check(child), readiness.PlanNotApproved)
	w.approve(epic)
	expect(t, w.check(child))
	// The epic's approval pinned the child's requirement too (ruling L-R9).
	w.edit(child, core.TicketPatch{Body: new("Columns, sorted.")})
	expect(t, w.check(child), readiness.RequirementChanged)
	w.approve(epic)
	expect(t, w.check(child))
	// The epic's latest answer counts for the child too.
	w.answer("Request changes", epic)
	expect(t, w.check(child), readiness.PlanNotApproved)
}

// Review Focus 4: editing the epic's requirement or plan makes every child
// that leans on its approval not ready, with the epic's reason.
func TestEditedEpicMakesChildrenNotReady(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	inherits := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	grouped := w.ticket(core.TicketInput{Title: "Rails", ParentID: epic.ID, Tier: "light"})
	w.plan(epic, "1. Columns.\n2. Rails.", 0)
	w.approve(epic)          // inherits: no approval of its own
	w.approve(epic, grouped) // grouped: approved with the epic, plan_revision 0
	expect(t, w.check(inherits))
	expect(t, w.check(grouped))

	w.edit(epic, core.TicketPatch{Body: new("A board with rails.")})
	expect(t, w.check(inherits), readiness.EpicRequirementChanged)
	expect(t, w.check(grouped), readiness.EpicRequirementChanged)

	w.approve(epic, grouped)
	expect(t, w.check(inherits))
	expect(t, w.check(grouped))

	w.plan(epic, "1. Columns.\n2. Rails.\n3. Filters.", 1)
	expect(t, w.check(inherits), readiness.EpicPlanChanged)
	expect(t, w.check(grouped), readiness.EpicPlanChanged)

	// Batched, every child gets its own answer.
	all, err := readiness.CheckAll(bg, w.d.RQ, []string{inherits.ID, grouped.ID, epic.ID})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, all[inherits.ID], readiness.EpicPlanChanged)
	expect(t, all[grouped.ID], readiness.EpicPlanChanged)
	expect(t, all[epic.ID], readiness.NoTier, readiness.PlanChanged)
}

// Ruling L-R5, no plan but an approval of its own: its requirement must
// match, and it still leans on the epic's own current approval.
func TestChildOwnApprovalStillLeansOnEpic(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	child := w.ticket(core.TicketInput{Title: "Rails", ParentID: epic.ID, Tier: "light"})
	w.plan(epic, "1. Rails.", 0)
	w.approve(child)                                         // its own approval, the epic not yet approved
	expect(t, w.check(child), readiness.EpicPlanNotApproved) // ruling L-R6
	w.approve(epic)
	expect(t, w.check(child))
	w.edit(child, core.TicketPatch{Body: new("Rails, folded.")})
	expect(t, w.check(child), readiness.RequirementChanged)
	w.edit(epic, core.TicketPatch{Title: new("Board, again")})
	expect(t, w.check(child), readiness.RequirementChanged, readiness.EpicRequirementChanged)
	// Its own latest answer counts: a request for changes takes it back.
	w.approve(epic)
	w.approve(child)
	expect(t, w.check(child))
	w.answer("Request changes", child)
	expect(t, w.check(child), readiness.PlanNotApproved)
}

// Ruling L-R5, a plan of its own: its own approval counts, and the epic's
// edits don't touch it.
func TestChildOwnPlanTakesPrecedence(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	child := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	w.plan(epic, "1. Columns.", 0)
	w.approve(epic)
	w.plan(child, "1. Sorted columns.", 0)
	// Its own plan isn't approved, so the epic's approval doesn't cover it.
	expect(t, w.check(child), readiness.PlanNotApproved)
	w.approve(child)
	expect(t, w.check(child))
	w.edit(epic, core.TicketPatch{Body: new("A board with rails.")})
	w.plan(epic, "1. Columns.\n2. Rails.", 1)
	expect(t, w.check(child))
	w.plan(child, "1. Sorted columns.\n2. Folded.", 1)
	expect(t, w.check(child), readiness.PlanChanged)
}

// Ruling L-R5, no parent and no plan: a small ticket's own approval
// (plan_revision 0) counts.
func TestSmallTicketApprovedWithoutPlan(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Typo", Tier: "light"})
	expect(t, w.check(tk), readiness.PlanNotApproved)
	w.approve(tk)
	expect(t, w.check(tk))
	w.plan(tk, "1. Fix it.", 0) // a plan written after: approve it
	expect(t, w.check(tk), readiness.PlanChanged)
}

func TestCheckAllEmpty(t *testing.T) {
	w := newWorld(t)
	all, err := readiness.CheckAll(bg, w.d.RQ, nil)
	if err != nil || len(all) != 0 {
		t.Fatalf("%v %v", all, err)
	}
}

// Ruling L-R8: an epic's approval covers only the sub-tickets it had when
// it was approved.
func TestEpicApprovalCoversOnlyItsChildrenThen(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	present := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	moved := w.ticket(core.TicketInput{Title: "Rails", Tier: "light"})
	w.plan(epic, "1. Columns.", 0)
	w.approve(epic)
	expect(t, w.check(present)) // present at approval: still covered

	later := w.ticket(core.TicketInput{Title: "Filters", ParentID: epic.ID, Tier: "light"})
	expect(t, w.check(later), readiness.PlanNotApproved)
	w.edit(moved, core.TicketPatch{ParentID: new(epic.ID)})
	expect(t, w.check(moved), readiness.PlanNotApproved)

	// Approving the epic again takes them in.
	w.approve(epic)
	expect(t, w.check(later))
	expect(t, w.check(moved))
}

// Ruling L-R9: a covered child moved away, edited and moved back is not
// ready: its requirement is not the one the epic's approval pinned.
func TestCoveredChildMovedAwayAndBackNotReady(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	other := w.ticket(core.TicketInput{Title: "Other"})
	child := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	w.plan(epic, "1. Columns.", 0)
	w.approve(epic)
	expect(t, w.check(child))
	w.edit(child, core.TicketPatch{ParentID: new(other.ID)})
	w.edit(child, core.TicketPatch{Body: new("Columns, and more.")})
	w.edit(child, core.TicketPatch{ParentID: new(epic.ID)})
	expect(t, w.check(child), readiness.RequirementChanged)
	// Moved back unchanged, it is covered again.
	w.edit(child, core.TicketPatch{Body: new("")})
	expect(t, w.check(child))
}

// Rulings L-R6 and L-R8: a sub-ticket whose own approval is current needs
// its epic's approval to be current, but not to list it.
func TestEpicPlanNotApproved(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	w.plan(epic, "1. Columns.", 0)

	// The epic has no approval.
	own := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	w.approve(own)
	expect(t, w.check(own), readiness.EpicPlanNotApproved)

	// The epic's approval came after the sub-ticket's, so it lists it, but
	// it need not: the sub-ticket's own approval covers it.
	w.approve(epic)
	expect(t, w.check(own))
	later := w.ticket(core.TicketInput{Title: "Rails", ParentID: epic.ID, Tier: "light"})
	w.approve(later) // asked under the epic, which doesn't list it
	expect(t, w.check(later))

	// A request for changes on the epic leaves it with no current approval.
	w.answer("Request changes", epic)
	expect(t, w.check(own), readiness.EpicPlanNotApproved)
	expect(t, w.check(later), readiness.EpicPlanNotApproved)
}

// Ruling L-R11: an approval holds only under the parent it was asked
// under. A ticket moved to another epic, out of its epic, or under one
// from having none is not ready.
func TestReparentedTicketNotReady(t *testing.T) {
	w := newWorld(t)
	e1 := w.ticket(core.TicketInput{Title: "Board", Body: "A board."})
	e2 := w.ticket(core.TicketInput{Title: "List", Body: "A list."})
	c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e1.ID, Tier: "light"})
	w.plan(e1, "1. Columns.", 0)
	w.plan(e2, "1. Rows.", 0)
	w.approve(e1, c)
	w.approve(e2)
	expect(t, w.check(c))
	w.edit(c, core.TicketPatch{ParentID: new(e2.ID)})
	expect(t, w.check(c), readiness.EpicPlanNotApproved)
	w.edit(c, core.TicketPatch{ParentID: new("")})
	expect(t, w.check(c), readiness.EpicPlanNotApproved)
	w.edit(c, core.TicketPatch{ParentID: new(e1.ID)})
	expect(t, w.check(c)) // back where it was asked: the approval holds

	small := w.ticket(core.TicketInput{Title: "Typo", Tier: "light"})
	w.approve(small)
	expect(t, w.check(small))
	w.edit(small, core.TicketPatch{ParentID: new(e1.ID)})
	expect(t, w.check(small), readiness.EpicPlanNotApproved)
}

// Ruling L-R10: leaning is recursive, up to the first ancestor with a plan
// of its own.
func TestGrandchildLeansThroughItsParent(t *testing.T) {
	w := newWorld(t)
	e := w.ticket(core.TicketInput{Title: "Epic", Body: "The epic."})
	c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
	g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID, Tier: "light"})
	w.plan(e, "1. Child.", 0)
	w.approve(e, c)
	expect(t, w.check(g)) // all current

	w.plan(e, "1. Child.\n2. More.", 1)
	expect(t, w.check(g), readiness.EpicPlanChanged)
	w.approve(e, c)
	expect(t, w.check(g))

	w.edit(e, core.TicketPatch{Body: new("The epic, changed.")})
	expect(t, w.check(g), readiness.EpicRequirementChanged)
	w.approve(e, c)
	expect(t, w.check(g))

	// E's approval taken back: C leans on nothing.
	w.answer("Request changes", e)
	expect(t, w.check(g), readiness.EpicPlanNotApproved)

	// Batched with its ancestors, every ticket gets its own answer.
	all, err := readiness.CheckAll(bg, w.d.RQ, []string{g.ID, c.ID, e.ID})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, all[g.ID], readiness.EpicPlanNotApproved)
	expect(t, all[c.ID], readiness.NoTier, readiness.EpicPlanNotApproved)
	expect(t, all[e.ID], readiness.NoTier, readiness.PlanNotApproved)
}

// N-R1: a ticket moved to another project keeps its approvals in history,
// but they no longer make it ready; its plan moves with it, and an approval
// asked in the new project makes it ready again. Its sub-tickets, which
// moved with it, lean on nothing until then.
func TestMovedTicketNeedsApprovalAgain(t *testing.T) {
	w := newWorld(t)
	epic := w.ticket(core.TicketInput{Title: "Board", Body: "A board.", Tier: "standard"})
	w.plan(epic, "1. Columns.", 0)
	sub := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID, Tier: "light"})
	w.approve(epic)
	expect(t, w.check(epic))
	expect(t, w.check(sub))
	other, err := w.c.CreateProject(bg, w.owner.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := w.c.UpdateProject(bg, w.owner.ID, other.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.c.MoveTicketToProject(bg, w.owner.ID, epic.ID, other.ID, 0); err != nil {
		t.Fatal(err)
	}
	expect(t, w.check(epic), readiness.PlanNotApproved)
	expect(t, w.check(sub), readiness.PlanNotApproved)
	if n := count(t, w, `SELECT count(*) FROM approvals WHERE ticket_id = ?`, epic.ID); n != 1 {
		t.Fatalf("approvals %d, want the old one kept", n)
	}
	if n := count(t, w, `SELECT count(*) FROM plans WHERE ticket_id = ?`, epic.ID); n != 1 {
		t.Fatal("plan didn't move with it")
	}
	w.p = other
	w.approve(epic)
	expect(t, w.check(epic))
	expect(t, w.check(sub))
}

func count(t *testing.T, w *world, q string, args ...any) int {
	t.Helper()
	var n int
	if err := w.d.R.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
