package core_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// column is the titles in a status, top to bottom.
func column(t *testing.T, d dbtest.DB, statusID string) []string {
	t.Helper()
	rows, err := d.R.Query(`SELECT title FROM tickets WHERE status_id = ? ORDER BY position`, statusID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func lastKind(t *testing.T, c *core.Core) string {
	t.Helper()
	var k string
	if err := c.DB.QueryRow(`SELECT kind FROM events ORDER BY seq DESC LIMIT 1`).Scan(&k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestNewTicketHasStatusTime(t *testing.T) {
	_, _, f := newFixture(t)
	if f.T1.StatusChangedAt != timefmt.Format(start) {
		t.Fatalf("%q", f.T1.StatusChangedAt)
	}
}

func TestMoveBetweenColumns(t *testing.T) {
	c, _, f := newFixture(t)
	later := start.Add(time3h)
	c.Now = func() timeT { return later }
	tk, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if tk.StatusID != f.Statuses[2].ID || tk.StatusChangedAt != timefmt.Format(later) {
		t.Fatalf("%+v", tk)
	}
	if lastKind(t, c) != core.EvTicketUpdated {
		t.Fatal(lastKind(t, c))
	}
	ch := lastPayload(t, c)["changes"].(map[string]any)["status"].(map[string]any)
	if ch["from"] != "Todo" || ch["to"] != "In progress" {
		t.Fatalf("%v", ch)
	}
}

func TestUpdateTicketStatusChangeSetsStatusTime(t *testing.T) {
	c, _, f := newFixture(t)
	later := start.Add(time3h)
	c.Now = func() timeT { return later }
	in := input(f.T2)
	in.Title = "retitled only"
	tk, _ := c.UpdateTicket(bg, f.Owner.ID, f.T2.ID, in)
	if tk.StatusChangedAt != timefmt.Format(start) {
		t.Fatalf("retitle moved the status time: %q", tk.StatusChangedAt)
	}
	in.StatusID = f.Statuses[4].ID
	tk, _ = c.UpdateTicket(bg, f.Owner.ID, f.T2.ID, in)
	if tk.StatusChangedAt != timefmt.Format(later) {
		t.Fatalf("%q", tk.StatusChangedAt)
	}
}

func TestReorderWithinColumn(t *testing.T) {
	c, d, f := newFixture(t)
	t3, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.T1.StatusID, f.T2.ID, t3.ID); err != nil {
		t.Fatal(err)
	}
	if got := column(t, d, f.T1.StatusID); !eq(got, []string{"Second", "First", "Third"}) {
		t.Fatalf("%v", got)
	}
	if lastKind(t, c) != core.EvTicketReordered {
		t.Fatal(lastKind(t, c))
	}
	// To the top.
	if _, err := c.MoveTicket(bg, f.Owner.ID, t3.ID, f.T1.StatusID, "", f.T2.ID); err != nil {
		t.Fatal(err)
	}
	if got := column(t, d, f.T1.StatusID); !eq(got, []string{"Third", "Second", "First"}) {
		t.Fatalf("%v", got)
	}
}

// Review Focus 2: a neighbour from a stale page that is no longer in the
// target column is ignored; the card lands at the end.
func TestMoveIgnoresForeignNeighbour(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.MoveTicket(bg, f.Owner.ID, f.T2.ID, f.Statuses[2].ID, "", ""); err != nil {
		t.Fatal(err)
	}
	t3, _ := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third", StatusID: f.Statuses[2].ID})
	// Drop T1 into In progress "after" O1 (another project) and "before" T1 itself.
	if _, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, f.O1.ID, f.T1.ID); err != nil {
		t.Fatal(err)
	}
	if got := column(t, d, f.Statuses[2].ID); !eq(got, []string{"Second", t3.Title, "First"}) {
		t.Fatalf("%v", got)
	}
}

func TestMoveToOtherProjectStatusRefused(t *testing.T) {
	c, _, f := newFixture(t)
	_, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.OtherTodo.ID, "", "")
	fieldErr(t, err, "status", "Choose a status from this project.")
}

// Review Focus 3: sixty drops into the same gap halve it sixty times; the
// column is renumbered before the gap vanishes and order holds.
func TestRepeatedInsertsInOneGapKeepOrder(t *testing.T) {
	c, d, f := newFixture(t)
	todo := f.T1.StatusID // column: First, Second
	ip := f.Statuses[2].ID
	want := []string{"First"}
	prev := f.T2.ID // each new card goes between First and the previous one
	var made []gen.Ticket
	for i := range 60 {
		tk, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "n" + itoa(i), StatusID: ip})
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, tk)
		if _, err := c.MoveTicket(bg, f.Owner.ID, tk.ID, todo, f.T1.ID, prev); err != nil {
			t.Fatal(err)
		}
		prev = tk.ID
	}
	for _, m := range slices.Backward(made) {
		want = append(want, m.Title)
	}
	want = append(want, "Second")
	if got := column(t, d, todo); !eq(got, want) {
		t.Fatalf("order lost:\n got %v\nwant %v", got, want)
	}
}

func TestMoveInPlaceWritesNothing(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	if _, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.T1.StatusID, "", f.T2.ID); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("a drop in place wrote an event")
	}
}

// A failed status lookup that isn't "no such status" is a server error, not
// a field error telling the user to choose another status.
func TestMoveTicketStatusLookupFailureIsNotAFieldError(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := d.W.Exec(`ALTER TABLE statuses RENAME TO statuses_gone`); err != nil {
		t.Fatal(err)
	}
	_, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, "", "")
	if err == nil {
		t.Fatal("want an error")
	}
	if _, ok := errors.AsType[*core.Invalid](err); ok {
		t.Fatalf("a database failure was reported as a field error: %v", err)
	}
}
