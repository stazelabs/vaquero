package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func ticket(t *testing.T, e *webtest.Env, p gen.Project, in core.TicketInput) gen.Ticket {
	t.Helper()
	tk, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func lacks(t *testing.T, body string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(body, b) {
			t.Errorf("page has %q", b)
		}
	}
}

func TestCreateTicketFlow(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	contains(t, c.Get("/p/VAQ/new").Body, `name="title"`, `name="tags"`, "Todo", "No priority")
	r := c.Post("/p/VAQ/tickets", url.Values{"title": {"Fix the gate"}, "body": {"Some **bold** text."}, "priority": {"2"}, "tags": {"ui, bug"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Ticket VAQ-1 created.", "Fix the gate", "<strong>bold</strong>", "High", "ui", "bug", "Todo")
}

func TestCreateTicketInvalid(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	r := e.Client().Post("/p/VAQ/tickets", url.Values{"title": {" "}, "body": {"keep me"}, "tags": {"kept-tag"}})
	if r.Status != 422 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "Enter a title.", "keep me", `value="kept-tag"`)
}

func TestSubTicketFromParent(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "Epic"})
	c := e.Client()
	contains(t, c.Get("/p/VAQ/new?parent=VAQ-1").Body, `value="VAQ-1"`)
	r := c.Post("/p/VAQ/tickets", url.Values{"title": {"Child"}, "parent": {"vaq-1"}})
	if r.Status != 303 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	contains(t, c.Follow(r).Body, `href="/t/VAQ-1"`, "Epic")
	contains(t, c.Get("/t/VAQ-1").Body, "Sub-tickets", `href="/t/VAQ-2"`, "Child", `href="/p/VAQ/new?parent=VAQ-1"`)
}

// Review Focus 4
func TestTicketKeyCanonicalRedirect(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	c := e.Client()
	for in, want := range map[string]string{"/t/vaq-1": "/t/VAQ-1", "/t/VAQ-001": "/t/VAQ-1", "/t/vaq-01/edit": "/t/VAQ-1/edit"} {
		if r := c.Get(in); r.Status != 301 || r.Location != want {
			t.Errorf("%s → %d %q", in, r.Status, r.Location)
		}
	}
}

// Review Focus 4
func TestUnknownTicketRedirects(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	for path, msg := range map[string]string{
		"/t/VAQ-99999": "No ticket VAQ-99999.",
		"/t/NOPE-1":    "No ticket NOPE-1.",
		"/t/nonsense":  "That isn&#39;t a ticket key.",
	} {
		r := c.Get(path)
		if r.Status != 303 || r.Location != "/" {
			t.Fatalf("%s → %d %q", path, r.Status, r.Location)
		}
		contains(t, c.Follow(r).Body, msg)
	}
}

func TestParentErrorsShownOnForm(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	one := ticket(t, e, p, core.TicketInput{Title: "One"})
	ticket(t, e, p, core.TicketInput{Title: "Two", ParentID: one.ID})
	c := e.Client()
	for parent, msg := range map[string]string{
		"VAQ-2":   "That ticket is a sub-ticket of this one.",
		"VAQ-50":  "No ticket VAQ-50.",
		"garbage": "That isn&#39;t a ticket key.",
	} {
		r := c.Post("/t/VAQ-1", url.Values{"title": {"One"}, "parent": {parent}})
		if r.Status != 422 {
			t.Fatalf("%s: %d", parent, r.Status)
		}
		contains(t, r.Body, msg, `value="`+parent+`"`)
	}
}

func TestEditTicket(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "Old title", Tags: []string{"ui"}})
	ss := statuses(t, e, p)
	c := e.Client()
	contains(t, c.Get("/t/VAQ-1/edit").Body, `value="Old title"`, `value="ui"`)
	r := c.Post("/t/VAQ-1", url.Values{"title": {"New title"}, "status": {ss[2].ID}, "tags": {"ui"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page := c.Follow(r).Body
	contains(t, page, "Ticket saved.", "New title", "In progress", "changed the title and status of VAQ-1")
	// A status-only change reads as a move.
	r = c.Post("/t/VAQ-1", url.Values{"title": {"New title"}, "status": {ss[4].ID}, "tags": {"ui"}})
	contains(t, c.Follow(r).Body, "moved VAQ-1 from In progress to Done")
}

func TestCommentLifecycle(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	c := e.Client()
	r := c.Post("/t/VAQ-1/comments", url.Values{"body": {"First *thoughts*."}})
	contains(t, c.Follow(r).Body, "Comment added.", "<em>thoughts</em>")
	tk, _ := e.Deps.R.GetTicketByNumber(bg, gen.GetTicketByNumberParams{ProjectID: mustProject(t, e).ID, Number: 1})
	cs, _ := e.Deps.R.ListComments(bg, tk.ID)
	contains(t, c.Get("/t/VAQ-1?edit="+cs[0].ID).Body, "First *thoughts*.</textarea>")
	r = c.Post("/t/VAQ-1/comments/"+cs[0].ID, url.Values{"body": {"Second thoughts."}})
	contains(t, c.Follow(r).Body, "Comment saved.", "Second thoughts.")
	r = c.Post("/t/VAQ-1/comments/"+cs[0].ID+"/delete", nil)
	page := c.Follow(r).Body
	contains(t, page, "Comment deleted.")
	lacks(t, page, "Second thoughts.")
	if r := c.Post("/t/VAQ-1/comments", url.Values{"body": {" "}}); r.Status != 422 || !strings.Contains(r.Body, "Write a comment.") {
		t.Fatalf("%d", r.Status)
	}
}

func mustProject(t *testing.T, e *webtest.Env) gen.Project {
	t.Helper()
	p, err := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLinkLifecycle(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "One"})
	ticket(t, e, p, core.TicketInput{Title: "Two"})
	c := e.Client()
	r := c.Post("/t/VAQ-1/links", url.Values{"kind": {"blocks"}, "to": {"vaq-2"}})
	contains(t, c.Follow(r).Body, "Link added.", "Blocks", `href="/t/VAQ-2"`)
	contains(t, c.Get("/t/VAQ-2").Body, "Blocked by", `href="/t/VAQ-1"`)
	bad := c.Post("/t/VAQ-1/links", url.Values{"kind": {"blocks"}, "to": {"VAQ-77"}})
	if bad.Status != 422 {
		t.Fatal(bad.Status)
	}
	contains(t, bad.Body, "No ticket VAQ-77.", `value="VAQ-77"`)
	one, _ := e.Deps.R.GetTicketByNumber(bg, gen.GetTicketByNumberParams{ProjectID: p.ID, Number: 1})
	ls, _ := e.Deps.R.ListLinksFrom(bg, one.ID)
	r = c.Post("/t/VAQ-1/links/"+ls[0].ID+"/delete", nil)
	page := c.Follow(r).Body
	contains(t, page, "Link removed.")
	lacks(t, page, "Blocked by")
}

// Review Focus 3, through the page.
func TestTicketBodyRenderedSafely(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "x", Body: "<script>alert(1)</script> [x](javascript:alert(2))"})
	page := e.Client().Get("/t/VAQ-1").Body
	lacks(t, page, "<script>alert", `href="javascript:`)
	contains(t, page, "&lt;script&gt;")
}

// Review Focus 1 in the browser: a save from a form opened before someone
// else's change is refused and keeps what was typed.
func TestEditFormConflict(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Original"})
	c := e.Client()
	contains(t, c.Get("/t/VAQ-1/edit").Body, `name="version" value="1"`)
	if r := c.Post("/t/VAQ-1", url.Values{"title": {"Agent's title"}, "version": {"1"}}); r.Status != 303 {
		t.Fatalf("first save: %d", r.Status)
	}
	r := c.Post("/t/VAQ-1", url.Values{"title": {"My title"}, "version": {"1"}})
	if r.Status != 409 {
		t.Fatalf("stale save: %d", r.Status)
	}
	contains(t, r.Body, "VAQ-1 changed since you opened this form.", `value="My title"`, `name="version" value="2"`)
	if r := c.Post("/t/VAQ-1", url.Values{"title": {"My title"}, "version": {"2"}}); r.Status != 303 {
		t.Fatalf("deliberate overwrite: %d", r.Status)
	}
}
