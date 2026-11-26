package board_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// countingDB counts the queries run through it.
type countingDB struct {
	gen.DBTX
	n int
}

func (c *countingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.n++
	return c.DBTX.QueryContext(ctx, q, args...)
}

func (c *countingDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.n++
	return c.DBTX.QueryRowContext(ctx, q, args...)
}

// statsProject makes a project with a ready ticket, a blocked one, one in
// review, one done, and with the workflow on when workflow is set.
func statsProject(t *testing.T, e *env, key string, workflow bool) gen.Project {
	t.Helper()
	p, err := e.c.CreateProject(ctx, e.owner.ID, core.ProjectInput{Key: key, Name: key + " project"})
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	ss, _ := e.d.RQ.ListStatuses(ctx, p.ID)
	for _, s := range ss {
		st[s.Name] = s.ID
	}
	mk := func(title, status string) gen.Ticket {
		tk, err := e.c.CreateTicket(ctx, e.owner.ID, p.ID, core.TicketInput{Title: title, StatusID: st[status]})
		if err != nil {
			t.Fatal(err)
		}
		return tk
	}
	blocker := mk("Ready", "Todo")
	blocked := mk("Blocked", "Todo")
	if _, err := e.c.AddLink(ctx, e.owner.ID, blocker.ID, "blocks", core.TicketKey{Project: p.Key, Number: blocked.Number}); err != nil {
		t.Fatal(err)
	}
	mk("Reviewing", "In review")
	mk("Finished", "Done")
	if workflow {
		on := true
		if _, err := e.c.UpdateProject(ctx, e.owner.ID, p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// VAQ-102: each project's counts, open milestone and last change, with
// the ready count the board's own (and with the workflow on, readiness's).
func TestProjectStats(t *testing.T) {
	e := newEnv(t) // VAQ: empty
	a := statsProject(t, e, "AAA", false)
	b := statsProject(t, e, "BBB", true)
	// AAA's milestones: an undated one, then one due soonest wins.
	if _, err := e.c.CreateMilestone(ctx, e.owner.ID, a.ID, core.MilestoneInput{Name: "Undated"}); err != nil {
		t.Fatal(err)
	}
	soon, err := e.c.CreateMilestone(ctx, e.owner.ID, a.ID, core.MilestoneInput{Name: "Soon", DueOn: "2026-12-01"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.c.CreateMilestone(ctx, e.owner.ID, a.ID, core.MilestoneInput{Name: "Later", DueOn: "2027-03-01"}); err != nil {
		t.Fatal(err)
	}
	ss, _ := e.d.RQ.ListStatuses(ctx, a.ID)
	done := ss[0].ID
	for _, s := range ss {
		if s.Category == "done" {
			done = s.ID
		}
	}
	e.now = t0.Add(time.Hour)
	for i, status := range []string{done, ""} {
		if _, err := e.c.CreateTicket(ctx, e.owner.ID, a.ID, core.TicketInput{Title: fmt.Sprint("M", i), StatusID: status, MilestoneID: soon.ID}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := board.LoadProjectStats(ctx, e.d.RQ)
	if err != nil {
		t.Fatal(err)
	}
	if s := got[e.p.ID]; s.Ready != 0 || s.InReview != 0 || s.Open != 0 || s.Milestone != nil || !s.LastChange.Equal(t0) {
		t.Errorf("empty VAQ: %+v", s)
	}
	sa := got[a.ID]
	if sa.Ready != 2 || sa.InReview != 1 || sa.Open != 4 {
		t.Errorf("AAA counts: %+v", sa)
	}
	if sa.Milestone == nil || sa.Milestone.Name != "Soon" || sa.Milestone.Done != 1 || sa.Milestone.Total != 2 {
		t.Errorf("AAA milestone: %+v", sa.Milestone)
	}
	if !sa.LastChange.Equal(t0.Add(time.Hour)) {
		t.Errorf("AAA last change %v", sa.LastChange)
	}
	// BBB has the workflow on: its one board-ready ticket has no tier and
	// no approval, so nothing is ready.
	if sb := got[b.ID]; sb.Ready != 0 || sb.InReview != 1 || sb.Open != 3 || sb.Milestone != nil {
		t.Errorf("BBB: %+v", sb)
	}
	// The ready counts are the board's, project by project.
	for _, p := range []gen.Project{e.p, a, b} {
		p, _ := e.d.RQ.GetProject(ctx, p.ID)
		cols, err := board.LoadList(ctx, e.d.RQ, p, e.owner, board.Filter{Priority: -1})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, c := range cols {
			for _, k := range c.Cards {
				if k.Ready {
					n++
				}
			}
		}
		if got[p.ID].Ready != n {
			t.Errorf("%s: %d ready, the board has %d", p.Key, got[p.ID].Ready, n)
		}
	}
}

// VAQ-102: the stats take the same number of queries for one project as
// for six: one per stat across all projects, never one per project.
func TestProjectsStatsOneQuery(t *testing.T) {
	count := func(projects int) int {
		e := newEnv(t)
		for i := range projects {
			p := statsProject(t, e, fmt.Sprintf("P%d", i), i%2 == 1)
			if _, err := e.c.CreateMilestone(ctx, e.owner.ID, p.ID, core.MilestoneInput{Name: "M"}); err != nil {
				t.Fatal(err)
			}
		}
		cdb := &countingDB{DBTX: e.d.R}
		got, err := board.LoadProjectStats(ctx, gen.New(cdb))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != projects+1 {
			t.Fatalf("stats for %d projects, want %d", len(got), projects+1)
		}
		return cdb.n
	}
	one, six := count(2), count(6)
	if one != six {
		t.Fatalf("%d queries for 2 projects, %d for 6", one, six)
	}
	// Three stats queries, then readiness's batch (three for tickets
	// without parents).
	if six > 6 {
		t.Fatalf("%d queries, want at most 6", six)
	}
}
