package core_test

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func key(s string) core.TicketKey {
	k, ok := core.ParseTicketKey(s)
	if !ok {
		panic(s)
	}
	return k
}

func init() {
	ops = append(ops,
		coreOp{"AddLink",
			func(c *core.Core, f fixture) error {
				_, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "blocks", key("VAQ-2"))
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "blocks", key("VAQ-99"))
				return err
			}},
		coreOp{"RemoveLink",
			func(c *core.Core, f fixture) error { return c.RemoveLink(bg, f.Owner.ID, f.Link.ID) },
			func(c *core.Core, f fixture) error { return c.RemoveLink(bg, f.Owner.ID, "nope") }},
	)
}

func TestAddLinkByKey(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "blocks", key("vaq-1")); err != nil {
		t.Fatal(err)
	}
	from, _ := d.RQ.ListLinksFrom(bg, f.T2.ID)
	to, _ := d.RQ.ListLinksTo(bg, f.T1.ID)
	// The fixture already has VAQ-2 "relates" VAQ-1; "blocks" sorts first.
	if len(from) != 2 || from[0].Kind != "blocks" || from[0].OtherKey != "VAQ" || from[0].OtherNumber != 1 || len(to) != 2 {
		t.Fatalf("from %+v to %+v", from, to)
	}
}

func TestLinkRefusals(t *testing.T) {
	c, _, f := newFixture(t)
	_, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "blocks", key("VAQ-99"))
	fieldErr(t, err, "to", "No ticket VAQ-99.")
	_, err = c.AddLink(bg, f.Owner.ID, f.T1.ID, "blocks", key("VAQ-1"))
	fieldErr(t, err, "to", "A ticket can't link to itself.")
	_, err = c.AddLink(bg, f.Owner.ID, f.T2.ID, "relates", key("VAQ-1"))
	fieldErr(t, err, "to", "Those tickets are already linked that way.")
	_, err = c.AddLink(bg, f.Owner.ID, f.T1.ID, "fixes", key("VAQ-2"))
	fieldErr(t, err, "kind", "Choose blocks, relates to or duplicates.")
}

func TestLinksCrossProjects(t *testing.T) {
	c, _, f := newFixture(t)
	if _, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "relates", key("OTH-1")); err != nil {
		t.Fatal(err)
	}
}

// Review Focus 4.
func TestBlockingLoopRefused(t *testing.T) {
	c, d, f := newFixture(t)
	t3, _ := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third"})
	// VAQ-1 blocks VAQ-2, VAQ-2 blocks VAQ-3.
	if _, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "blocks", key("VAQ-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "blocks", key("VAQ-3")); err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	_, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "blocks", key("VAQ-1"))
	fieldErr(t, err, "to", "VAQ-1 already blocks this ticket.")
	_, err = c.AddLink(bg, f.Owner.ID, t3.ID, "blocks", key("VAQ-1"))
	fieldErr(t, err, "to", "That would make a loop: VAQ-1 blocks this ticket through VAQ-2.")
	if eventCount(t, d) != before {
		t.Fatal("refusals wrote events")
	}
}

func TestDuplicateBothWaysRefused(t *testing.T) {
	c, _, f := newFixture(t)
	if _, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "duplicates", key("VAQ-2")); err != nil {
		t.Fatal(err)
	}
	_, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "duplicates", key("VAQ-1"))
	fieldErr(t, err, "to", "VAQ-1 already duplicates this ticket.")
}

func TestRelatesLoopAllowed(t *testing.T) {
	c, _, f := newFixture(t)
	t3, _ := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third"})
	if _, err := c.AddLink(bg, f.Owner.ID, f.T1.ID, "relates", key("VAQ-3")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddLink(bg, f.Owner.ID, t3.ID, "relates", key("VAQ-2")); err != nil {
		t.Fatal(err)
	}
}

// A link is one event on the "from" ticket; the "to" ticket's history lists
// it too, and no second event is written.
func TestLinkEventsAppearInBothTicketsHistory(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	l, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "blocks", core.TicketKey{Project: "VAQ", Number: f.T1.Number})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLink(bg, f.Owner.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	if got := eventCount(t, d) - before; got != 2 {
		t.Fatalf("%d events for add and remove, want 2", got)
	}
	hist := func(tk gen.Ticket) []string {
		evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: tk.ID, Valid: true})
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, e := range evs {
			if e.Kind == core.EvLinkCreated || e.Kind == core.EvLinkDeleted {
				kinds = append(kinds, e.Kind)
			}
		}
		return kinds
	}
	// The fixture already holds one link from T2 to T1.
	want := []string{core.EvLinkDeleted, core.EvLinkCreated, core.EvLinkCreated}
	if got := hist(f.T2); !slices.Equal(got, want) {
		t.Fatalf("from side: %v", got)
	}
	if got := hist(f.T1); !slices.Equal(got, want) {
		t.Fatalf("to side: %v", got)
	}
}

// A key freed by a rename and taken by a new project must not pull the old
// ticket's link events into the new ticket's history.
func TestLinkHistoryFollowsTheTicketNotItsKey(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "blocks", core.TicketKey{Project: "VAQ", Number: f.T1.Number}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "OLD"); err != nil {
		t.Fatal(err)
	}
	np, err := c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: "VAQ", Name: "Reuse"})
	if err != nil {
		t.Fatal(err)
	}
	reuse, err := c.CreateTicket(bg, f.Owner.ID, np.ID, core.TicketInput{Title: "Unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	// reuse is VAQ-1, as T1 once was.
	other, err := c.CreateTicket(bg, f.Owner.ID, np.ID, core.TicketInput{Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddLink(bg, f.Owner.ID, other.ID, "blocks", core.TicketKey{Project: "OLD", Number: f.T1.Number}); err != nil {
		t.Fatal(err)
	}
	links := func(tk gen.Ticket) int {
		evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: tk.ID, Valid: true})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range evs {
			if e.Kind == core.EvLinkCreated {
				n++
			}
		}
		return n
	}
	if n := links(reuse); n != 0 {
		t.Fatalf("a ticket that reuses the key shows %d foreign link events", n)
	}
	// T1 (now OLD-1) keeps the fixture link, T2's, and the new one from other.
	if n := links(f.T1); n != 3 {
		t.Fatalf("renamed ticket has %d link events, want 3", n)
	}
}

// VAQ-61: the loop check has no cap: a loop is refused however far the
// chain reaches, even past the walk that names the chain in the message.
func TestBlockingLoopRefusedPastTheWalk(t *testing.T) {
	c, _, f := newFixture(t)
	core.SetMaxLoopWalk(t, 2)
	// VAQ-1 blocks VAQ-2 blocks VAQ-3 blocks VAQ-4 blocks VAQ-5.
	prev := f.T1
	for i := range 4 {
		next := f.T2
		if i > 0 {
			var err error
			if next, err = c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Next"}); err != nil {
				t.Fatal(err)
			}
		}
		k := core.TicketKey{Project: "VAQ", Number: next.Number}.String()
		if _, err := c.AddLink(bg, f.Owner.ID, prev.ID, "blocks", key(k)); err != nil {
			t.Fatal(err)
		}
		prev = next
	}
	_, err := c.AddLink(bg, f.Owner.ID, prev.ID, "blocks", key("VAQ-1"))
	fieldErr(t, err, "to", "That would make a loop: VAQ-1 blocks this ticket through a chain of blocking tickets.")
}
