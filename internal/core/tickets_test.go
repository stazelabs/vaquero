package core_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestTicketNumbersIncrease(t *testing.T) {
	c, _, f := newFixture(t)
	tk, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third"})
	if err != nil || f.T1.Number != 1 || f.T2.Number != 2 || tk.Number != 3 {
		t.Fatalf("%d %d %d %v", f.T1.Number, f.T2.Number, tk.Number, err)
	}
}

func TestNewTicketDefaultsToFirstTodoStatus(t *testing.T) {
	_, d, f := newFixture(t)
	s, err := d.RQ.GetStatus(bg, f.T1.StatusID)
	if err != nil || s.Name != "Todo" {
		t.Fatalf("%+v %v", s, err)
	}
}

// Review Focus 1
func TestConcurrentCreatesGetDistinctNumbers(t *testing.T) {
	c, _, f := newFixture(t)
	var wg sync.WaitGroup
	nums := make(chan int64, 20)
	for range 20 {
		wg.Go(func() {
			tk, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "t"})
			if err != nil {
				t.Error(err)
				return
			}
			nums <- tk.Number
		})
	}
	wg.Wait()
	close(nums)
	seen := map[int64]bool{}
	for n := range nums {
		if seen[n] {
			t.Fatalf("duplicate number %d", n)
		}
		seen[n] = true
	}
	if len(seen) != 20 {
		t.Fatalf("%d tickets", len(seen))
	}
}

// Review Focus 2
func TestParentRules(t *testing.T) {
	c, d, f := newFixture(t)
	// a → b → c: c's parent is b, b's parent is a.
	a := f.T1
	b, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "b", ParentID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "c", ParentID: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	for parent, want := range map[string]string{
		cc.ID:   "That ticket is a sub-ticket of this one.",
		b.ID:    "That ticket is a sub-ticket of this one.",
		a.ID:    "A ticket can't be its own parent.",
		f.O1.ID: "The parent must be in the same project.",
	} {
		in := input(a, "bug")
		in.ParentID = parent
		_, err := c.UpdateTicket(bg, f.Owner.ID, a.ID, in)
		fieldErr(t, err, "parent", want)
	}
	got, _ := d.RQ.GetTicket(bg, a.ID)
	if got.ParentID.Valid {
		t.Fatal("parent changed")
	}
	if eventCount(t, d) != before {
		t.Fatal("refused updates wrote events")
	}
	_, err = c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "x", ParentID: f.O1.ID})
	fieldErr(t, err, "parent", "The parent must be in the same project.")
}

func TestTicketFieldValidation(t *testing.T) {
	c, _, f := newFixture(t)
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	cases := []struct {
		in         core.TicketInput
		field, msg string
	}{
		{core.TicketInput{Title: ""}, "title", "Enter a title."},
		{core.TicketInput{Title: string(long)}, "title", "Keep the title under 200 characters."},
		{core.TicketInput{Title: "x", StatusID: f.Statuses[0].ID + "x"}, "status", "Choose a status from this project."},
		{core.TicketInput{Title: "x", Priority: 5}, "priority", "Choose a priority."},
		{core.TicketInput{Title: "x", MilestoneID: "nope"}, "milestone", "Choose a milestone from this project."},
		{core.TicketInput{Title: "x", AssigneeID: "nope"}, "assignee", "Choose someone from the list."},
		{core.TicketInput{Title: "x", Tags: []string{string(long[:41])}}, "tags", "Keep each tag under 40 characters."},
	}
	for _, tc := range cases {
		_, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, tc.in)
		fieldErr(t, err, tc.field, tc.msg)
	}
	// A status from another project is refused too.
	_, err := c.CreateTicket(bg, f.Owner.ID, f.Other.ID, core.TicketInput{Title: "x", StatusID: f.Statuses[0].ID})
	fieldErr(t, err, "status", "Choose a status from this project.")
}

func TestTitleLengthCountsCharacters(t *testing.T) {
	c, _, f := newFixture(t)
	title := ""
	for range 200 {
		title += "é"
	}
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: title}); err != nil {
		t.Fatalf("200 two-byte characters refused: %v", err)
	}
}

func lastPayload(t *testing.T, c *core.Core) map[string]any {
	t.Helper()
	var raw string
	if err := c.DB.QueryRow(`SELECT payload FROM events ORDER BY seq DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUpdateTicketPayloadListsChanges(t *testing.T) {
	c, _, f := newFixture(t)
	in := input(f.T1, "ui", "bug", "UI")
	in.Title = "Renamed"
	if _, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(lastPayload(t, c))
	want := `{"changes":{"tags":{"from":["bug"],"to":["bug","ui"]},"title":{"from":"First","to":"Renamed"}}}`
	if string(b) != want {
		t.Fatalf("payload\n got %s\nwant %s", b, want)
	}
}

func TestUpdateWithNoChangeWritesNoEvent(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	if _, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, input(f.T1, "BUG")); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("no-op update wrote an event")
	}
}

func TestStatusChangeMovesToEndOfColumn(t *testing.T) {
	c, _, f := newFixture(t)
	// Move T2 to Backlog, then T1 too: T1 must land after T2.
	in2 := input(f.T2)
	in2.StatusID = f.Statuses[0].ID
	t2, err := c.UpdateTicket(bg, f.Owner.ID, f.T2.ID, in2)
	if err != nil {
		t.Fatal(err)
	}
	in1 := input(f.T1, "bug")
	in1.StatusID = f.Statuses[0].ID
	t1, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in1)
	if err != nil {
		t.Fatal(err)
	}
	if t1.Position <= t2.Position {
		t.Fatalf("T1 at %v, T2 at %v", t1.Position, t2.Position)
	}
	// An update that keeps the status keeps the position.
	in1.Title = "Renamed"
	t1b, err := c.UpdateTicket(bg, f.Owner.ID, f.T1.ID, in1)
	if err != nil || t1b.Position != t1.Position {
		t.Fatalf("%v → %v %v", t1.Position, t1b.Position, err)
	}
}

func TestResolveTicket(t *testing.T) {
	c, d, f := newFixture(t)
	tk, p, err := c.ResolveTicket(bg, d.RQ, core.TicketKey{Project: "VAQ", Number: 1})
	if err != nil || tk.ID != f.T1.ID || p.ID != f.Project.ID {
		t.Fatalf("%v", err)
	}
	for _, k := range []core.TicketKey{{Project: "VAQ", Number: 99}, {Project: "NOPE", Number: 1}} {
		if _, _, err := c.ResolveTicket(bg, d.RQ, k); !errors.Is(err, core.ErrNotFound) {
			t.Errorf("%v: %v", k, err)
		}
	}
}

func TestTagsAreNormalised(t *testing.T) {
	c, d, f := newFixture(t)
	tk, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "x", Tags: []string{" ui ", "", "UI", "Bug"}})
	if err != nil {
		t.Fatal(err)
	}
	tags, _ := d.RQ.ListTicketTags(bg, tk.ID)
	if len(tags) != 2 || tags[0].Name != "bug" || tags[1].Name != "ui" {
		t.Fatalf("%+v", tags)
	}
}

// Tags whose case folds differently in SQLite (ASCII only) and Go (Unicode)
// still compare equal on a no-op save.
func TestNoOpUpdateWithAccentedTags(t *testing.T) {
	c, d, f := newFixture(t)
	tags := []string{"Érable", "école", "Zeta", "alpha"}
	in := input(f.T2, tags...)
	tk, err := c.UpdateTicket(bg, f.Owner.ID, f.T2.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	if _, err := c.UpdateTicket(bg, f.Owner.ID, f.T2.ID, input(tk, tags...)); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatalf("no-op save wrote an event: %v", lastPayload(t, c))
	}
}

// A NUL or other control character is refused plainly, not a 500 from the
// database's CHECK constraints (SQLite's length() stops at NUL).
func TestControlCharactersRefused(t *testing.T) {
	c, _, f := newFixture(t)
	const msg = "Remove the hidden control characters."
	_, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "\x00"})
	fieldErr(t, err, "title", msg)
	_, err = c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "ok", Body: "a\x00b"})
	fieldErr(t, err, "body", msg)
	_, err = c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "ok", Tags: []string{"x\x07"}})
	fieldErr(t, err, "tags", msg)
	_, err = c.AddComment(bg, f.Owner.ID, f.T1.ID, "hi\x00")
	fieldErr(t, err, "body", msg)
	_, err = c.CreateProject(bg, f.Owner.ID, core.ProjectInput{Key: "CTL", Name: "a\x1bb"})
	fieldErr(t, err, "name", msg)
	// Tabs and newlines are ordinary text.
	if _, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "ok", Body: "line\n\tindented\r\n"}); err != nil {
		t.Fatal(err)
	}
}
