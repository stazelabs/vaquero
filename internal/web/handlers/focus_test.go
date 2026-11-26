package handlers_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The Focus view's empty lanes say what they mean; the Next lane names
// Needs decision only when the project has it.
func TestFocusEmptyStatesCopy(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	page := c.Get("/p/VAQ").Body
	contains(t, page, `id="focus"`, "Nothing ready. Triage the backlog.", "Nothing in flight.", "Nothing finished this week.")
	lacks(t, page, "Needs decision")
	if _, err := e.Deps.Core.CreateStatus(bg, e.Deps.Actor.ID, p.ID, "Needs decision", "backlog"); err != nil {
		t.Fatal(err)
	}
	contains(t, c.Get("/p/VAQ").Body, "Nothing ready. Triage the backlog or check Needs decision.")
}

// The lanes, their counts and their links.
func TestFocusLanes(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	for i := range 7 {
		ticket(t, e, p, core.TicketInput{Title: fmt.Sprint("ready ", i)})
	}
	ticket(t, e, p, core.TicketInput{Title: "mine", AssigneeID: e.Deps.Actor.ID}) // VAQ-8, waiting
	ticket(t, e, p, core.TicketInput{Title: "doing", StatusID: statusID(t, e, "In progress")})
	ticket(t, e, p, core.TicketInput{Title: "shipped", StatusID: statusID(t, e, "Done")})
	page := e.Client().Get("/p/VAQ").Body
	contains(t, page, "2 more ready", "1 waiting", "Assigned to Owner", "doing", "shipped", "today",
		`href="/p/VAQ/list?category=done"`, "Older: in List",
		`hx-get="/p/VAQ/focus/fragment"`, `hx-trigger="vq:change"`, "data-live",
		`href="/p/VAQ/board"`, `aria-current="page"`)
	lacks(t, page, "Nothing ready.", "Nothing in flight.", "Nothing finished this week.", "data-board-column")
	// Lanes stack in the order Next, In flight, Just finished.
	next, flight, finished := strings.Index(page, `data-lane="next"`), strings.Index(page, `data-lane="flight"`), strings.Index(page, `data-lane="finished"`)
	if next < 0 || next > flight || flight > finished {
		t.Fatalf("lane order %d %d %d", next, flight, finished)
	}
}

// Review Focus 1: a bookmarked board address (/p/{key} with the board's
// filters) opens Focus with those filters, and its live part keeps them.
func TestOldBoardURLOpensFocusWithFilters(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	ticket(t, e, p, core.TicketInput{Title: "mine and ui", Tags: []string{"ui"}, StatusID: statusID(t, e, "In progress"), AssigneeID: e.Deps.Actor.ID})
	ticket(t, e, p, core.TicketInput{Title: "ui only", Tags: []string{"ui"}, StatusID: statusID(t, e, "In progress")})
	ticket(t, e, p, core.TicketInput{Title: "mine only", StatusID: statusID(t, e, "In progress"), AssigneeID: e.Deps.Actor.ID})
	r := e.Client().Get("/p/VAQ?tag=ui&mine=1")
	if r.Status != 200 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, r.Body, `id="focus"`, "mine and ui", `hx-get="/p/VAQ/focus/fragment?mine=1&amp;tag=ui"`)
	lacks(t, r.Body, "ui only", "mine only")
	// The active filters show as removable chips, and the tabs keep them.
	contains(t, r.Body, `aria-label="Remove filter Tag: ui"`, `href="/p/VAQ?mine=1"`, `href="/p/VAQ/board?mine=1&amp;tag=ui"`)
	// The fragment is the live part alone.
	f := e.Client().Get("/p/VAQ/focus/fragment?tag=ui&mine=1")
	if f.Status != 200 || !strings.HasPrefix(strings.TrimSpace(f.Body), `<div id="focus"`) || strings.Contains(f.Body, "<html") {
		t.Fatalf("%d %.80q", f.Status, f.Body)
	}
	contains(t, f.Body, "mine and ui")
	lacks(t, f.Body, "ui only")
}

// The board is its own page, and its live part moved under it.
func TestBoardPageAndFragmentRoutes(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	page := c.Get("/p/VAQ/board?mine=1")
	if page.Status != 200 || !strings.Contains(page.Body, "<html") {
		t.Fatalf("%d", page.Status)
	}
	contains(t, page.Body, `id="board"`, `hx-get="/p/VAQ/board/fragment?mine=1"`, `data-filter="mine=1"`)
	frag := c.Get("/p/VAQ/board/fragment?mine=1")
	if frag.Status != 200 || !strings.HasPrefix(strings.TrimSpace(frag.Body), `<div id="board"`) || strings.Contains(frag.Body, "<html") {
		t.Fatalf("%d %.80q", frag.Status, frag.Body)
	}
	contains(t, frag.Body, `data-filter="mine=1"`)
	// A gone project's board and fragment go home with a toast.
	for _, path := range []string{"/p/GONE/board/fragment", "/p/GONE/focus/fragment"} {
		if r := c.Get(path); r.Status != 303 {
			t.Errorf("%s: %d", path, r.Status)
		}
	}
}

// The tag, priority and milestone filters are menus; the tag menu lists
// every tag in the project, not only those on the page.
func TestFilterMenus(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	if _, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Beta"}); err != nil {
		t.Fatal(err)
	}
	ticket(t, e, p, core.TicketInput{Title: "a", Tags: []string{"ui"}})
	ticket(t, e, p, core.TicketInput{Title: "b", Tags: []string{"api"}})
	for _, base := range []string{"/p/VAQ", "/p/VAQ/board", "/p/VAQ/list"} {
		page := e.Client().Get(base + "?tag=ui").Body
		contains(t, page, `data-menu="tags"`, `data-menu-search`, `data-menu-item="api"`, `data-menu-item="ui"`,
			`data-menu="priority"`, `data-menu="milestone"`, ">Beta<", "No milestone",
			`href="`+base+`?mine=1&amp;tag=ui"`, `href="`+base+`?tag=api"`, "Clear filters")
	}
	contains(t, e.Client().Get("/p/VAQ/list").Body, `data-menu="category"`)
	lacks(t, e.Client().Get("/p/VAQ").Body, `data-menu="category"`)
}

var showMore = regexp.MustCompile(`hx-get="(/p/VAQ/list\?[^"]*)"[^>]*>\s*Show 50 more`)

// Done and Cancelled start collapsed and page 50 at a time; the page
// renders as many as the browser says it shows (shown=STATUS:N), so a live
// refresh keeps them.
func TestListDonePagesFifty(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	done := statusID(t, e, "Done")
	for i := range 120 {
		ticket(t, e, p, core.TicketInput{Title: fmt.Sprint("done ", i), StatusID: done})
	}
	ticket(t, e, p, core.TicketInput{Title: "to do"})
	c := e.Client()
	list := c.Get("/p/VAQ/list").Body
	group := `data-list-group="` + done + `"`
	contains(t, list, group, "Done <span", "· 120")
	if n := strings.Count(list, `data-card="VAQ-`); n != 51 {
		t.Fatalf("%d rows, want 50 done and 1 todo", n)
	}
	// The todo group is open, the done group closed.
	if !regexp.MustCompile(`<details[^>]*data-list-group="[^"]+"[^>]*open`).MatchString(list) {
		t.Error("no open group")
	}
	if regexp.MustCompile(`<details[^>]*` + group + `[^>]*open`).MatchString(list) {
		t.Error("done starts open")
	}
	m := showMore.FindStringSubmatch(list)
	if m == nil || !strings.Contains(m[1], "offset=50") || !strings.Contains(m[1], "group="+done) {
		t.Fatalf("show more: %v", m)
	}
	// The next page is rows alone, with the next button.
	page2 := c.Get(strings.ReplaceAll(m[1], "&amp;", "&")).Body
	if strings.Contains(page2, "<html") || strings.Count(page2, `data-card="VAQ-`) != 50 {
		t.Fatalf("page 2: %.200q", page2)
	}
	m = showMore.FindStringSubmatch(page2)
	if m == nil || !strings.Contains(m[1], "offset=100") {
		t.Fatalf("page 2 show more: %v", m)
	}
	page3 := c.Get(strings.ReplaceAll(m[1], "&amp;", "&")).Body
	if strings.Count(page3, `data-card="VAQ-`) != 20 || strings.Contains(page3, "Show 50 more") {
		t.Fatalf("page 3: %d rows", strings.Count(page3, `data-card="VAQ-`))
	}
	// A refresh that says 100 are shown renders 100, with the button at 100.
	again := c.Get("/p/VAQ/list?shown=" + done + ":100").Body
	if n := strings.Count(again, `data-card="VAQ-`); n != 101 {
		t.Fatalf("%d rows after a refresh at 100", n)
	}
	m = showMore.FindStringSubmatch(again)
	if m == nil || !strings.Contains(m[1], "offset=100") {
		t.Fatalf("refresh show more: %v", m)
	}
	// The todo group isn't paged; a bad offset or group is the first page.
	for _, q := range []string{"group=" + done + "&offset=-5", "group=nope&offset=50", "shown=" + done + ":abc", "shown=junk"} {
		if r := c.Get("/p/VAQ/list?" + q); r.Status != 200 {
			t.Errorf("%s: %d", q, r.Status)
		}
	}
	// The list filtered to done opens its group.
	if !regexp.MustCompile(`<details[^>]*` + group + `[^>]*open`).MatchString(c.Get("/p/VAQ/list?category=done").Body) {
		t.Error("done closed when the list is filtered to it")
	}
}
