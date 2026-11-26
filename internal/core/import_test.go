package core_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops,
		coreOp{"ImportTicket",
			func(c *core.Core, f fixture) error {
				_, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, 40, core.TicketInput{Title: "Imported"})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, 2, core.TicketInput{Title: "Taken"})
				return err
			}})
}

// ImportTicket keeps a number from another tracker; numbering goes on
// after it, and a number at or below one already given is refused.
func TestImportTicketKeepsItsNumber(t *testing.T) {
	c, d, f := newFixture(t) // VAQ-1 and VAQ-2 exist
	tk, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, 123, core.TicketInput{Title: "From elsewhere", Priority: 2, Tags: []string{"bug"}})
	if err != nil || tk.Number != 123 || tk.Priority != 2 {
		t.Fatalf("%v %+v", err, tk)
	}
	ev, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: tk.ID, Valid: true})
	if err != nil || len(ev) != 1 || ev[0].Kind != core.EvTicketCreated {
		t.Fatalf("%v %+v", err, ev)
	}
	next, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "After"})
	if err != nil || next.Number != 124 {
		t.Fatalf("%v %d", err, next.Number)
	}
	for _, n := range []int64{0, -1, 1, 2, 123, 124} {
		_, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, n, core.TicketInput{Title: "Again"})
		inv, ok := errors.AsType[*core.Invalid](err)
		if !ok || inv.Fields["number"] == "" {
			t.Fatalf("%d: %v", n, err)
		}
	}
	// The gap is kept: 125 is free, and a later number may skip ahead.
	if tk, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, 125, core.TicketInput{Title: "Next"}); err != nil || tk.Number != 125 {
		t.Fatalf("%v %+v", err, tk)
	}
	// The ticket's other fields are checked as CreateTicket checks them.
	if _, err := c.ImportTicket(bg, f.Owner.ID, f.Project.ID, 200, core.TicketInput{Title: " "}); err == nil {
		t.Fatal("blank title accepted")
	}
}
