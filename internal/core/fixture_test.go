package core_test

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// fixture is the shared starting state for operation tests: project VAQ
// with its default statuses plus an empty extra one, tickets VAQ-1 (tagged
// "bug", with one comment) and VAQ-2 (which "relates" to VAQ-1), milestone
// "M1", and project OTH with ticket OTH-1.
type fixture struct {
	Owner     gen.Actor
	Project   gen.Project
	Statuses  []gen.Status // VAQ's, by position; the last is the empty "Extra" (todo), moved there
	T1, T2    gen.Ticket // T1 tagged bug, T2 tagged ui
	Milestone gen.Milestone
	Other     gen.Project
	O1        gen.Ticket
	OtherTodo gen.Status     // OTH's Todo
	Comment   gen.Comment    // on T1
	Link      gen.TicketLink // T2 relates to T1
	Token     gen.ApiToken   // the owner's, live
	Bug, UI   gen.Tag        // T1's and T2's tags
}

func newFixture(t *testing.T) (*core.Core, dbtest.DB, fixture) {
	t.Helper()
	ctx := context.Background()
	c, d, owner := newCore(t)
	f := fixture{Owner: owner}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.Project, err = c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	must(err)
	extra, err := c.CreateStatus(ctx, owner.ID, f.Project.ID, "Extra", "todo")
	must(err)
	for range 4 { // from after Todo to the end, past In progress … Cancelled
		must(c.MoveStatus(ctx, owner.ID, extra.ID, +1))
	}
	f.Statuses, err = d.RQ.ListStatuses(ctx, f.Project.ID)
	must(err)
	f.T1, err = c.CreateTicket(ctx, owner.ID, f.Project.ID, core.TicketInput{Title: "First", Tags: []string{"bug"}})
	must(err)
	f.T2, err = c.CreateTicket(ctx, owner.ID, f.Project.ID, core.TicketInput{Title: "Second", Tags: []string{"ui"}})
	must(err)
	for _, tg := range [...]struct {
		into *gen.Tag
		tk   gen.Ticket
	}{{&f.Bug, f.T1}, {&f.UI, f.T2}} {
		tt, err := d.RQ.ListTicketTags(ctx, tg.tk.ID)
		must(err)
		*tg.into = tt[0]
	}
	f.Milestone, err = c.CreateMilestone(ctx, owner.ID, f.Project.ID, core.MilestoneInput{Name: "M1"})
	must(err)
	f.Other, err = c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	must(err)
	f.O1, err = c.CreateTicket(ctx, owner.ID, f.Other.ID, core.TicketInput{Title: "Elsewhere"})
	must(err)
	f.OtherTodo, err = d.RQ.GetStatus(ctx, f.O1.StatusID)
	must(err)
	f.Comment, err = c.AddComment(ctx, owner.ID, f.T1.ID, "First comment.")
	must(err)
	f.Link, err = c.AddLink(ctx, owner.ID, f.T2.ID, "relates", core.TicketKey{Project: "VAQ", Number: 1})
	must(err)
	_, f.Token, err = c.CreateToken(ctx, owner.ID, owner.ID, "fixture")
	must(err)
	return c, d, f
}

// input is the TicketInput that reproduces tk unchanged (tags given).
func input(tk gen.Ticket, tags ...string) core.TicketInput {
	return core.TicketInput{
		Title: tk.Title, Body: tk.Body, StatusID: tk.StatusID, Priority: tk.Priority,
		AssigneeID: tk.AssigneeID.String, ParentID: tk.ParentID.String, MilestoneID: tk.MilestoneID.String,
		Tags: tags,
	}
}

var bg = context.Background()

func init() {
	ops = append(ops,
		coreOp{"CreateProject",
			func(c *core.Core, f fixture) error {
				_, err := c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: "NEW", Name: "New"})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: "x", Name: "New"})
				return err
			}},
		coreOp{"UpdateProject",
			func(c *core.Core, f fixture) error {
				_, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, "Renamed", "")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, " ", "")
				return err
			}},
		coreOp{"SetProjectArchived",
			func(c *core.Core, f fixture) error { return c.SetProjectArchived(bg, f.Owner.ID, f.Project.ID, true) },
			func(c *core.Core, f fixture) error { return c.SetProjectArchived(bg, f.Owner.ID, f.Project.ID, false) }},
		coreOp{"CreateStatus",
			func(c *core.Core, f fixture) error {
				_, err := c.CreateStatus(bg, f.Owner.ID, f.Project.ID, "Blocked", "in_progress")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.CreateStatus(bg, f.Owner.ID, f.Project.ID, "Blocked", "nope")
				return err
			}},
		coreOp{"RenameStatus",
			func(c *core.Core, f fixture) error { return c.RenameStatus(bg, f.Owner.ID, f.Statuses[0].ID, "Icebox") },
			func(c *core.Core, f fixture) error { return c.RenameStatus(bg, f.Owner.ID, f.Statuses[0].ID, "Todo") }},
		coreOp{"MoveStatus",
			func(c *core.Core, f fixture) error { return c.MoveStatus(bg, f.Owner.ID, f.Statuses[0].ID, +1) },
			func(c *core.Core, f fixture) error { return c.MoveStatus(bg, f.Owner.ID, f.Statuses[0].ID, -1) }},
		coreOp{"CreateStatusAt",
			func(c *core.Core, f fixture) error {
				_, err := c.CreateStatusAt(bg, f.Owner.ID, f.Project.ID, "Blocked", "in_progress", core.StatusPlace{Before: f.Statuses[0].ID})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.CreateStatusAt(bg, f.Owner.ID, f.Project.ID, "Blocked", "in_progress", core.StatusPlace{After: f.OtherTodo.ID})
				return err
			}},
		coreOp{"MoveStatusTo",
			func(c *core.Core, f fixture) error {
				return c.MoveStatusTo(bg, f.Owner.ID, f.Statuses[len(f.Statuses)-1].ID, core.StatusPlace{After: f.Statuses[1].ID})
			},
			func(c *core.Core, f fixture) error {
				return c.MoveStatusTo(bg, f.Owner.ID, f.Statuses[0].ID, core.StatusPlace{Before: f.Statuses[0].ID})
			}},
		coreOp{"DeleteStatus",
			func(c *core.Core, f fixture) error {
				return c.DeleteStatus(bg, f.Owner.ID, f.Statuses[len(f.Statuses)-1].ID)
			},
			func(c *core.Core, f fixture) error { return c.DeleteStatus(bg, f.Owner.ID, f.T1.StatusID) }},
		coreOp{"CreateMilestone",
			func(c *core.Core, f fixture) error {
				_, err := c.CreateMilestone(bg, f.Owner.ID, f.Project.ID, core.MilestoneInput{Name: "M2", DueOn: "2026-12-31"})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.CreateMilestone(bg, f.Owner.ID, f.Project.ID, core.MilestoneInput{Name: "M1"})
				return err
			}},
		coreOp{"UpdateMilestone",
			func(c *core.Core, f fixture) error {
				return c.UpdateMilestone(bg, f.Owner.ID, f.Milestone.ID, core.MilestoneInput{Name: "M1", DueOn: "2027-01-15"})
			},
			func(c *core.Core, f fixture) error {
				return c.UpdateMilestone(bg, f.Owner.ID, f.Milestone.ID, core.MilestoneInput{Name: "M1", DueOn: "tomorrow"})
			}},
		coreOp{"SetMilestoneClosed",
			func(c *core.Core, f fixture) error { return c.SetMilestoneClosed(bg, f.Owner.ID, f.Milestone.ID, true) },
			func(c *core.Core, f fixture) error {
				return c.SetMilestoneClosed(bg, f.Owner.ID, f.Milestone.ID, false)
			}},
		coreOp{"CreateTicket",
			func(c *core.Core, f fixture) error {
				_, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third", Tags: []string{"new-tag"}})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "  ", Tags: []string{"never-made"}})
				return err
			}},
		coreOp{"MoveTicket",
			func(c *core.Core, f fixture) error {
				_, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[2].ID, "", "")
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.OtherTodo.ID, "", "")
				return err
			}},
		coreOp{"UpdateTicket",
			func(c *core.Core, f fixture) error {
				in := input(f.T1, "bug", "ui")
				in.Title = "First, renamed"
				_, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in)
				return err
			},
			func(c *core.Core, f fixture) error {
				in := input(f.T1, "never-made")
				in.ParentID = f.T1.ID
				_, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in)
				return err
			}},
	)
}
