package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// statusOrder is a project's status names, by position, and checks the
// positions run 1, 2, 3...
func statusOrder(t *testing.T, d dbtest.DB, projectID string) []string {
	t.Helper()
	ss, err := d.RQ.ListStatuses(bg, projectID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i, s := range ss {
		if s.Position != int64(i+1) {
			t.Errorf("%s at position %d, want %d", s.Name, s.Position, i+1)
		}
		out = append(out, s.Name)
	}
	return out
}

func sameOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func newProject(t *testing.T) (*core.Core, dbtest.DB, gen.Actor, gen.Project) {
	t.Helper()
	c, d, owner := newCore(t)
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	return c, d, owner, p
}

// VAQ-126: a new status goes right after the last status of its category.
func TestCreateStatusLandsAtTheEndOfItsCategory(t *testing.T) {
	c, d, owner, p := newProject(t)
	before := eventCount(t, d)
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Needs decision", "backlog"); err != nil {
		t.Fatal(err)
	}
	if n := eventCount(t, d) - before; n != 1 {
		t.Fatalf("%d events", n)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Backlog", "Needs decision", "Todo", "In progress", "In review", "Done", "Cancelled")

	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Second review", "in_review"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Needs design", "backlog"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Won't do", "cancelled"); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Backlog", "Needs decision", "Needs design", "Todo", "In progress",
		"In review", "Second review", "Done", "Cancelled", "Won't do")
}

// A status of a category that has none (an imported database can have
// one) goes after the last status of the nearest earlier category, or
// first when no earlier category has any.
func TestCreateStatusInAnEmptyCategory(t *testing.T) {
	c, d, owner, p := newProject(t)
	if _, err := d.W.Exec(`DELETE FROM statuses WHERE project_id = ? AND category IN ('backlog', 'in_review', 'in_progress')`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Review", "in_review"); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Todo", "Review", "Done", "Cancelled")
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Icebox", "backlog"); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Icebox", "Todo", "Review", "Done", "Cancelled")
}

// The order follows the last status of the category even after a status
// was moved out of its category's run.
// A neighbour, when given, places the new status instead.
// Review Focus 3: a claim moves a ticket to the first in-progress status
// by position, whatever order the statuses were added in.
func TestClaimUsesTheFirstInProgressStatus(t *testing.T) {
	c, d, owner, p := newProject(t)
	ss, _ := d.RQ.ListStatuses(bg, p.ID)
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Building", "in_progress"); err != nil {
		t.Fatal(err)
	}
	tk, err := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := c.EnsureAgent(bg, owner.ID, "builder")
	got, err := c.Claim(bg, agent.ID, tk.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != ss[2].ID {
		t.Fatalf("claimed into %s, want In progress", got.StatusID)
	}
	// Building moved first: the next claim lands there.
	building, _ := d.RQ.ListStatuses(bg, p.ID)
	if err := c.MoveStatus(bg, owner.ID, building[3].ID, -1); err != nil {
		t.Fatal(err)
	}
	t2, _ := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "More"})
	got, err = c.Claim(bg, agent.ID, t2.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusID != building[3].ID {
		t.Fatalf("claimed into %s, want Building", got.StatusID)
	}
}

func TestCreateStatusFollowsTheLastOfItsCategory(t *testing.T) {
	c, d, owner, p := newProject(t)
	ss, _ := d.RQ.ListStatuses(bg, p.ID)
	// Move Todo to the end: Backlog, In progress, In review, Done, Cancelled, Todo.
	if err := c.MoveStatusTo(bg, owner.ID, ss[1].ID, core.StatusPlace{After: ss[5].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatus(bg, owner.ID, p.ID, "Ready", "todo"); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Backlog", "In progress", "In review", "Done", "Cancelled", "Todo", "Ready")
}

func TestCreateStatusAtANeighbour(t *testing.T) {
	c, d, owner, p := newProject(t)
	ss, _ := d.RQ.ListStatuses(bg, p.ID)
	if _, err := c.CreateStatusAt(bg, owner.ID, p.ID, "Triage", "backlog", core.StatusPlace{Before: ss[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatusAt(bg, owner.ID, p.ID, "Blocked", "in_progress", core.StatusPlace{After: ss[3].ID}); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Triage", "Backlog", "Todo", "In progress", "In review", "Blocked", "Done", "Cancelled")

	_, err := c.CreateStatusAt(bg, owner.ID, p.ID, "X", "todo", core.StatusPlace{Before: ss[0].ID, After: ss[1].ID})
	fieldErr(t, err, "after", "Give before or after, not both.")
	other, _ := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	os, _ := d.RQ.ListStatuses(bg, other.ID)
	_, err = c.CreateStatusAt(bg, owner.ID, p.ID, "X", "todo", core.StatusPlace{After: os[0].ID})
	fieldErr(t, err, "after", "Choose a status from this project.")
}

func TestMoveStatusTo(t *testing.T) {
	c, d, owner, p := newProject(t)
	ss, _ := d.RQ.ListStatuses(bg, p.ID)
	backlog, todo, done, cancelled := ss[0], ss[1], ss[4], ss[5]

	before, _ := d.RQ.MaxEventSeq(bg)
	if err := c.MoveStatusTo(bg, owner.ID, cancelled.ID, core.StatusPlace{Before: todo.ID}); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Backlog", "Cancelled", "Todo", "In progress", "In review", "Done")
	if after, _ := d.RQ.MaxEventSeq(bg); after != before+1 {
		t.Fatalf("%d events", after-before)
	}
	evs, _ := d.RQ.ListEventsNewest(bg, 1)
	if evs[0].Kind != core.EvStatusMoved || evs[0].Payload != `{"from":6,"name":"Cancelled","to":2}` {
		t.Fatalf("%s %s", evs[0].Kind, evs[0].Payload)
	}

	if err := c.MoveStatusTo(bg, owner.ID, backlog.ID, core.StatusPlace{After: done.ID}); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, statusOrder(t, d, p.ID), "Cancelled", "Todo", "In progress", "In review", "Done", "Backlog")

	// Already there: nothing changes and no event.
	seq, _ := d.RQ.MaxEventSeq(bg)
	if err := c.MoveStatusTo(bg, owner.ID, backlog.ID, core.StatusPlace{After: done.ID}); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveStatusTo(bg, owner.ID, done.ID, core.StatusPlace{Before: backlog.ID}); err != nil {
		t.Fatal(err)
	}
	if now, _ := d.RQ.MaxEventSeq(bg); now != seq {
		t.Fatal("a move to where it already is wrote an event")
	}

	fieldErr(t, c.MoveStatusTo(bg, owner.ID, todo.ID, core.StatusPlace{}), "before", "Choose a status to move it before or after.")
	fieldErr(t, c.MoveStatusTo(bg, owner.ID, todo.ID, core.StatusPlace{Before: done.ID, After: done.ID}), "after", "Give before or after, not both.")
	fieldErr(t, c.MoveStatusTo(bg, owner.ID, todo.ID, core.StatusPlace{Before: todo.ID}), "before", "Choose another status.")
	fieldErr(t, c.MoveStatusTo(bg, owner.ID, todo.ID, core.StatusPlace{After: "nope"}), "after", "Choose a status from this project.")
	other, _ := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	os, _ := d.RQ.ListStatuses(bg, other.ID)
	fieldErr(t, c.MoveStatusTo(bg, owner.ID, todo.ID, core.StatusPlace{Before: os[0].ID}), "before", "Choose a status from this project.")
}
