package core_test

import (
	"errors"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func fieldErr(t *testing.T, err error, field, want string) {
	t.Helper()
	var inv *core.Invalid
	if !errors.As(err, &inv) {
		t.Fatalf("err = %v, want *core.Invalid", err)
	}
	if got := inv.Fields[field]; want != "" && got != want || want == "" && got == "" {
		t.Fatalf("field %q = %q, want %q (all: %v)", field, got, want, inv.Fields)
	}
}

func TestCreateProjectSeedsStatuses(t *testing.T) {
	c, d, owner := newCore(t)
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := d.RQ.ListStatuses(bg, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"Backlog", "backlog"}, {"Todo", "todo"}, {"In progress", "in_progress"}, {"In review", "in_review"}, {"Done", "done"}, {"Cancelled", "cancelled"}}
	if len(ss) != len(want) {
		t.Fatalf("%d statuses", len(ss))
	}
	for i, s := range ss {
		if s.Name != want[i][0] || s.Category != want[i][1] {
			t.Errorf("status %d = %s/%s, want %v", i, s.Name, s.Category, want[i])
		}
	}
}

func TestCreateProjectRefusesBadOrTakenKey(t *testing.T) {
	c, _, owner := newCore(t)
	_, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "vaq", Name: "V"})
	fieldErr(t, err, "key", "Use 2 to 10 capital letters or digits, starting with a letter.")
	if _, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "V"}); err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "V"})
	fieldErr(t, err, "key", "That key is taken.")
	_, err = c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "NEW", Name: ""})
	fieldErr(t, err, "name", "Enter a name.")
}

func TestDeleteStatusRules(t *testing.T) {
	c, _, f := newFixture(t)
	fieldErr(t, c.DeleteStatus(bg, f.Owner.ID, f.T1.StatusID), "status", "Move this status's tickets first.")
	var done string
	for _, s := range f.Statuses {
		if s.Category == "done" {
			done = s.ID
		}
	}
	fieldErr(t, c.DeleteStatus(bg, f.Owner.ID, done), "status", "A project needs at least one status in each category.")
	if err := c.DeleteStatus(bg, f.Owner.ID, f.Statuses[len(f.Statuses)-1].ID); err != nil {
		t.Fatal(err)
	}
}

func TestMoveStatusSwaps(t *testing.T) {
	c, d, f := newFixture(t)
	if err := c.MoveStatus(bg, f.Owner.ID, f.Statuses[0].ID, +1); err != nil {
		t.Fatal(err)
	}
	ss, _ := d.RQ.ListStatuses(bg, f.Project.ID)
	if ss[0].Name != "Todo" || ss[1].Name != "Backlog" {
		t.Fatalf("%s, %s", ss[0].Name, ss[1].Name)
	}
	fieldErr(t, c.MoveStatus(bg, f.Owner.ID, ss[len(ss)-1].ID, +1), "status", "")
}

func TestArchivedProjectRefusesNewTickets(t *testing.T) {
	c, _, f := newFixture(t)
	if err := c.SetProjectArchived(bg, f.Owner.ID, f.Project.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "x"})
	fieldErr(t, err, "project", "Restore this project to add tickets.")
}

func TestUnknownIDsAreNotFound(t *testing.T) {
	c, _, f := newFixture(t)
	for name, err := range map[string]error{
		"project":   c.SetProjectArchived(bg, f.Owner.ID, "nope", true),
		"status":    c.RenameStatus(bg, f.Owner.ID, "nope", "X"),
		"milestone": c.SetMilestoneClosed(bg, f.Owner.ID, "nope", true),
	} {
		if !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := c.UpdateTicket(bg, f.Owner.ID, "nope", core.TicketInput{Title: "x"})
	if !errors.Is(err, core.ErrNotFound) {
		t.Errorf("ticket: %v", err)
	}
}
