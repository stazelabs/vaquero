package handlers_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func statusID(t *testing.T, e *webtest.Env, name string) string {
	t.Helper()
	for _, s := range statuses(t, e, mustProject(t, e)) {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no status %s", name)
	return ""
}

func TestBoardShowsActiveColumnsAndRails(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	page := e.Client().Get("/p/VAQ").Body
	contains(t, page, `id="board"`, "<details", "Backlog", "Cancelled", "Todo", "In progress", "In review", "Done",
		`href="/p/VAQ/list"`, `href="/p/VAQ/settings"`, `href="/p/VAQ/new"`)
	if n := strings.Count(page, "data-board-column"); n != 6 {
		t.Fatalf("%d columns", n)
	}
}

func TestReadyCardMarked(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "ready"})
	ticket(t, e, p, core.TicketInput{Title: "taken", AssigneeID: e.Deps.Actor.ID})
	page := e.Client().Get("/p/VAQ").Body
	if n := strings.Count(page, "data-ready"); n != 1 {
		t.Fatalf("%d ready cards", n)
	}
	contains(t, page, `data-card="VAQ-1" data-ready`)
}

func TestFilterChipsInURL(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "tagged", Tags: []string{"ui"}})
	ticket(t, e, p, core.TicketInput{Title: "plain"})
	c := e.Client()
	all := c.Get("/p/VAQ").Body
	contains(t, all, `href="/p/VAQ?tag=ui"`, `href="/p/VAQ?mine=1"`, `href="/p/VAQ?agents=1"`, `href="/p/VAQ?priority=1"`)
	page := c.Get("/p/VAQ?tag=ui").Body
	contains(t, page, `data-card="VAQ-1"`, `aria-current="true"`, "Clear filters")
	lacks(t, page, `data-card="VAQ-2"`)
	// The fragment keeps the filter for live refreshes and drops.
	contains(t, page, `data-filter="tag=ui"`)
	// Odd filter input renders, empty.
	for _, q := range []string{"priority=9", "tag=no-such-tag", "milestone=garbage", "tag=" + strings.Repeat("x", 500)} {
		if r := c.Get("/p/VAQ?" + q); r.Status != 200 {
			t.Errorf("%s: %d", q, r.Status)
		}
	}
}

func TestMilestonePanel(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	m, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Beta", DueOn: "2026-12-15"})
	if err != nil {
		t.Fatal(err)
	}
	done := statusID(t, e, "Done")
	ticket(t, e, p, core.TicketInput{Title: "a", MilestoneID: m.ID, StatusID: done})
	ticket(t, e, p, core.TicketInput{Title: "b", MilestoneID: m.ID, StatusID: done})
	ticket(t, e, p, core.TicketInput{Title: "c", MilestoneID: m.ID})
	ticket(t, e, p, core.TicketInput{Title: "loose"})
	page := e.Client().Get("/p/VAQ").Body
	contains(t, page, "Beta", "Dec 15, 2026", "2 of 3 done", `<progress`, `href="/p/VAQ?milestone=`+m.ID+`"`,
		"No milestone", "1 open", `href="/p/VAQ?milestone=none"`)
}

func TestMoveEndpoint(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "one"})
	ticket(t, e, p, core.TicketInput{Title: "two"})
	ip := statusID(t, e, "In progress")
	c := e.Client()
	r := c.PostHX("/t/VAQ-1/move", url.Values{"status": {ip}, "before": {""}, "after": {""}, "filter": {""}})
	if r.Status != 200 || !strings.HasPrefix(strings.TrimSpace(r.Body), `<div id="board"`) {
		t.Fatalf("%d %.80q", r.Status, r.Body)
	}
	if tk, _, _ := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: 1}); tk.StatusID != ip {
		t.Fatal("not moved")
	}
	// Reorder by key: two above one.
	r = c.PostHX("/t/VAQ-2/move", url.Values{"status": {ip}, "before": {""}, "after": {"vaq-1"}})
	if i, j := strings.Index(r.Body, `data-card="VAQ-2"`), strings.Index(r.Body, `data-card="VAQ-1"`); i < 0 || i > j {
		t.Fatalf("order: %d %d", i, j)
	}
	// Without htmx: back to the board.
	r = c.Post("/t/VAQ-1/move", url.Values{"status": {statusID(t, e, "Todo")}})
	if r.Status != 303 || r.Location != "/p/VAQ" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
}

// Review Focus 2: a drop from a stale page.
func TestMoveToDeletedStatus(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "one"})
	c := e.Client()
	r := c.PostHX("/t/VAQ-1/move", url.Values{"status": {"gone"}})
	if r.Status != 200 || r.Header.Get("HX-Refresh") != "true" {
		t.Fatalf("%d %v", r.Status, r.Header)
	}
	contains(t, c.Get("/p/VAQ").Body, "Choose a status from this project.")
}

func TestMoveUnknownTicket(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	for _, key := range []string{"VAQ-99", "nonsense"} {
		r := c.PostHX("/t/"+key+"/move", url.Values{"status": {statusID(t, e, "Todo")}})
		if r.Status != 200 || r.Header.Get("HX-Refresh") != "true" {
			t.Fatalf("%s: %d", key, r.Status)
		}
	}
	contains(t, c.Get("/").Body, "That isn&#39;t a ticket key.")
}

func TestListViewShowsEverything(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "in backlog", StatusID: statusID(t, e, "Backlog")})
	ticket(t, e, p, core.TicketInput{Title: "cancelled one", StatusID: statusID(t, e, "Cancelled")})
	now := e.Deps.Core.Now
	e.Deps.Core.Now = func() time.Time { return webtest.Start.Add(-10 * 24 * time.Hour) }
	ticket(t, e, p, core.TicketInput{Title: "long done", StatusID: statusID(t, e, "Done")})
	e.Deps.Core.Now = now
	c := e.Client()
	boardPage := c.Get("/p/VAQ").Body
	contains(t, boardPage, "1 older", `href="/p/VAQ/list"`)
	lacks(t, boardPage, "long done")
	list := c.Get("/p/VAQ/list").Body
	contains(t, list, "in backlog", "cancelled one", "long done", `href="/p/VAQ"`)
	contains(t, c.Get("/p/VAQ/list?tag=nothing").Body, "No tickets match.")
}

// "N older" leads to the list's done tickets, and the list has a chip per
// status category (VAQ-39).
func TestOlderLinksToDoneList(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "in backlog", StatusID: statusID(t, e, "Backlog")})
	now := e.Deps.Core.Now
	e.Deps.Core.Now = func() time.Time { return webtest.Start.Add(-10 * 24 * time.Hour) }
	ticket(t, e, p, core.TicketInput{Title: "long done", StatusID: statusID(t, e, "Done"), Tags: []string{"ui"}})
	e.Deps.Core.Now = now
	c := e.Client()
	contains(t, c.Get("/p/VAQ?tag=ui").Body, "1 older", `href="/p/VAQ/list?category=done&amp;tag=ui"`)
	list := c.Get("/p/VAQ/list?category=done").Body
	contains(t, list, "long done", `href="/p/VAQ/list"`, "Clear filters")
	lacks(t, list, "in backlog")
	all := c.Get("/p/VAQ/list").Body
	contains(t, all, `href="/p/VAQ/list?category=done"`, `href="/p/VAQ/list?category=backlog"`, "In review")
	lacks(t, c.Get("/p/VAQ").Body, `href="/p/VAQ?category=done"`)
}

func TestBoardFragmentRoute(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	r := e.Client().Get("/p/VAQ/board?mine=1")
	if r.Status != 200 || !strings.HasPrefix(strings.TrimSpace(r.Body), `<div id="board"`) || strings.Contains(r.Body, "<html") {
		t.Fatalf("%d %.80q", r.Status, r.Body)
	}
	contains(t, r.Body, `data-filter="mine=1"`)
}

// Chips are links, so the one that is on says aria-current, inside a
// labelled group; a tag filter spelled in another case still marks its
// chip (VAQ-43).
func TestFilterChipsAccessible(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "tagged", Tags: []string{"école"}})
	for _, path := range []string{"/p/VAQ?tag=%C3%89cole", "/p/VAQ/list?tag=%C3%89cole"} {
		page := e.Client().Get(path).Body
		contains(t, page, `role="group" aria-label="Filters"`, `aria-current="true" class="rounded-full border px-3 py-0.5 text-xs border-accent`, ">école</a>")
		lacks(t, page, "aria-pressed", ">École</a>")
	}
}
