package sweep_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/sweep"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

var bg = context.Background()

func TestSweepExpires(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	c := core.New(d.W, clock)
	owner, _ := c.EnsureOwner(bg)
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := c.CreateTicket(bg, owner.ID, p.ID, core.TicketInput{Title: "x"})
	a, _ := c.EnsureAgent(bg, owner.ID, "builder")
	if _, err := c.Claim(bg, a.ID, tk.ID, 0); err != nil {
		t.Fatal(err)
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: clock}
	if n, err := s.Tick(bg); n != 0 || err != nil {
		t.Fatalf("early: %d %v", n, err)
	}
	now = now.Add(16 * time.Minute)
	if n, err := s.Tick(bg); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if got, _ := d.RQ.GetTicket(bg, tk.ID); got.ClaimedBy.Valid || got.AssigneeID.Valid {
		t.Fatalf("%+v", got)
	}
}

func TestSweeperStartStop(t *testing.T) {
	d := dbtest.New(t)
	c := core.New(d.W, time.Now)
	ticks := make(chan struct{}, 10)
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: time.Now, Interval: 5 * time.Millisecond,
		OnTick: func() { ticks <- struct{}{} }}
	s.Start()
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("never ticked")
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung")
	}
	s.Stop() // a second Stop is harmless
}

func TestMCPLogPruned(t *testing.T) {
	d := dbtest.New(t)
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	c := core.New(d.W, func() time.Time { return now })
	q := gen.New(d.W)
	for id, at := range map[string]time.Time{"01OLD": now.Add(-31 * 24 * time.Hour), "01NEW": now.Add(-29 * 24 * time.Hour)} {
		if err := q.RecordMCPCall(bg, gen.RecordMCPCallParams{ID: id, At: timefmt.Format(at), Tool: "search", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
	s := &sweep.Sweeper{Core: c, DB: d.W, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}
	if _, err := s.Tick(bg); err != nil {
		t.Fatal(err)
	}
	rows, _ := d.RQ.ListMCPCalls(bg, gen.ListMCPCallsParams{Lim: 10})
	if len(rows) != 1 || rows[0].ID != "01NEW" {
		t.Fatalf("%+v", rows)
	}
}
