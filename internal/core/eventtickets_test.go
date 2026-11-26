package core_test

import (
	"database/sql"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// kindsIn counts each event kind in a ticket's history.
func kindsIn(t *testing.T, d dbtest.DB, ticketID string) map[string]int {
	t.Helper()
	evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: ticketID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	n := map[string]int{}
	for _, e := range evs {
		if seen[e.Seq] {
			t.Errorf("event %d (%s) twice in %s's history", e.Seq, e.Kind, ticketID)
		}
		seen[e.Seq] = true
		n[e.Kind]++
	}
	return n
}

// VAQ-144: a decision naming several tickets is one project event, and it
// shows in each member's history, once. A one-ticket decision, on its
// ticket, shows once too.
func TestGroupDecisionInMemberHistory(t *testing.T) {
	c, d, f := newFixture(t)
	group, err := c.AskDecision(bg, f.Owner.ID, core.DecisionInput{
		ProjectID: f.Project.ID, TicketIDs: []string{f.T1.ID, f.T2.ID}, Kind: core.DecisionQuestion, Question: "Which first?",
		Options: []core.Option{{Label: "VAQ-1"}, {Label: "VAQ-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, group.ID, core.Answer{Option: "VAQ-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AskDecision(bg, f.Owner.ID, question(f)); err != nil {
		t.Fatal(err)
	}
	one := kindsIn(t, d, f.T1.ID)
	if one[core.EvDecisionAsked] != 2 || one[core.EvDecisionAnswered] != 1 {
		t.Errorf("VAQ-1's history: %v", one)
	}
	two := kindsIn(t, d, f.T2.ID)
	if two[core.EvDecisionAsked] != 1 || two[core.EvDecisionAnswered] != 1 {
		t.Errorf("VAQ-2's history: %v", two)
	}
	if o := kindsIn(t, d, f.O1.ID); o[core.EvDecisionAsked] != 0 {
		t.Errorf("OTH-1's history: %v", o)
	}
}

// Run N's tag history (VAQ-134), through event_tickets now: a rename, a
// merge and a delete each show once on the tickets they changed, and on
// no other.
func TestTagEventsStillInHistory(t *testing.T) {
	c, d, f := newFixture(t)
	if err := c.RenameTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, "defect", false); err != nil {
		t.Fatal(err)
	}
	if err := c.MergeTag(bg, f.Owner.ID, f.Project.ID, f.Bug.ID, f.UI.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteTag(bg, f.Owner.ID, f.Project.ID, f.UI.ID); err != nil {
		t.Fatal(err)
	}
	one, two := kindsIn(t, d, f.T1.ID), kindsIn(t, d, f.T2.ID)
	if one[core.EvTagRenamed] != 1 || one[core.EvTagMerged] != 1 || one[core.EvTagDeleted] != 1 {
		t.Errorf("VAQ-1's history: %v", one)
	}
	if two[core.EvTagRenamed] != 0 || two[core.EvTagMerged] != 0 || two[core.EvTagDeleted] != 1 {
		t.Errorf("VAQ-2's history: %v", two)
	}
	if o := kindsIn(t, d, f.O1.ID); o[core.EvTagRenamed]+o[core.EvTagMerged]+o[core.EvTagDeleted] != 0 {
		t.Errorf("OTH-1's history: %v", o)
	}
	// A project delete removes the lookup rows with the events (PR-2).
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "VAQ"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM event_tickets`); n != 0 {
		t.Errorf("%d event_tickets rows left", n)
	}
}
