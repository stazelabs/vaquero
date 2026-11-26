package handlers_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// oneEvent checks that exactly one event was appended since seq, of kind,
// by actor, and returns the new sequence.
func oneEvent(t *testing.T, e *webtest.Env, seq int64, kind, actor string) int64 {
	t.Helper()
	now, err := e.Deps.R.MaxEventSeq(bg)
	if err != nil {
		t.Fatal(err)
	}
	if now != seq+1 {
		t.Fatalf("%d events appended, want 1", now-seq)
	}
	evs, err := e.Deps.R.ListEventsNewest(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if evs[0].Kind != kind || evs[0].ActorName != actor {
		t.Fatalf("event %s by %s, want %s by %s", evs[0].Kind, evs[0].ActorName, kind, actor)
	}
	return now
}

// noEvent checks nothing was appended since seq.
func noEvent(t *testing.T, e *webtest.Env, seq int64) {
	t.Helper()
	if now, _ := e.Deps.R.MaxEventSeq(bg); now != seq {
		t.Fatalf("%d events appended, want none", now-seq)
	}
}

func statusNames(t *testing.T, c *webtest.Client, tok string) []api.Status {
	t.Helper()
	var ss []api.Status
	decode(t, c.API("GET", "/api/v1/projects/VAQ/statuses", tok, nil).Body, &ss)
	return ss
}

func orderIs(t *testing.T, ss []api.Status, want ...string) {
	t.Helper()
	var got []string
	for _, s := range ss {
		got = append(got, s.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] || ss[i].Position != int64(i+1) {
			t.Fatalf("order %v (%+v), want %v", got, ss, want)
		}
	}
}

// refused checks a 422 whose message is the one field message.
func refused(t *testing.T, r *webtest.Response, field, msg string) {
	t.Helper()
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Fields[field] != msg || e.Error.Message != msg {
		t.Fatalf("%d %s, want 422 %s: %q", r.Status, r.Body, field, msg)
	}
}

func TestAPIStatusesCountTickets(t *testing.T) {
	_, c, tok := apiWorld(t)
	ss := statusNames(t, c, tok)
	counts := map[string]int64{}
	for _, s := range ss {
		counts[s.Name] = s.TicketCount
	}
	if counts["Todo"] != 2 || counts["Done"] != 1 || counts["Backlog"] != 0 {
		t.Fatalf("%+v", ss)
	}
}

func TestAPIStatusCreate(t *testing.T) {
	e, c, tok := apiWorld(t)
	seq := seqNow(t, e)
	r := c.API("POST", "/api/v1/projects/vaq/statuses", tok, map[string]string{"name": "Needs decision", "category": "backlog"})
	var s api.Status
	decode(t, r.Body, &s)
	if r.Status != 201 || s.Name != "Needs decision" || s.Category != "backlog" || s.Position != 2 || s.TicketCount != 0 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.created", "Owner")

	// A neighbour, by name in any case; a category in words.
	r = c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "Blocked", "category": "In progress", "before": "in PROGRESS"})
	if r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.created", "Owner")
	r = c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "Shipped", "category": "done", "after": "Done"})
	if r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.created", "Owner")
	orderIs(t, statusNames(t, c, tok), "Backlog", "Needs decision", "Todo", "Blocked", "In progress", "In review", "Done", "Shipped", "Cancelled")

	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "todo", "category": "todo"}), "name", "A status with that name exists.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "X", "category": "later"}), "category",
		"Use one of backlog, todo, in_progress, in_review, done or cancelled.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "X", "category": "todo", "after": "Nope"}), "after", "No status Nope in VAQ.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "X", "category": "todo", "before": "Todo", "after": "Done"}), "after", "Give before or after, not both.")
	if r := c.API("POST", "/api/v1/projects/NOPE/statuses", tok, map[string]string{"name": "X", "category": "todo"}); r.Status != 404 {
		t.Fatalf("unknown project: %d", r.Status)
	}
	noEvent(t, e, seq)
}

func TestAPIStatusRename(t *testing.T) {
	e, c, tok := apiWorld(t)
	seq := seqNow(t, e)
	r := c.API("PATCH", "/api/v1/projects/VAQ/statuses/in%20review", tok, map[string]string{"name": "Review"})
	var s api.Status
	decode(t, r.Body, &s)
	if r.Status != 200 || s.Name != "Review" || s.Category != "in_review" || s.Position != 4 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.updated", "Owner")
	// The same name: nothing changes, no event.
	if r := c.API("PATCH", "/api/v1/projects/VAQ/statuses/Review", tok, map[string]string{"name": "Review"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	refused(t, c.API("PATCH", "/api/v1/projects/VAQ/statuses/Review", tok, map[string]string{"name": "DONE"}), "name", "A status with that name exists.")
	refused(t, c.API("PATCH", "/api/v1/projects/VAQ/statuses/Nope", tok, map[string]string{"name": "X"}), "status", "No status Nope in VAQ.")
	if r := c.API("PATCH", "/api/v1/projects/VAQ/statuses/Review", tok, map[string]string{"category": "done"}); r.Status != 400 {
		t.Fatalf("a category change: %d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)
}

func TestAPIStatusMove(t *testing.T) {
	e, c, tok := apiWorld(t)
	seq := seqNow(t, e)
	r := c.API("POST", "/api/v1/projects/VAQ/statuses/cancelled/move", tok, map[string]string{"after": "backlog"})
	var s api.Status
	decode(t, r.Body, &s)
	if r.Status != 200 || s.Name != "Cancelled" || s.Position != 2 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.moved", "Owner")
	r = c.API("POST", "/api/v1/projects/VAQ/statuses/Backlog/move", tok, map[string]string{"before": "Cancelled"})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq) // already there
	orderIs(t, statusNames(t, c, tok), "Backlog", "Cancelled", "Todo", "In progress", "In review", "Done")

	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses/Todo/move", tok, map[string]string{}), "before", "Choose a status to move it before or after.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses/Todo/move", tok, map[string]string{"before": "Todo"}), "before", "Choose another status.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses/Todo/move", tok, map[string]string{"after": "Later"}), "after", "No status Later in VAQ.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/statuses/Later/move", tok, map[string]string{"after": "Todo"}), "status", "No status Later in VAQ.")
	noEvent(t, e, seq)
}

func TestAPIStatusDelete(t *testing.T) {
	e, c, tok := apiWorld(t)
	if r := c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "Spare", "category": "todo"}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq := seqNow(t, e)
	refused(t, c.API("DELETE", "/api/v1/projects/VAQ/statuses/Todo", tok, nil), "status", "Move this status's tickets first.")
	refused(t, c.API("DELETE", "/api/v1/projects/VAQ/statuses/Backlog", tok, nil), "status", "A project needs at least one status in each category.")
	refused(t, c.API("DELETE", "/api/v1/projects/VAQ/statuses/Nope", tok, nil), "status", "No status Nope in VAQ.")
	noEvent(t, e, seq)
	if r := c.API("DELETE", "/api/v1/projects/VAQ/statuses/spare", tok, nil); r.Status != 204 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	oneEvent(t, e, seq, "status.deleted", "Owner")
	orderIs(t, statusNames(t, c, tok), "Backlog", "Todo", "In progress", "In review", "Done", "Cancelled")
}

// R6: an agent changes statuses through the owner's token and the
// Vaquero-Agent header, and each change is its own.
func TestAPIStatusChangesByAnAgent(t *testing.T) {
	e, c, tok := apiWorld(t)
	c.Agent = "planner"
	seq := seqNow(t, e) + 1 // the agent's actor is made on first use, with its own event
	if r := c.API("POST", "/api/v1/projects/VAQ/statuses", tok, map[string]string{"name": "Triage", "category": "backlog"}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.created", "planner")
	if r := c.API("POST", "/api/v1/projects/VAQ/statuses/Triage/move", tok, map[string]string{"before": "Backlog"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.moved", "planner")
	if r := c.API("PATCH", "/api/v1/projects/VAQ/statuses/Triage", tok, map[string]string{"name": "Inbox"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "status.updated", "planner")
	if r := c.API("DELETE", "/api/v1/projects/VAQ/statuses/Inbox", tok, nil); r.Status != 204 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	oneEvent(t, e, seq, "status.deleted", "planner")
}
