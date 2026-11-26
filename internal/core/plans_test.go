package core_test

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops,
		coreOp{"SavePlan",
			func(c *core.Core, f fixture) error {
				_, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Do the thing.", 0)
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "   ", 0)
				return err
			}},
	)
}

// A revision whose event fails leaves the plan and its revisions as they
// were (the ops table covers creating).
func TestSavePlanReviseRollsBack(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "First.", 0); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	refuseEvents(t, c)
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Second.", 1); err == nil || !strings.Contains(err.Error(), eventRefused) {
		t.Fatalf("want a failure at the event write, got %v", err)
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a failed revision left changes")
	}
}

func TestSavePlanCreatesAndRevises(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	p, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Step one.\n\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != 1 || p.Body != "Step one." || p.TicketID != f.T1.ID || p.UpdatedBy != f.Owner.ID {
		t.Fatalf("%+v", p)
	}
	kind, payload := lastEvent(t, d)
	if kind != core.EvPlanCreated || len(payload) != 1 || payload["revision"] != float64(1) {
		t.Fatalf("%s %v", kind, payload)
	}
	// NFC: e + combining acute is stored as é.
	p, err = c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Step one.\nStep two, café.", 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != 2 || p.Body != "Step one.\nStep two, café." {
		t.Fatalf("%+v", p)
	}
	kind, payload = lastEvent(t, d)
	if kind != core.EvPlanRevised || len(payload) != 1 || payload["revision"] != float64(2) {
		t.Fatalf("%s %v", kind, payload)
	}
	if n := eventCount(t, d); n != before+2 {
		t.Fatalf("%d events, want 2", n-before)
	}
	// Every revision is kept, the first with its own text.
	if n := count(t, d, `SELECT count(*) FROM plan_revisions WHERE plan_id = ?`, p.ID); n != 2 {
		t.Fatalf("%d revisions", n)
	}
	var old string
	if err := d.R.QueryRow(`SELECT body FROM plan_revisions WHERE plan_id = ? AND revision = 1`, p.ID).Scan(&old); err != nil || old != "Step one." {
		t.Fatalf("%q %v", old, err)
	}
	// The ticket itself is not changed by its plan.
	tk, _ := d.RQ.GetTicket(bg, f.T1.ID)
	if tk.Version != f.T1.Version {
		t.Fatalf("ticket version %d, want %d", tk.Version, f.T1.Version)
	}
}

func TestSavePlanStaleBaseConflicts(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "One.", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Two, from agent A.", 1); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	_, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Two, from agent B.", 1)
	var pc *core.PlanConflict
	if !errors.As(err, &pc) {
		t.Fatalf("err = %v, want *core.PlanConflict", err)
	}
	if pc.Current.Revision != 2 || pc.Current.Body != "Two, from agent A." || !strings.Contains(pc.Error(), "VAQ-1") {
		t.Fatalf("%+v %q", pc.Current, pc.Error())
	}
	// Creating a plan that already exists is stale too.
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Fresh.", 0); !errors.As(err, &pc) {
		t.Fatalf("create over a plan: %v", err)
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a stale save wrote")
	}
	// A revision for a ticket with no plan is refused.
	_, err = c.SavePlan(bg, f.Owner.ID, f.T2.ID, "Plan.", 3)
	fieldErr(t, err, "revision", "")
}

func TestSavePlanUnchangedWritesNothing(t *testing.T) {
	c, d, f := newFixture(t)
	p, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Same.", 0)
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	got, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Same.\n", 1)
	if err != nil || got.Revision != 1 || got.ID != p.ID {
		t.Fatalf("%+v %v", got, err)
	}
	if now := snapshot(t, d); !maps.Equal(now, snap) {
		t.Fatal("an unchanged save wrote")
	}
}

func TestSavePlanRefusals(t *testing.T) {
	c, _, f := newFixture(t)
	_, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, " \n", 0)
	fieldErr(t, err, "body", "Write the plan.")
	_, err = c.SavePlan(bg, f.Owner.ID, f.T1.ID, strings.Repeat("é", 200001), 0)
	fieldErr(t, err, "body", "Keep the plan under 200,000 characters.")
	_, err = c.SavePlan(bg, f.Owner.ID, f.T1.ID, "a\x00b", 0)
	fieldErr(t, err, "body", "Remove the hidden control characters.")
	_, err = c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Plan.", -1)
	fieldErr(t, err, "revision", "")
	if _, err := c.SavePlan(bg, f.Owner.ID, "nope", "Plan.", 0); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, strings.Repeat("é", 200000), 0); err != nil {
		t.Fatalf("200,000 characters: %v", err)
	}
}

// A project delete takes its tickets' plans and every revision with it.
func TestDeleteProjectRemovesPlans(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "One.", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SavePlan(bg, f.Owner.ID, f.T1.ID, "Two.", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SavePlan(bg, f.Owner.ID, f.O1.ID, "Elsewhere.", 0); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "VAQ"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM plans`); n != 1 {
		t.Fatalf("plans left: %d, want the other project's one", n)
	}
	if n := count(t, d, `SELECT count(*) FROM plan_revisions`); n != 1 {
		t.Fatalf("revisions left: %d", n)
	}
}
