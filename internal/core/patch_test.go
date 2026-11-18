package core_test

import (
	"errors"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops, coreOp{"PatchTicket",
		func(c *core.Core, f fixture) error {
			_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("Patched")}, f.T1.Version)
			return err
		},
		func(c *core.Core, f fixture) error {
			_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("Patched")}, f.T1.Version+5)
			return err
		}})
}

func tags(t *testing.T, c *core.Core, f fixture, id string) []string {
	t.Helper()
	rows, _ := c.DB.Query(`SELECT g.name FROM tags g JOIN ticket_tags tt ON tt.tag_id = g.id WHERE tt.ticket_id = ? ORDER BY lower(g.name)`, id)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestPatchChangesOnlyNamedFields(t *testing.T) {
	c, _, f := newFixture(t)
	parent, _ := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "parent"})
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{ParentID: new(parent.ID), Priority: new(int64(2))}, 0); err != nil {
		t.Fatal(err)
	}
	tk, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{StatusID: new(f.Statuses[2].ID)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tk.StatusID != f.Statuses[2].ID || tk.Title != "First" || tk.ParentID.String != parent.ID || tk.Priority != 2 {
		t.Fatalf("%+v", tk)
	}
	if got := tags(t, c, f, f.T1.ID); len(got) != 1 || got[0] != "bug" {
		t.Fatalf("tags %v", got)
	}
}

// The backlog item: an update that names no status keeps it.
func TestPatchEmptyStatusKeeps(t *testing.T) {
	c, _, f := newFixture(t)
	moved, _ := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, "", "")
	tk, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("Renamed")}, 0)
	if err != nil || tk.StatusID != moved.StatusID {
		t.Fatalf("%v status %s", err, tk.StatusID)
	}
}

func TestPatchTagsAddRemove(t *testing.T) {
	c, _, f := newFixture(t)
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{AddTags: []string{"ui", "BUG"}, RemoveTags: []string{"Bug"}}, 0); err != nil {
		t.Fatal(err)
	}
	if got := tags(t, c, f, f.T1.ID); len(got) != 1 || got[0] != "ui" {
		t.Fatalf("%v", got)
	}
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{SetTags: true}, 0); err != nil {
		t.Fatal(err)
	}
	if got := tags(t, c, f, f.T1.ID); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestPatchClearsParent(t *testing.T) {
	c, _, f := newFixture(t)
	child, _ := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "c", ParentID: f.T1.ID})
	tk, err := c.PatchTicket(bg, f.Owner.ID, child.ID, core.TicketPatch{ParentID: new("")}, 0)
	if err != nil || tk.ParentID.Valid {
		t.Fatalf("%v %+v", err, tk.ParentID)
	}
}

// Review Focus 1.
func TestStaleVersionRefused(t *testing.T) {
	c, d, f := newFixture(t)
	v := f.T1.Version
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("A")}, v); err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{Title: new("B")}, v)
	var conflict *core.Conflict
	if !errors.As(err, &conflict) || conflict.Current.Version != v+1 || conflict.Current.Title != "A" {
		t.Fatalf("%v", err)
	}
	if err.Error() != "VAQ-1 changed since you read it." {
		t.Fatal(err.Error())
	}
	if eventCount(t, d) != before {
		t.Fatal("refused save wrote an event")
	}
}

func TestVersionRisesOnMoveAndUpdate(t *testing.T) {
	c, _, f := newFixture(t)
	tk, _ := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, "", "")
	if tk.Version != f.T1.Version+1 {
		t.Fatalf("move: %d", tk.Version)
	}
	in := input(tk, "bug")
	in.Title = "x"
	tk2, _ := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in)
	if tk2.Version != tk.Version+1 {
		t.Fatalf("update: %d", tk2.Version)
	}
	// A no-op leaves it.
	tk3, _ := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, input(tk2, "bug"))
	if tk3.Version != tk2.Version {
		t.Fatalf("no-op: %d", tk3.Version)
	}
}
