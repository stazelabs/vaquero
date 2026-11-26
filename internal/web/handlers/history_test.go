package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
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
