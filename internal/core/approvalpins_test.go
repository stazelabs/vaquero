package core_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// askedChildren is what plan approval dec pinned of ticketID's
// descendants, as asked.
func askedChildren(t *testing.T, d dbtest.DB, dec, ticketID string) []core.PinnedChild {
	t.Helper()
	var raw string
	if err := d.R.QueryRow(`SELECT children FROM decision_tickets WHERE decision_id = ? AND ticket_id = ?`, dec, ticketID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var out []core.PinnedChild
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// An epic's plan approval pins every descendant without a plan of its
// own, at any depth, each with its parent and requirement; a descendant
// with its own plan, and everything under it, is left out (it needs its
// own approval). An Approve after a deep descendant changed is refused.
func TestChildPinsDescend(t *testing.T) {
	c, d, f := newFixture(t)
	mk := func(title, parent string) gen.Ticket {
		t.Helper()
		tk, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: title, ParentID: parent})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Epic plan.", 0); err != nil {
		t.Fatal(err)
	}
	child := mk("Child", f.T1.ID)
	grand := mk("Grandchild", child.ID)
	planned := mk("Planned", f.T1.ID)
	under := mk("Under planned", planned.ID)
	if _, err := c.SavePlan(bg, f.Owner.ID, planned.ID, "Own plan.", 0); err != nil {
		t.Fatal(err)
	}
	q, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	want := []core.PinnedChild{
		{ID: child.ID, Parent: f.T1.ID, RequirementHash: core.RequirementHash("Child", "")},
		{ID: grand.ID, Parent: child.ID, RequirementHash: core.RequirementHash("Grandchild", "")},
	}
	slices.SortFunc(want, func(a, b core.PinnedChild) int {
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	if got := askedChildren(t, d, q.ID, f.T1.ID); !slices.Equal(got, want) {
		t.Fatalf("asked children %+v, want %+v (not %s or %s)", got, want, planned.ID, under.ID)
	}
	// A grandchild edited since: Approve refused.
	body := "Rewritten."
	if _, err := c.PatchTicket(bg, f.Owner.ID, grand.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		t.Fatal(err)
	}
	_, err = c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: core.ApproveLabel})
	fieldErr(t, err, "answer", "The plan or requirement for VAQ-1 changed since this was asked; ask again.")
	// Asked again, it approves, and the approval row pins the same list.
	q, _ = c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: core.ApproveLabel}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := d.R.QueryRow(`SELECT pins FROM approvals WHERE decision_id = ? AND ticket_id = ?`, q.ID, f.T1.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var pins core.ApprovalPins
	if err := json.Unmarshal([]byte(raw), &pins); err != nil {
		t.Fatal(err)
	}
	if len(pins.Children) != 2 || !slices.ContainsFunc(pins.Children, func(p core.PinnedChild) bool {
		return p.ID == grand.ID && p.Parent == child.ID && p.RequirementHash == core.RequirementHash("Grandchild", body)
	}) {
		t.Fatalf("approval pins %+v", pins.Children)
	}
	// A descendant gaining a plan of its own since leaves the list: refused.
	q, _ = c.AskDecision(bg, f.Owner.ID, planApproval(f, f.T1.ID))
	if _, err := c.SavePlan(bg, f.Owner.ID, child.ID, "Child plan.", 0); err != nil {
		t.Fatal(err)
	}
	_, err = c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: core.ApproveLabel})
	fieldErr(t, err, "answer", "The plan or requirement for VAQ-1 changed since this was asked; ask again.")
}

// A move to another project deletes the approvals of every ticket it
// re-keys, and drops them from the descendants other approvals cover, in
// the move's one transaction.
func TestMoveClearsApprovals(t *testing.T) {
	c, d, f := newFixture(t)
	approve := func(ids ...string) {
		t.Helper()
		q, err := c.AskDecision(bg, f.Owner.ID, planApproval(f, ids...))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: core.ApproveLabel}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(id string) int {
		t.Helper()
		var n int
		if err := d.R.QueryRow(`SELECT count(*) FROM approvals WHERE ticket_id = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	sub, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Sub", ParentID: f.T1.ID})
	if err != nil {
		t.Fatal(err)
	}
	approve(f.T1.ID, sub.ID)
	approve(f.T2.ID)
	if count(f.T1.ID) != 1 || count(sub.ID) != 1 {
		t.Fatal("approvals not written")
	}
	events := func() int {
		var n int
		if err := d.R.QueryRow(`SELECT count(*) FROM events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := events()
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if count(f.T1.ID) != 0 || count(sub.ID) != 0 {
		t.Fatalf("approvals left: %d %d", count(f.T1.ID), count(sub.ID))
	}
	if count(f.T2.ID) != 1 {
		t.Fatal("another ticket's approval went too")
	}
	if events() != before+1 {
		t.Fatalf("events %d, want one more than %d", events(), before)
	}

	// A detached sub-ticket moved alone leaves its epic's approval, which
	// no longer covers it.
	child, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Child", ParentID: f.T2.ID})
	if err != nil {
		t.Fatal(err)
	}
	approve(f.T2.ID)
	none := ""
	if _, err := c.PatchTicket(bg, f.Owner.ID, child.ID, core.TicketPatch{ParentID: &none}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, child.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	rows, err := d.R.Query(`SELECT pins FROM approvals WHERE ticket_id = ?`, f.T2.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var pins core.ApprovalPins
		if err := json.Unmarshal([]byte(raw), &pins); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(pins.Children, func(p core.PinnedChild) bool { return p.ID == child.ID }) {
			t.Fatalf("still covers the moved ticket: %s", raw)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("%d approvals of VAQ-2, want both kept", n)
	}
}
