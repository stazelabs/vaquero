package board_test

import (
	"context"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
)

type env struct {
	d     dbtest.DB
	c     *core.Core
	owner gen.Actor
	p     gen.Project
	st    map[string]gen.Status
	now   time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{d: dbtest.New(t), now: t0, st: map[string]gen.Status{}}
	e.c = core.New(e.d.W, func() time.Time { return e.now })
	var err error
	if e.owner, err = e.c.EnsureOwner(ctx); err != nil {
		t.Fatal(err)
	}
	if e.p, err = e.c.CreateProject(ctx, e.owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"}); err != nil {
		t.Fatal(err)
	}
	ss, _ := e.d.RQ.ListStatuses(ctx, e.p.ID)
	for _, s := range ss {
		e.st[s.Name] = s
	}
	return e
}

func (e *env) ticket(t *testing.T, in core.TicketInput) gen.Ticket {
	t.Helper()
	tk, err := e.c.CreateTicket(ctx, e.owner.ID, e.p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func (e *env) load(t *testing.T, f board.Filter) board.Board {
	t.Helper()
	b, err := board.Load(ctx, e.d.RQ, e.p, e.owner, f, e.now)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func names(cols []board.Column) []string {
	var out []string
	for _, c := range cols {
		out = append(out, c.Status.Name)
	}
	return out
}

func titles(c board.Column) []string {
	var out []string
	for _, k := range c.Cards {
		out = append(out, k.Title)
	}
	return out
}

func any_() board.Filter { return board.Filter{Priority: -1} }

func TestLoadSplitsActiveAndRails(t *testing.T) {
	e := newEnv(t)
	b := e.load(t, any_())
	if got := names(b.Active); len(got) != 4 || got[0] != "Todo" || got[3] != "Done" {
		t.Fatalf("active %v", got)
	}
	if got := names(b.Rails); len(got) != 2 || got[0] != "Backlog" || got[1] != "Cancelled" {
		t.Fatalf("rails %v", got)
	}
}

func TestCardsCarryFields(t *testing.T) {
	e := newEnv(t)
	e.ticket(t, core.TicketInput{Title: "Build", Priority: 1, Tags: []string{"ui"}, AssigneeID: e.owner.ID})
	e.ticket(t, core.TicketInput{Title: "Ready one"})
	cards := e.load(t, any_()).Active[0].Cards
	if len(cards) != 2 {
		t.Fatalf("%d cards", len(cards))
	}
	c := cards[0]
	if c.Key != "VAQ-1" || c.Priority != 1 || c.Assignee != "Owner" || c.AssigneeIsAgent || c.Ready || len(c.Tags) != 1 || c.Tags[0] != "ui" {
		t.Fatalf("%+v", c)
	}
	if !cards[1].Ready {
		t.Fatal("unassigned todo ticket not ready")
	}
}

func TestDoneWindowHidesOld(t *testing.T) {
	e := newEnv(t)
	e.ticket(t, core.TicketInput{Title: "old", StatusID: e.st["Done"].ID})
	e.now = t0.Add(8 * 24 * time.Hour)
	e.ticket(t, core.TicketInput{Title: "new", StatusID: e.st["Done"].ID})
	done := e.load(t, any_()).Active[3]
	if got := titles(done); len(got) != 1 || got[0] != "new" || done.Older != 1 {
		t.Fatalf("%v older=%d", got, done.Older)
	}
	list, err := board.LoadList(ctx, e.d.RQ, e.p, e.owner, any_())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Status.Name == "Done" && len(c.Cards) != 2 {
			t.Fatalf("list hides done: %v", titles(c))
		}
	}
}

func TestLoadAppliesFilters(t *testing.T) {
	e := newEnv(t)
	e.ticket(t, core.TicketInput{Title: "mine", AssigneeID: e.owner.ID})
	e.ticket(t, core.TicketInput{Title: "tagged", Tags: []string{"ui"}, Priority: 3})
	count := func(f board.Filter) int {
		n := 0
		for _, c := range e.load(t, f).Active {
			n += len(c.Cards)
		}
		return n
	}
	for name, tc := range map[string]struct {
		f    board.Filter
		want int
	}{
		"any":      {any_(), 2},
		"mine":     {any_().With("mine", "1"), 1},
		"agents":   {any_().With("agents", "1"), 0},
		"tag":      {any_().With("tag", "UI"), 1},
		"priority": {any_().With("priority", "3"), 1},
		"none":     {any_().With("milestone", "none"), 2},
		"garbage":  {any_().With("milestone", "garbage"), 0},
		"nosuch":   {any_().With("tag", "no-such-tag"), 0},
	} {
		if got := count(tc.f); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}

func TestMilestonePanelCounts(t *testing.T) {
	e := newEnv(t)
	m, _ := e.c.CreateMilestone(ctx, e.owner.ID, e.p.ID, core.MilestoneInput{Name: "Beta", DueOn: "2026-12-15"})
	closed, _ := e.c.CreateMilestone(ctx, e.owner.ID, e.p.ID, core.MilestoneInput{Name: "Alpha"})
	_ = e.c.SetMilestoneClosed(ctx, e.owner.ID, closed.ID, true)
	e.ticket(t, core.TicketInput{Title: "a", MilestoneID: m.ID, StatusID: e.st["Done"].ID})
	e.ticket(t, core.TicketInput{Title: "b", MilestoneID: m.ID})
	e.ticket(t, core.TicketInput{Title: "c"})
	b := e.load(t, any_())
	if len(b.Milestones) != 1 || b.Milestones[0].Name != "Beta" || b.Milestones[0].Done != 1 || b.Milestones[0].Total != 2 || b.Milestones[0].Due != "Dec 15, 2026" {
		t.Fatalf("%+v", b.Milestones)
	}
	if b.NoMilestone != 1 {
		t.Fatalf("no milestone %d", b.NoMilestone)
	}
}
