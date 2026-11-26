package board_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/readiness"
)

func (e *env) focus(t *testing.T, f board.Filter) board.Focus {
	t.Helper()
	fo, err := board.LoadFocus(ctx, e.d.RQ, e.p, e.owner, f, e.now)
	if err != nil {
		t.Fatal(err)
	}
	return fo
}

func keys(cs []board.Card) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Key)
	}
	return out
}

// Next: Urgent, High, Medium, Low, then no priority; within a priority,
// board position.
func TestFocusNextOrdersByPriorityThenPosition(t *testing.T) {
	e := newEnv(t)
	e.ticket(t, core.TicketInput{Title: "none a"})              // VAQ-1
	e.ticket(t, core.TicketInput{Title: "low", Priority: 4})    // VAQ-2
	e.ticket(t, core.TicketInput{Title: "urgent", Priority: 1}) // VAQ-3
	e.ticket(t, core.TicketInput{Title: "high a", Priority: 2}) // VAQ-4
	e.ticket(t, core.TicketInput{Title: "high b", Priority: 2}) // VAQ-5
	e.ticket(t, core.TicketInput{Title: "none b"})              // VAQ-6
	// VAQ-5 dragged above VAQ-4: position decides within High.
	if _, err := e.c.MoveTicket(ctx, e.owner.ID, ticketID(t, e, 5), e.st["Todo"].ID, "", ticketID(t, e, 4)); err != nil {
		t.Fatal(err)
	}
	fo := e.focus(t, any_())
	want := []string{"VAQ-3", "VAQ-5", "VAQ-4", "VAQ-2", "VAQ-1"}
	if got := keys(fo.Next); !slices.Equal(got, want) {
		t.Fatalf("next %v, want %v", got, want)
	}
	if got := keys(fo.NextRest); !slices.Equal(got, []string{"VAQ-6"}) || fo.NextMore != 1 {
		t.Fatalf("rest %v more %d", got, fo.NextMore)
	}
}

func TestFocusNextShowsFiveAndCountsMore(t *testing.T) {
	e := newEnv(t)
	for i := range 9 {
		e.ticket(t, core.TicketInput{Title: fmt.Sprint("t", i)})
	}
	e.ticket(t, core.TicketInput{Title: "taken", AssigneeID: e.owner.ID})
	fo := e.focus(t, any_())
	if len(fo.Next) != 5 || fo.NextMore != 4 || len(fo.NextRest) != 4 {
		t.Fatalf("next %d more %d rest %d", len(fo.Next), fo.NextMore, len(fo.NextRest))
	}
	// A filter narrows the lanes as it narrows the board.
	e.ticket(t, core.TicketInput{Title: "tagged", Tags: []string{"ui"}})
	f := any_()
	f.Tag = "ui"
	if got := keys(e.focus(t, f).Next); !slices.Equal(got, []string{"VAQ-11"}) {
		t.Fatalf("filtered next %v", got)
	}
}

func reasonOf(fo board.Focus, key string) string {
	for _, w := range fo.Waiting {
		if w.Key == key {
			return w.Reason
		}
	}
	return "<not waiting>"
}

// Waiting: todo tickets that aren't ready, each with why. With the
// workflow on, the workflow's reasons too.
func TestFocusWaitingReasons(t *testing.T) {
	e := newEnv(t)
	blocker := e.ticket(t, core.TicketInput{Title: "blocker"})               // VAQ-1, ready
	blocked := e.ticket(t, core.TicketInput{Title: "blocked"})               // VAQ-2
	parent := e.ticket(t, core.TicketInput{Title: "parent"})                 // VAQ-3
	e.ticket(t, core.TicketInput{Title: "child", ParentID: parent.ID})       // VAQ-4, ready
	claimed := e.ticket(t, core.TicketInput{Title: "claimed"})               // VAQ-5
	e.ticket(t, core.TicketInput{Title: "assigned", AssigneeID: e.owner.ID}) // VAQ-6
	_ = blocked
	if _, err := e.c.AddLink(ctx, e.owner.ID, blocker.ID, "blocks", core.TicketKey{Project: "VAQ", Number: 2}); err != nil {
		t.Fatal(err)
	}
	agent, err := e.c.EnsureAgent(ctx, e.owner.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.Claim(ctx, agent.ID, claimed.ID, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	// A claim moves the ticket to in progress; back in todo it still waits.
	if _, err := e.c.MoveTicket(ctx, e.owner.ID, claimed.ID, e.st["Todo"].ID, "", ""); err != nil {
		t.Fatal(err)
	}
	fo := e.focus(t, any_())
	for key, want := range map[string]string{
		"VAQ-2": "Blocked by VAQ-1",
		"VAQ-3": "Has open sub-tickets",
		"VAQ-5": "Claimed by builder",
		"VAQ-6": "Assigned to Owner",
	} {
		if got := reasonOf(fo, key); got != want {
			t.Errorf("%s: reason %q, want %q", key, got, want)
		}
	}
	if got := keys(fo.Next); !slices.Equal(got, []string{"VAQ-1", "VAQ-4"}) {
		t.Errorf("next %v", got)
	}

	// The workflow on: a ticket with no tier or approval waits with the
	// workflow's reasons; a blocked one gives both.
	on := true
	if _, err := e.c.UpdateProject(ctx, e.owner.ID, e.p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
	if e.p, err = e.d.RQ.GetProject(ctx, e.p.ID); err != nil {
		t.Fatal(err)
	}
	fo = e.focus(t, any_())
	if len(fo.Next) != 0 {
		t.Errorf("next with the workflow on and nothing approved: %v", keys(fo.Next))
	}
	if got := reasonOf(fo, "VAQ-1"); got != readiness.NoTier+" "+readiness.PlanNotApproved {
		t.Errorf("not approved: %q", got)
	}
	if got := reasonOf(fo, "VAQ-2"); !strings.HasPrefix(got, "Blocked by VAQ-1 · ") || !strings.Contains(got, readiness.PlanNotApproved) {
		t.Errorf("blocked and not approved: %q", got)
	}
}

// Just finished: done in the last seven days, newest first, with its day.
func TestFocusFinishedSevenDays(t *testing.T) {
	e := newEnv(t)
	done := e.st["Done"].ID
	start := e.now
	e.ticket(t, core.TicketInput{Title: "eight days", StatusID: done}) // VAQ-1
	e.now = start.Add(2 * 24 * time.Hour)
	e.ticket(t, core.TicketInput{Title: "six days", StatusID: done}) // VAQ-2
	e.now = start.Add(7 * 24 * time.Hour)
	e.ticket(t, core.TicketInput{Title: "yesterday", StatusID: done}) // VAQ-3
	e.now = start.Add(8*24*time.Hour - time.Hour)
	e.ticket(t, core.TicketInput{Title: "today", StatusID: done}) // VAQ-4
	e.ticket(t, core.TicketInput{Title: "cancelled", StatusID: e.st["Cancelled"].ID})
	e.now = start.Add(8 * 24 * time.Hour)
	fo := e.focus(t, any_())
	if got := keys(fo.Finished); !slices.Equal(got, []string{"VAQ-4", "VAQ-3", "VAQ-2"}) {
		t.Fatalf("finished %v", got)
	}
	// t0 is a Wednesday; eight days on is a Thursday, and six days before it a Friday.
	var days []string
	for _, c := range fo.Finished {
		days = append(days, c.When)
	}
	if !slices.Equal(days, []string{"today", "yesterday", "Fri"}) {
		t.Fatalf("days %v", days)
	}
	for i := range 9 {
		e.ticket(t, core.TicketInput{Title: fmt.Sprint("more ", i), StatusID: done})
	}
	fo = e.focus(t, any_())
	if len(fo.Finished) != 8 || fo.FinishedMore != 4 || len(fo.FinishedRest) != 4 {
		t.Fatalf("finished %d more %d", len(fo.Finished), fo.FinishedMore)
	}
}

// In flight: in progress, then in review, eight a group.
func TestFocusInFlightGroups(t *testing.T) {
	e := newEnv(t)
	for i := range 10 {
		e.ticket(t, core.TicketInput{Title: fmt.Sprint("wip ", i), StatusID: e.st["In progress"].ID})
	}
	e.ticket(t, core.TicketInput{Title: "review", StatusID: e.st["In review"].ID})
	fo := e.focus(t, any_())
	if len(fo.InFlight) != 2 || fo.InFlight[0].Status.Name != "In progress" || fo.InFlight[1].Status.Name != "In review" {
		t.Fatalf("%+v", fo.InFlight)
	}
	g := fo.InFlight[0]
	if g.Run != "" || len(g.Cards) != 8 || g.More != 2 || len(g.Rest) != 2 || len(fo.InFlight[1].Cards) != 1 {
		t.Fatalf("groups %d/%d/%d", len(g.Cards), g.More, len(fo.InFlight[1].Cards))
	}
}

func TestFocusNeedsDecision(t *testing.T) {
	e := newEnv(t)
	if e.focus(t, any_()).NeedsDecision {
		t.Fatal("needs decision without the status")
	}
	if _, err := e.c.CreateStatus(ctx, e.owner.ID, e.p.ID, "Needs decision", "backlog"); err != nil {
		t.Fatal(err)
	}
	if !e.focus(t, any_()).NeedsDecision {
		t.Fatal("no needs decision with the status")
	}
}

func ticketID(t *testing.T, e *env, n int64) string {
	t.Helper()
	tk, _, err := e.c.ResolveTicket(ctx, e.d.RQ, core.TicketKey{Project: "VAQ", Number: n})
	if err != nil {
		t.Fatal(err)
	}
	return tk.ID
}
