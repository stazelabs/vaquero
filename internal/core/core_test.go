package core_test

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestEnsureOwnerIsIdempotent(t *testing.T) {
	c, d, _ := newCore(t) // newCore already called EnsureOwner once
	before := eventCount(t, d)
	a, err := c.EnsureOwner(context.Background())
	if err != nil || a.Kind != "human" || a.Name != "Owner" {
		t.Fatalf("%+v %v", a, err)
	}
	if eventCount(t, d) != before {
		t.Fatal("second EnsureOwner wrote an event")
	}
}

func TestEnsureOwnerWritesOneEvent(t *testing.T) {
	_, d, _ := newCore(t)
	if n := eventCount(t, d); n != 1 {
		t.Fatalf("events = %d, want 1 (actor.created)", n)
	}
}

func TestInvalidErrorJoinsFields(t *testing.T) {
	e := &core.Invalid{Fields: map[string]string{"title": "Enter a title.", "key": "Use 2 to 10 capital letters or digits."}}
	if !strings.Contains(e.Error(), "Enter a title.") || !strings.Contains(e.Error(), "Use 2 to 10") {
		t.Fatal(e.Error())
	}
}

// coreOp is one core operation with an input it must accept and one it must
// refuse, both run against a fresh fixture.
type coreOp struct {
	name      string
	good, bad func(*core.Core, fixture) error
}

// lateFail replaces the ghost-actor run in the rollback test for operations
// a missing actor does not carry as far as their event write (the ghost
// fails them at their own first write, or at setup). Each arranges real
// rows, calls snap, then runs the operation.
var lateFail = map[string]func(c *core.Core, f fixture, snap func()) error{}

// eventRefused is the message of the trigger that makes the next event
// insert fail, so a rollback test knows the operation got as far as its
// event.
const eventRefused = "event write refused"

// refuseEvents makes every later insert into events fail.
func refuseEvents(t *testing.T, c *core.Core) {
	t.Helper()
	if _, err := c.DB.Exec(`CREATE TRIGGER refuse_events BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, '` + eventRefused + `'); END`); err != nil {
		t.Fatal(err)
	}
}

// ops lists every core operation. Each later task adds its own.
var ops []coreOp

// Review Focus 5: every operation either succeeds and appends exactly one
// event, or fails and leaves every tracker table unchanged.
func TestEveryOperationEmitsOneEventOrNothing(t *testing.T) {
	if len(ops) == 0 {
		t.Skip("no operations registered yet")
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			c, d, f := newFixture(t)
			snap := snapshot(t, d)
			if err := op.bad(c, f); err == nil {
				t.Fatal("bad input accepted")
			}
			if got := snapshot(t, d); !maps.Equal(got, snap) {
				t.Fatalf("bad input changed rows: %v → %v", snap, got)
			}
			// Count appended events by the sequence, not the row count:
			// deleting a project removes its history, then appends one.
			before, _ := d.RQ.MaxEventSeq(bg)
			if err := op.good(c, f); err != nil {
				t.Fatal(err)
			}
			if after, _ := d.RQ.MaxEventSeq(bg); after != before+1 {
				t.Fatalf("events appended: %d", after-before)
			}
		})
	}
}

// A failure after the first write (here: the event's actor doesn't exist, so
// the event insert fails on its foreign key) rolls the whole operation back.
func TestEveryOperationRollsBackPartWay(t *testing.T) {
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			c, d, f := newFixture(t)
			snap := snapshot(t, d)
			if late := lateFail[op.name]; late != nil {
				err := late(c, f, func() { snap = snapshot(t, d); refuseEvents(t, c) })
				if err == nil || !strings.Contains(err.Error(), eventRefused) {
					t.Fatalf("want a failure at the event write, got %v", err)
				}
			} else {
				ghost := f
				ghost.Owner.ID = "no-such-actor"
				if err := op.good(c, ghost); err == nil {
					t.Fatal("succeeded as a missing actor")
				}
			}
			if got := snapshot(t, d); !maps.Equal(got, snap) {
				t.Fatalf("failed operation left changes: %v → %v", snap, got)
			}
		})
	}
}
