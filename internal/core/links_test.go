package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
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
