package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

func init() {
	ops = append(ops, coreOp{"DeleteProject",
		func(c *core.Core, f fixture) error { return c.DeleteProject(bg, f.Owner.ID, f.Other.ID, "OTH") },
		func(c *core.Core, f fixture) error { return c.DeleteProject(bg, f.Owner.ID, f.Other.ID, "VAQ") }})
}

func count(t *testing.T, d dbtest.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.R.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Review Focus 1 and 3: everything about the project goes; tickets in other
// projects lose links into it; search forgets it.
func TestDeleteProjectRemovesEverything(t *testing.T) {
	c, d, f := newFixture(t)
	// Cross-project links both ways, and a comment with a findable word.
	if _, err := c.AddLink(bg, f.Owner.ID, f.O1.ID, "blocks", core.TicketKey{Project: "VAQ", Number: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddLink(bg, f.Owner.ID, f.T2.ID, "relates", core.TicketKey{Project: "OTH", Number: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddComment(bg, f.Owner.ID, f.T1.ID, "zanzibar"); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(c, f.Owner.ID, f.T1.ID, "gone.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(c, f.Owner.ID, f.O1.ID, "stays.png"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "vaq"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM attachments`); n != 1 {
		t.Errorf("attachments left: %d, want the other project's one", n)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM projects WHERE id = ?`:           0,
		`SELECT count(*) FROM tickets WHERE project_id = ?`:    0,
		`SELECT count(*) FROM statuses WHERE project_id = ?`:   0,
		`SELECT count(*) FROM milestones WHERE project_id = ?`: 0,
		`SELECT count(*) FROM events WHERE project_id = ?`:     0,
	} {
		if n := count(t, d, q, f.Project.ID); n != want {
			t.Errorf("%s: %d", q, n)
		}
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM comments`:                              0,
		`SELECT count(*) FROM ticket_links`:                          0,
		`SELECT count(*) FROM ticket_tags`:                           0,
		`SELECT count(*) FROM tickets`:                               1, // OTH-1 survives
		`SELECT count(*) FROM projects`:                              1,
		`SELECT count(*) FROM events WHERE kind = 'project.deleted'`: 1,
	} {
		if n := count(t, d, q); n != want {
			t.Errorf("%s: %d, want %d", q, n, want)
		}
	}
	for _, word := range []string{`"zanzibar"*`, `"first"*`} {
		if hits, err := appdb.SearchTickets(bg, d.R, word, 10); err != nil || len(hits) != 0 {
			t.Errorf("%s: %d hits, %v", word, len(hits), err)
		}
	}
	if hits, _ := appdb.SearchTickets(bg, d.R, `"elsewhere"*`, 10); len(hits) != 1 {
		t.Error("the other project's ticket left the index")
	}
	if p := lastPayload(t, c); p["key"] != "VAQ" || p["name"] != "Vaquero" {
		t.Fatalf("%v", p)
	}
}

// Review Focus 2.
func TestDeleteConfirmation(t *testing.T) {
	c, d, f := newFixture(t)
	snap := snapshot(t, d)
	err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "OTH")
	fieldErr(t, err, "confirm", "Type VAQ to confirm.")
	fieldErr(t, c.DeleteProject(bg, f.Owner.ID, f.Project.ID, ""), "confirm", "Type VAQ to confirm.")
	if got := snapshot(t, d); len(got) != len(snap) || got["tickets"] != snap["tickets"] {
		t.Fatal("refused delete changed rows")
	}
}
