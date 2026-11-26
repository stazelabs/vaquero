package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-21: the ticket history shows a description edit's line diff behind
// "Show changes"; the activity page shows only the sentence.
func TestHistoryShowsDescriptionDiff(t *testing.T) {
	e := webtest.New(t)
	tk := ticket(t, e, project(t, e), core.TicketInput{Title: "x", Body: "keep\nold line"})
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: new("keep\nnew line")}, 0); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "changed the description of VAQ-1", "<details", "Show changes", "old line", "new line")
	act := c.Get("/activity").Body
	contains(t, act, "changed the description of VAQ-1")
	lacks(t, act, "Show changes", "old line")
}

// VAQ-21: an event from before the change (no stored text) still renders,
// with no toggle; one past the limit says so.
func TestHistoryOldAndLongDescriptionEvents(t *testing.T) {
	e := webtest.New(t)
	tk := ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: new("a")}, 0); err != nil {
		t.Fatal(err)
	}
	// Make it look like a 0.1.x event: changes only.
	if _, err := e.Deps.Core.DB.Exec(`UPDATE events SET payload = '{"changes":{"body":{"from":null,"to":null}}}' WHERE kind = 'ticket.updated'`); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "changed the description of VAQ-1")
	lacks(t, page, "Show changes")

	long := strings.Repeat("z", core.BodyHistoryMax+1)
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: &long}, 0); err != nil {
		t.Fatal(err)
	}
	page = c.Get("/t/VAQ-1").Body
	contains(t, page, "Show changes", "The description is too long to show.")
}

// reorderWorld is two tickets in one column, so each can be dragged above
// the other.
func reorderWorld(t *testing.T) (*webtest.Env, gen.Ticket, gen.Ticket, string) {
	t.Helper()
	e := webtest.New(t)
	p := project(t, e)
	a := ticket(t, e, p, core.TicketInput{Title: "A"})
	b := ticket(t, e, p, core.TicketInput{Title: "B"})
	return e, a, b, a.StatusID
}

func reorder(t *testing.T, e *webtest.Env, by string, tk, other gen.Ticket, above bool) {
	t.Helper()
	before, after := "", other.ID
	if !above {
		before, after = other.ID, ""
	}
	if _, err := e.Deps.Core.MoveTicket(bg, by, tk.ID, tk.StatusID, before, after); err != nil {
		t.Fatal(err)
	}
}

// VAQ-32: reorders stay out of the activity page and the activity API,
// though they are still recorded and still advance the sequence.
func TestActivityLeavesOutReorders(t *testing.T) {
	e, a, b, _ := reorderWorld(t)
	tok := e.Token(t)
	seq0, _ := e.Deps.R.MaxEventSeq(bg)
	reorder(t, e, e.Deps.Actor.ID, b, a, true)
	if seq, _ := e.Deps.R.MaxEventSeq(bg); seq != seq0+1 {
		t.Fatalf("seq %d → %d", seq0, seq)
	}
	c := e.Client()
	contains(t, c.Get("/activity").Body, "created VAQ-2 B")
	lacks(t, c.Get("/activity").Body, "reordered")
	for _, path := range []string{"/api/v1/events?after=0", "/api/v1/events?newest=50"} {
		var evs []api.Event
		decode(t, c.API("GET", path, tok, nil).Body, &evs)
		if len(evs) == 0 {
			t.Fatalf("%s: no events", path)
		}
		for _, ev := range evs {
			if ev.Kind == core.EvTicketReordered {
				t.Fatalf("%s lists a reorder", path)
			}
		}
	}
	contains(t, c.Get("/t/VAQ-2").Body, "reordered VAQ-2 in Todo")
}

// VAQ-32: a ticket's history folds a run of reorders by one actor into one
// line, and never folds across another event or another actor.
func TestHistoryFoldsReorders(t *testing.T) {
	e, a, b, _ := reorderWorld(t)
	owner := e.Deps.Actor.ID
	for i := range 4 {
		reorder(t, e, owner, b, a, i%2 == 0)
	}
	c := e.Client()
	page := c.Get("/t/VAQ-2").Body
	contains(t, page, "reordered VAQ-2 in Todo 4 times")
	if n := strings.Count(page, "reordered VAQ-2"); n != 1 {
		t.Fatalf("%d reorder lines", n)
	}

	// A status change between reorders splits the run and stays visible.
	ss := statuses(t, e, mustProject(t, e))
	if _, err := e.Deps.Core.PatchTicket(bg, owner, b.ID, core.TicketPatch{StatusID: new(ss[2].ID)}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, owner, a.ID, core.TicketPatch{StatusID: new(ss[2].ID)}, 0); err != nil {
		t.Fatal(err)
	}
	b, _ = e.Deps.R.GetTicket(bg, b.ID)
	reorder(t, e, owner, b, a, false) // b was above a
	reorder(t, e, owner, b, a, true)
	agent, err := e.Deps.Core.EnsureAgent(bg, owner, "bot")
	if err != nil {
		t.Fatal(err)
	}
	reorder(t, e, agent.ID, b, a, false)
	page = c.Get("/t/VAQ-2").Body
	contains(t, page, "reordered VAQ-2 in In progress 2 times", "moved VAQ-2 from Todo to In progress", "reordered VAQ-2 in Todo 4 times")
	if n := strings.Count(page, "reordered VAQ-2"); n != 3 {
		t.Fatalf("%d reorder lines, want 3 (bot, owner ×2, owner ×4)", n)
	}
	lacks(t, page, "in In progress 3 times")
}
