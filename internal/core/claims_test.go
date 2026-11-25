package core_test

import (
	"database/sql"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func init() {
	ops = append(ops,
		coreOp{"Claim",
			func(c *core.Core, f fixture) error { _, err := c.Claim(bg, f.Owner.ID, f.T1.ID, 0); return err },
			func(c *core.Core, f fixture) error {
				_, err := c.Claim(bg, f.Owner.ID, f.T1.ID, time.Second)
				return err
			}},
		// Release needs a claim first, which is an event of its own, so its
		// one event is counted in TestReleaseKeepsStatusAndAssignee and its
		// refusals in TestHeartbeatAndReleaseRules. Heartbeat is exempt: it
		// writes no event (see core/events.go).
	)
}

// clock is a movable now for a core.
type clock struct{ t time.Time }

func (k *clock) now() time.Time      { return k.t }
func (k *clock) add(d time.Duration) { k.t = k.t.Add(d) }
func newClock(c *core.Core) *clock   { k := &clock{t: start}; c.Now = k.now; return k }
func agent(t *testing.T, c *core.Core, owner gen.Actor, name string) gen.Actor {
	t.Helper()
	a, err := c.EnsureAgent(bg, owner.ID, name)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func status(t *testing.T, d dbtest.DB, id string) gen.Status {
	t.Helper()
	s, err := d.RQ.GetStatus(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ticket(t *testing.T, d dbtest.DB, id string) gen.Ticket {
	t.Helper()
	tk, err := d.RQ.GetTicket(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func invalidMsg(err error) string {
	var inv *core.Invalid
	if !errors.As(err, &inv) {
		return ""
	}
	return inv.Error()
}

func TestClaimMovesAndAssigns(t *testing.T) {
	c, d, f := newFixture(t)
	newClock(c)
	a := agent(t, c, f.Owner, "builder")
	before := eventCount(t, d)
	tk, err := c.Claim(bg, a.ID, f.T1.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s := status(t, d, tk.StatusID); s.Category != "in_progress" || s.Name != "In progress" {
		t.Fatalf("status %+v", s)
	}
	if tk.AssigneeID.String != a.ID || tk.ClaimedBy.String != a.ID || tk.ClaimPrevStatusID.String != f.T1.StatusID {
		t.Fatalf("%+v", tk)
	}
	if tk.ClaimExpiresAt.String != timefmt.Format(start.Add(core.DefaultLease)) || tk.Version != f.T1.Version+1 {
		t.Fatalf("lease %q version %d", tk.ClaimExpiresAt.String, tk.Version)
	}
	if eventCount(t, d)-before != 1 {
		t.Fatal("want one event")
	}
	evs, _ := d.RQ.ListTicketEvents(bg, sql.NullString{String: f.T1.ID, Valid: true})
	if evs[0].Kind != core.EvTicketClaimed {
		t.Fatalf("newest event %s", evs[0].Kind)
	}
}

// Review Focus 1.
func TestConcurrentClaimsOneWins(t *testing.T) {
	c, _, f := newFixture(t)
	newClock(c)
	agents := make([]gen.Actor, 10)
	for i := range agents {
		agents[i] = agent(t, c, f.Owner, "agent "+itoa(i))
	}
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i, a := range agents {
		wg.Go(func() { _, errs[i] = c.Claim(bg, a.ID, f.T1.ID, 0) })
	}
	wg.Wait()
	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("two winners: %d and %d", winner, i)
			}
			winner = i
		}
	}
	if winner < 0 {
		t.Fatalf("no winner: %v", errs)
	}
	want := "VAQ-1 is claimed by " + agents[winner].Name + " until 2026-11-18 12:15 UTC."
	for i, err := range errs {
		if i != winner && invalidMsg(err) != want {
			t.Errorf("agent %d: %v, want %q", i, err, want)
		}
	}
}

func TestClaimRefusals(t *testing.T) {
	for name, arrange := range map[string]func(*core.Core, fixture, gen.Actor) error{
		"done": func(c *core.Core, f fixture, _ gen.Actor) error {
			_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{StatusID: &f.Statuses[4].ID}, 0)
			return err
		},
		"cancelled": func(c *core.Core, f fixture, _ gen.Actor) error {
			_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{StatusID: &f.Statuses[5].ID}, 0)
			return err
		},
		"assigned": func(c *core.Core, f fixture, _ gen.Actor) error {
			_, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{AssigneeID: &f.Owner.ID}, 0)
			return err
		},
		"claimed": func(c *core.Core, f fixture, other gen.Actor) error {
			_, err := c.Claim(bg, other.ID, f.T1.ID, 0)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, d, f := newFixture(t)
			newClock(c)
			other := agent(t, c, f.Owner, "other")
			a := agent(t, c, f.Owner, "builder")
			if err := arrange(c, f, other); err != nil {
				t.Fatal(err)
			}
			snap := snapshot(t, d)
			_, err := c.Claim(bg, a.ID, f.T1.ID, 0)
			if invalidMsg(err) == "" {
				t.Fatalf("claim allowed: %v", err)
			}
			if !maps.Equal(snapshot(t, d), snap) {
				t.Fatal("a refused claim changed rows")
			}
		})
	}
}

func TestOwnClaimRenews(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	if _, err := c.Claim(bg, a.ID, f.T1.ID, 0); err != nil {
		t.Fatal(err)
	}
	k.add(10 * time.Minute)
	tk, err := c.Claim(bg, a.ID, f.T1.ID, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ClaimExpiresAt.String != timefmt.Format(k.t.Add(30*time.Minute)) || tk.ClaimPrevStatusID.String != f.T1.StatusID {
		t.Fatalf("%+v", tk)
	}
	if s := status(t, d, tk.StatusID); s.Category != "in_progress" {
		t.Fatal(s.Name)
	}
}

// Review Focus 3.
func TestHeartbeatAndReleaseRules(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	other := agent(t, c, f.Owner, "other")
	if _, err := c.Claim(bg, a.ID, f.T1.ID, 0); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	if _, err := c.Heartbeat(bg, other.ID, f.T1.ID, 0); invalidMsg(err) != "VAQ-1 is claimed by builder, not you." {
		t.Errorf("heartbeat by other: %v", err)
	}
	if _, err := c.Release(bg, other.ID, f.T1.ID); invalidMsg(err) != "VAQ-1 is claimed by builder, not you." {
		t.Errorf("release by other: %v", err)
	}
	if _, err := c.Heartbeat(bg, a.ID, f.T2.ID, 0); invalidMsg(err) != "VAQ-2 isn't claimed." {
		t.Errorf("heartbeat unclaimed: %v", err)
	}
	k.add(16 * time.Minute)
	lapsed := "Your claim on VAQ-1 lapsed at 2026-11-18 12:15 UTC; claim it again."
	if _, err := c.Heartbeat(bg, a.ID, f.T1.ID, 0); invalidMsg(err) != lapsed {
		t.Errorf("heartbeat lapsed: %v", err)
	}
	if _, err := c.Release(bg, a.ID, f.T1.ID); invalidMsg(err) != lapsed {
		t.Errorf("release lapsed: %v", err)
	}
	if !maps.Equal(snapshot(t, d), snap) {
		t.Fatal("refusals changed rows")
	}
}

func TestHeartbeatWritesNoEvent(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	claimed, err := c.Claim(bg, a.ID, f.T1.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	k.add(10 * time.Minute)
	tk, err := c.Heartbeat(bg, a.ID, f.T1.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ClaimExpiresAt.String != timefmt.Format(k.t.Add(core.DefaultLease)) || tk.Version != claimed.Version {
		t.Fatalf("%+v", tk)
	}
	if eventCount(t, d) != before {
		t.Fatal("heartbeat wrote an event")
	}
}

func TestReleaseKeepsStatusAndAssignee(t *testing.T) {
	c, d, f := newFixture(t)
	newClock(c)
	a := agent(t, c, f.Owner, "builder")
	claimed, _ := c.Claim(bg, a.ID, f.T1.ID, 0)
	before := eventCount(t, d)
	tk, err := c.Release(bg, a.ID, f.T1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.StatusID != claimed.StatusID || tk.AssigneeID.String != a.ID || tk.ClaimedBy.Valid || tk.ClaimExpiresAt.Valid {
		t.Fatalf("%+v", tk)
	}
	if eventCount(t, d)-before != 1 {
		t.Fatal("want one event")
	}
}

// Review Focus 2.
func TestExpireReturnsTicket(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	if _, err := c.Claim(bg, a.ID, f.T1.ID, 0); err != nil {
		t.Fatal(err)
	}
	k.add(14 * time.Minute)
	if n, err := c.ExpireClaims(bg); n != 0 || err != nil {
		t.Fatalf("early: %d %v", n, err)
	}
	k.add(2 * time.Minute)
	before := eventCount(t, d)
	if n, err := c.ExpireClaims(bg); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	tk := ticket(t, d, f.T1.ID)
	if tk.StatusID != f.T1.StatusID || tk.AssigneeID.Valid || tk.ClaimedBy.Valid || tk.ClaimExpiresAt.Valid || tk.ClaimPrevStatusID.Valid {
		t.Fatalf("%+v", tk)
	}
	if eventCount(t, d)-before != 1 {
		t.Fatal("want one event")
	}
	evs, _ := d.RQ.ListTicketEvents(bg, sql.NullString{String: f.T1.ID, Valid: true})
	if last := evs[0]; last.Kind != core.EvClaimLapsed || last.ActorID != a.ID {
		t.Fatalf("%+v", last)
	}
	rows, _ := d.RQ.ListBoardTickets(bg, gen.ListBoardTicketsParams{ProjectID: f.Project.ID})
	for _, r := range rows {
		if r.ID == f.T1.ID && !r.Ready {
			t.Fatal("not ready again after the lapse")
		}
	}
	if n, _ := c.ExpireClaims(bg); n != 0 {
		t.Fatal("expired twice")
	}
}

// An agent that moved its ticket on (to review, say) and then died leaves
// the work where it got to: the claim ends, the status and assignee stay.
func TestExpireAfterProgressKeepsStatus(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	c.Claim(bg, a.ID, f.T1.ID, 0)
	if _, err := c.PatchTicket(bg, a.ID, f.T1.ID, core.TicketPatch{StatusID: &f.Statuses[3].ID}, 0); err != nil {
		t.Fatal(err)
	}
	k.add(time.Hour)
	if n, _ := c.ExpireClaims(bg); n != 1 {
		t.Fatal(n)
	}
	tk := ticket(t, d, f.T1.ID)
	if tk.StatusID != f.Statuses[3].ID || tk.AssigneeID.String != a.ID || tk.ClaimedBy.Valid {
		t.Fatalf("%+v", tk)
	}
}

// Finishing the work ends the claim, so it can't lapse and move it back.
func TestDoneEndsClaim(t *testing.T) {
	c, d, f := newFixture(t)
	newClock(c)
	a := agent(t, c, f.Owner, "builder")
	c.Claim(bg, a.ID, f.T1.ID, 0)
	if _, err := c.MoveTicket(bg, a.ID, f.T1.ID, f.Statuses[4].ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if tk := ticket(t, d, f.T1.ID); tk.ClaimedBy.Valid || tk.ClaimExpiresAt.Valid {
		t.Fatalf("%+v", tk)
	}
}

func TestLeaseBounds(t *testing.T) {
	c, _, f := newFixture(t)
	newClock(c)
	a := agent(t, c, f.Owner, "builder")
	for _, bad := range []time.Duration{30 * time.Second, -time.Minute, 2*time.Hour + time.Minute} {
		if _, err := c.Claim(bg, a.ID, f.T1.ID, bad); !strings.Contains(invalidMsg(err), "1 to 120 minutes") {
			t.Errorf("%v: %v", bad, err)
		}
	}
	for _, good := range []time.Duration{core.MinLease, core.MaxLease} {
		if _, err := c.Claim(bg, a.ID, f.T1.ID, good); err != nil {
			t.Errorf("%v: %v", good, err)
		}
	}
}

// Review fix 1: a person who takes over a claimed ticket keeps it: the
// claim ends at the reassignment, and nothing lapses later.
func TestReassignEndsClaim(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	claimed, _ := c.Claim(bg, a.ID, f.T1.ID, 0)
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{AssigneeID: &f.Owner.ID}, 0); err != nil {
		t.Fatal(err)
	}
	if tk := ticket(t, d, f.T1.ID); tk.ClaimedBy.Valid || tk.ClaimExpiresAt.Valid {
		t.Fatalf("claim kept after reassignment: %+v", tk)
	}
	if _, err := c.Heartbeat(bg, a.ID, f.T1.ID, 0); invalidMsg(err) == "" {
		t.Fatal("the old claimant renewed a claim that ended")
	}
	k.add(time.Hour)
	if n, _ := c.ExpireClaims(bg); n != 0 {
		t.Fatalf("lapsed %d", n)
	}
	if tk := ticket(t, d, f.T1.ID); tk.StatusID != claimed.StatusID || tk.AssigneeID.String != f.Owner.ID {
		t.Fatalf("%+v", tk)
	}
}

// Review fix 2: a claimed ticket a person moved back to todo loses its dead
// claimant when the claim lapses, so it is ready again.
func TestLapseInTodoUnassigns(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "builder")
	c.Claim(bg, a.ID, f.T1.ID, 0)
	if _, err := c.MoveTicket(bg, f.Owner.ID, f.T1.ID, f.Statuses[1].ID, "", ""); err != nil {
		t.Fatal(err)
	}
	k.add(time.Hour)
	if n, _ := c.ExpireClaims(bg); n != 1 {
		t.Fatal(n)
	}
	tk := ticket(t, d, f.T1.ID)
	if tk.StatusID != f.Statuses[1].ID || tk.AssigneeID.Valid {
		t.Fatalf("%+v", tk)
	}
}

// Review fix 3: claiming over a lapsed claim the sweep hasn't reached keeps
// the status the ticket was first taken from.
func TestClaimOverUnsweptLapseKeepsOrigin(t *testing.T) {
	c, d, f := newFixture(t)
	k := newClock(c)
	a := agent(t, c, f.Owner, "first")
	b := agent(t, c, f.Owner, "second")
	c.Claim(bg, a.ID, f.T1.ID, 0)
	k.add(16 * time.Minute)
	if _, err := c.Claim(bg, b.ID, f.T1.ID, 0); err != nil {
		t.Fatal(err)
	}
	k.add(16 * time.Minute)
	c.ExpireClaims(bg)
	if tk := ticket(t, d, f.T1.ID); tk.StatusID != f.T1.StatusID || tk.AssigneeID.Valid {
		t.Fatalf("%+v", tk)
	}
}
