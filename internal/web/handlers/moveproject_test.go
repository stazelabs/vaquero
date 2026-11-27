package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// moveEnv is agentEnv (VAQ-1) with OTH-1 in a second project.
func moveEnv(t *testing.T) (*webtest.Env, string) {
	t.Helper()
	e, tok := agentEnv(t)
	oth, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, oth.ID, core.TicketInput{Title: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}
	return e, tok
}

type movedTicket struct {
	Key, Status string
	Version     int64
	FormerKeys  []string `json:"former_keys"`
}

// Review Focus 1: POST /api/v1/tickets/{ticket}/move-to moves the ticket,
// and its old key answers for the new one everywhere in the API.
func TestAPIMoveTicketToProject(t *testing.T) {
	e, tok := moveEnv(t)
	c := e.Client()
	r := c.API("POST", "/api/v1/tickets/VAQ-1/move-to", tok, map[string]any{"project": "oth", "version": 1})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var got movedTicket
	decode(t, r.Body, &got)
	if got.Key != "OTH-2" || got.Status != "Todo" || got.Version != 2 || len(got.FormerKeys) != 1 || got.FormerKeys[0] != "VAQ-1" {
		t.Fatalf("%+v", got)
	}
	// The old key reads, writes and comments on the moved ticket, and every
	// answer names it by its new key.
	decode(t, c.API("GET", "/api/v1/tickets/vaq-1", tok, nil).Body, &got)
	if got.Key != "OTH-2" {
		t.Fatalf("GET by old key: %+v", got)
	}
	r = c.API("PATCH", "/api/v1/tickets/VAQ-1?view=summary", tok, map[string]any{"status": "Done"})
	if r.Status != 200 || !strings.Contains(r.Body, `"key": "OTH-2"`) {
		t.Fatalf("PATCH by old key: %d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]any{"body": "Still here."}); r.Status != 201 {
		t.Fatalf("comment by old key: %d %s", r.Status, r.Body)
	}
	// The activity names both projects in one event.
	r = c.API("GET", "/api/v1/events?newest=20", tok, nil)
	if !strings.Contains(r.Body, `"kind": "ticket.moved"`) || !strings.Contains(r.Body, "moved VAQ-1 to OTH as OTH-2") {
		t.Fatalf("events: %s", r.Body)
	}
	// Events from before the move name the ticket by its key now, never
	// VAQ-2 (another ticket's key, or none).
	if strings.Contains(r.Body, "created VAQ-2") || !strings.Contains(r.Body, "created OTH-2 First") {
		t.Fatalf("old events' keys: %s", r.Body)
	}
}

func TestAPIMoveTicketToProjectRefusals(t *testing.T) {
	e, tok := moveEnv(t)
	c := e.Client()
	p := mustProject(t, e)
	tk, _ := e.Deps.R.GetTicketByNumber(bg, ticketNumber(p.ID, 1))
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "Child", ParentID: tk.ID}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		body       map[string]any
		status     int
		want       string
	}{
		{"no project", "/api/v1/tickets/VAQ-1/move-to", map[string]any{"project": "NOPE"}, 422, `"project": "No project NOPE."`},
		{"same project", "/api/v1/tickets/VAQ-1/move-to", map[string]any{"project": "VAQ"}, 422, "VAQ-1 is already in VAQ."},
		{"sub-ticket", "/api/v1/tickets/VAQ-2/move-to", map[string]any{"project": "OTH"}, 422, "Move its parent, or detach it first."},
		{"stale", "/api/v1/tickets/VAQ-1/move-to", map[string]any{"project": "OTH", "version": 9}, 409, "changed since you read it"},
		{"no ticket", "/api/v1/tickets/VAQ-9/move-to", map[string]any{"project": "OTH"}, 404, "No ticket VAQ-9."},
		{"bad view", "/api/v1/tickets/VAQ-1/move-to?view=tiny", map[string]any{"project": "OTH"}, 422, "No view tiny."},
	} {
		r := c.API("POST", tc.path, tok, tc.body)
		if r.Status != tc.status || !strings.Contains(r.Body, tc.want) {
			t.Errorf("%s: %d %s", tc.name, r.Status, r.Body)
		}
	}
	// Moving the parent takes the child along.
	r := c.API("POST", "/api/v1/tickets/VAQ-1/move-to?view=summary", tok, map[string]any{"project": "OTH"})
	if r.Status != 200 || !strings.Contains(r.Body, `"key": "OTH-2"`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var child movedTicket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &child)
	if child.Key != "OTH-3" {
		t.Fatalf("child %+v", child)
	}
}

// Bulk: an item's project moves the ticket first, and the item's update
// then names the new project's statuses; results carry the new key.
func TestAPIBulkMovesToProject(t *testing.T) {
	e, tok := moveEnv(t)
	c := e.Client()
	out := bulk(t, c, tok,
		map[string]any{"key": "VAQ-1", "version": 1, "project": "OTH", "update": map[string]any{"status": "In progress"}},
		map[string]any{"key": "vaq-1", "comment": "By the old key."},
		map[string]any{"key": "OTH-1", "project": "NOPE"},
	)
	got := out.Items
	if !got[0].OK || got[0].Key != "OTH-2" || got[0].Status != "In progress" || got[0].Version != 3 {
		t.Errorf("item 1: %+v %+v", got[0], got[0].Error)
	}
	if !got[1].OK || got[1].Key != "OTH-2" {
		t.Errorf("item 2: %+v", got[1])
	}
	if got[2].OK || got[2].Error == nil || got[2].Error.Fields["project"] != "No project NOPE." {
		t.Errorf("item 3: %+v %+v", got[2], got[2].Error)
	}
}

// The web: an old key's page redirects to the new key (302: an old key
// may lead elsewhere later); the ticket page's move form posts behind the
// confirm dialog; a sub-ticket's page says why it can't move.
func TestWebMoveTicketToProject(t *testing.T) {
	e, _ := moveEnv(t)
	c := e.Client()
	page := c.Get("/t/VAQ-1")
	if !strings.Contains(page.Body, `action="/t/VAQ-1/move-to"`) || !strings.Contains(page.Body, `data-confirm="Move VAQ-1`) ||
		!strings.Contains(page.Body, `<option value="OTH">`) {
		t.Fatalf("no move form:\n%s", page.Body)
	}
	r := c.Post("/t/VAQ-1/move-to", url.Values{"project": {"OTH"}})
	if r.Status != 303 || r.Location != "/t/OTH-2" {
		t.Fatalf("%d %s", r.Status, r.Location)
	}
	if body := c.Follow(r).Body; !strings.Contains(body, "Moved to OTH-2.") {
		t.Fatal("no toast")
	}
	for _, raw := range []string{"/t/VAQ-1", "/t/vaq-01"} {
		r := c.Get(raw)
		if r.Status != 302 || r.Location != "/t/OTH-2" {
			t.Errorf("%s: %d %s", raw, r.Status, r.Location)
		}
	}
	if r := c.Get("/t/VAQ-1/edit"); r.Status != 302 || r.Location != "/t/OTH-2/edit" {
		t.Errorf("edit: %d %s", r.Status, r.Location)
	}
	// A sub-ticket's page has no move form; a post is refused with a toast.
	oth, _ := e.Deps.R.GetProjectByKey(bg, "OTH")
	parent, _ := e.Deps.R.GetTicketByNumber(bg, ticketNumber(oth.ID, 2))
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, oth.ID, core.TicketInput{Title: "Child", ParentID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	page = c.Get("/t/OTH-3")
	if strings.Contains(page.Body, `action="/t/OTH-3/move-to"`) || !strings.Contains(page.Body, "Move its parent, or detach it first.") {
		t.Fatalf("sub-ticket page:\n%s", page.Body)
	}
	r = c.Post("/t/OTH-3/move-to", url.Values{"project": {"VAQ"}})
	if r.Status != 303 || r.Location != "/t/OTH-3" || !strings.Contains(c.Follow(r).Body, "Move its parent, or detach it first.") {
		t.Fatalf("%d %s", r.Status, r.Location)
	}
	// The history says what happened, naming both projects.
	if body := c.Get("/t/OTH-2").Body; !strings.Contains(body, "moved VAQ-1 to OTH as OTH-2") {
		t.Fatal("history lacks the move")
	}
}

func ticketNumber(projectID string, n int64) gen.GetTicketByNumberParams {
	return gen.GetTicketByNumberParams{ProjectID: projectID, Number: n}
}

// MCP: bulk_update's project moves a ticket, and every tool takes the old
// key, answering for the new one.
func TestMCPMoveToProject(t *testing.T) {
	e, tok := moveEnv(t)
	cs := mcpConnect(t, e, tok, "")
	res, text := mcpCall(t, cs, "bulk_update", map[string]any{"items": []any{map[string]any{"key": "VAQ-1", "project": "OTH"}}})
	if res.IsError || !strings.Contains(text, `"key": "OTH-2"`) || !strings.Contains(text, `"ok": true`) {
		t.Fatalf("bulk_update: %s", text)
	}
	res, text = mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	if res.IsError || !strings.Contains(text, `"key": "OTH-2"`) || !strings.Contains(text, `"VAQ-1"`) {
		t.Fatalf("get_ticket by old key: %s", text)
	}
	res, text = mcpCall(t, cs, "create_comment", map[string]any{"key": "vaq-1", "body": "Moved.", "view": "summary"})
	if res.IsError || !strings.Contains(text, `"key": "OTH-2"`) {
		t.Fatalf("create_comment by old key: %s", text)
	}
}
