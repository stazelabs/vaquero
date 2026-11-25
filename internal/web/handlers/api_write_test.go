package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

type apiErrBody struct {
	Error struct {
		Code, Message string
		Fields        map[string]string
		Current       *api.Ticket
	}
}

func TestAPICreateProjectAndTicket(t *testing.T) {
	_, c, tok := apiWorld(t)
	r := c.API("POST", "/api/v1/projects", tok, map[string]string{"key": "ops", "name": "Operations"})
	if r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{
		"title": "From the API", "status": "in progress", "priority": 1, "assignee": "owner",
		"parent": "vaq-1", "tags": []string{"api"},
	})
	if r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var tk api.Ticket
	decode(t, r.Body, &tk)
	if tk.Key != "VAQ-4" || tk.Status != "In progress" || tk.Assignee != "Owner" || tk.Parent != "VAQ-1" || len(tk.Tags) != 1 {
		t.Fatalf("%+v", tk)
	}
	r = c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{"title": "x", "status": "Nope", "milestone": "Never"})
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Fields["status"] == "" || e.Error.Fields["milestone"] == "" {
		t.Fatalf("%d %+v", r.Status, e)
	}
}

func TestAPIPatchPartial(t *testing.T) {
	_, c, tok := apiWorld(t)
	r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"status": "In review", "add_tags": []string{"api"}})
	var tk api.Ticket
	decode(t, r.Body, &tk)
	if r.Status != 200 || tk.Status != "In review" || tk.Title != "Board work" || len(tk.Tags) != 2 || tk.Priority != 2 {
		t.Fatalf("%d %+v", r.Status, tk)
	}
	r = c.API("PATCH", "/api/v1/tickets/VAQ-2", tok, map[string]any{"parent": ""})
	decode(t, r.Body, &tk)
	if tk.Parent != "" {
		t.Fatalf("parent not cleared: %+v", tk)
	}
}

// Review Focus 1 at the API.
func TestAPIPatchConflict(t *testing.T) {
	_, c, tok := apiWorld(t)
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	v := tk.Version
	if r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"title": "First writer", "version": v}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"title": "Second writer", "version": v})
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 409 || e.Error.Code != "conflict" || e.Error.Current == nil || e.Error.Current.Title != "First writer" || e.Error.Current.Version != v+1 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

func TestAPIUnknownFieldRefused(t *testing.T) {
	_, c, tok := apiWorld(t)
	r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"titel": "x"})
	if r.Status != 400 || !strings.Contains(r.Body, "titel") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

func TestAPIMoveByName(t *testing.T) {
	_, c, tok := apiWorld(t)
	r := c.API("POST", "/api/v1/tickets/VAQ-2/move", tok, map[string]any{"status": "in progress"})
	var tk api.Ticket
	decode(t, r.Body, &tk)
	if r.Status != 200 || tk.Status != "In progress" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-2/move", tok, map[string]any{"status": "Nowhere"}); r.Status != 422 {
		t.Fatalf("%d", r.Status)
	}
}

func TestAPICommentsAndLinks(t *testing.T) {
	_, c, tok := apiWorld(t)
	r := c.API("POST", "/api/v1/tickets/VAQ-2/comments", tok, map[string]string{"body": "On it."})
	var cm api.Comment
	decode(t, r.Body, &cm)
	if r.Status != 201 || cm.ID == "" || cm.Author != "Owner" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("PATCH", "/api/v1/comments/"+cm.ID, tok, map[string]string{"body": "Done."}); r.Status != 200 {
		t.Fatalf("edit %d", r.Status)
	}
	if r := c.API("DELETE", "/api/v1/comments/"+cm.ID, tok, nil); r.Status != 204 {
		t.Fatalf("delete %d", r.Status)
	}
	// VAQ-1 blocks VAQ-2 already: the reverse is a loop.
	r = c.API("POST", "/api/v1/tickets/VAQ-2/links", tok, map[string]string{"kind": "blocks", "to": "VAQ-1"})
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 422 || !strings.Contains(e.Error.Fields["to"], "already blocks this ticket") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.API("POST", "/api/v1/tickets/VAQ-2/links", tok, map[string]string{"kind": "relates", "to": "VAQ-3"})
	var l api.Link
	decode(t, r.Body, &l)
	if r.Status != 201 || l.Key != "VAQ-3" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("DELETE", "/api/v1/links/"+l.ID, tok, nil); r.Status != 204 {
		t.Fatalf("unlink %d", r.Status)
	}
}

func TestAPIWritesRecordTheTokenActor(t *testing.T) {
	e, c, tok := apiWorld(t)
	c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]string{"body": "hi"})
	evs, _ := e.Deps.R.ListEvents(bg, 1)
	if evs[0].ActorName != "Owner" || evs[0].Kind != "comment.created" {
		t.Fatalf("%+v", evs[0])
	}
}

func TestAPIProjectEdits(t *testing.T) {
	_, c, tok := apiWorld(t)
	if r := c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]string{"name": "Renamed"}); r.Status != 200 || !strings.Contains(r.Body, "Renamed") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/projects/VAQ/key", tok, map[string]string{"key": "VQ"}); r.Status != 200 || !strings.Contains(r.Body, `"key": "VQ"`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// VAQ-100: one unmatched name is named in the top-level message; several
// keep the generic sentence, with the fields alongside.
func TestAPINameRefusalMessage(t *testing.T) {
	_, c, tok := apiWorld(t)
	var e apiErrBody
	r := c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{"title": "x", "milestone": "Never"})
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Message != "No milestone Never in VAQ." || e.Error.Fields["milestone"] != e.Error.Message {
		t.Fatalf("%d %+v", r.Status, e)
	}
	r = c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{"title": "x", "status": "Nope", "milestone": "Never"})
	e = apiErrBody{}
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Message != "Some names don't match anything." || len(e.Error.Fields) != 2 {
		t.Fatalf("%d %+v", r.Status, e)
	}
}
