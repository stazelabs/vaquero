//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-111: the Focus view, the filter menus and the list's paging.

func focusFixture(t *testing.T) (*webtest.Env, *fixture) {
	env := webtest.New(t)
	f := newProject(t, env, "FOC")
	prio := func(n int64) func(*core.TicketInput) { return func(in *core.TicketInput) { in.Priority = n } }
	tags := func(ts ...string) func(*core.TicketInput) { return func(in *core.TicketInput) { in.Tags = ts } }
	for i := range 7 {
		f.ticket(fmt.Sprintf("Ready ticket %d with a title long enough to wrap on a phone", i+1), "", prio(int64(i%5)), tags("frontend")) // FOC-1..7
	}
	f.ticket("Mine already", "", func(in *core.TicketInput) { in.AssigneeID = env.Deps.Actor.ID }) // FOC-8, waiting
	f.ticket("Being built", "In progress", tags("backend"))                                        // FOC-9
	f.ticket("Also being built", "In progress", tags("docs"))                                      // FOC-10
	f.ticket("Up for review", "In review")                                                         // FOC-11
	f.ticket("Shipped", "Done")                                                                    // FOC-12
	f.ticket("Shipped too", "Done")                                                                // FOC-13
	return env, f
}

// focusLayout is where the lanes are drawn, and the page's width.
type focusLayout struct {
	Next, Flight, Finished rect
	Scroll                 float64
	LaneBg, PaperAlt       string
}

const focusLayoutJS = `(function () {
	function box(sel) {
		var r = document.querySelector(sel).getBoundingClientRect();
		return {Left: r.left, Top: r.top, Right: r.right, Bottom: r.bottom, Width: r.width, Height: r.height};
	}
	var d = document.createElement("div");
	d.style.color = "var(--color-paper-alt)";
	document.body.appendChild(d);
	var alt = getComputedStyle(d).color;
	d.remove();
	return {Next: box('[data-lane="next"]'), Flight: box('[data-lane="flight"]'), Finished: box('[data-lane="finished"]'),
		Scroll: document.documentElement.scrollWidth,
		LaneBg: getComputedStyle(document.querySelector('[data-lane="next"]')).backgroundColor, PaperAlt: alt};
})()`

// The lanes at phone, tablet and desktop widths, light and dark: stacked
// Next, In flight, Just finished below md, side by side from md, never
// wider than the screen; a live refresh keeps an opened "more" open.
func TestE2EFocusLanes(t *testing.T) {
	for _, w := range []int64{375, 768, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env, f := focusFixture(t)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/p/FOC")
				l := eval[focusLayout](p, focusLayoutJS)
				if l.Scroll > float64(w) {
					t.Fatalf("the page is %v px wide at %d px", l.Scroll, w)
				}
				if w < 768 {
					if !(l.Next.Bottom <= l.Flight.Top && l.Flight.Bottom <= l.Finished.Top) || l.Next.Left != l.Flight.Left {
						t.Fatalf("want the lanes stacked: %+v", l)
					}
				} else if !(l.Next.Top == l.Flight.Top && l.Flight.Top == l.Finished.Top && l.Next.Right <= l.Flight.Left && l.Flight.Right <= l.Finished.Left) {
					t.Fatalf("want the lanes side by side: %+v", l)
				}
				if l.LaneBg != l.PaperAlt {
					t.Errorf("lane background %s, want the paper-alt token %s", l.LaneBg, l.PaperAlt)
				}
				// Next: five cards, the most urgent first, then "2 more ready".
				first := eval[string](p, `document.querySelector('[data-lane="next"] [data-card]').dataset.card`)
				if n := eval[int](p, `document.querySelectorAll('[data-lane="next"] > ol > [data-card]').length`); n != 5 || first != "FOC-2" {
					t.Fatalf("next shows %d cards, first %s", n, first)
				}
				more := `details[data-keep="next-more"]`
				p.openDetails(more)
				p.until(visible(more + ` [data-card]`))
				// A live refresh redraws the lanes and keeps it open.
				eval[bool](p, `(document.getElementById("focus").vqOld = true)`)
				f.ticket("Arrives live", "In review")
				p.until(`!document.getElementById("focus").vqOld && document.getElementById("focus").textContent.indexOf("Arrives live") >= 0`)
				if !eval[bool](p, isOpen(more)) {
					t.Fatal("the live refresh closed 2 more ready")
				}
				p.openDetails(`details[data-keep="waiting"]`)
				if !eval[bool](p, `document.querySelector('details[data-keep="waiting"]').textContent.indexOf("Assigned to Owner") >= 0`) {
					t.Fatal("the waiting list doesn't say why")
				}
				// A filter menu stays on the screen.
				p.openDetails(`details[data-menu="tags"]`)
				if r := p.rect(`details[data-menu="tags"] > div`); r.Left < 0 || r.Right > float64(w) {
					t.Fatalf("the tags menu runs off the screen: %+v", r)
				}
				if sw := eval[float64](p, `document.documentElement.scrollWidth`); sw > float64(w) {
					t.Fatalf("with the menu open the page is %v px wide", sw)
				}
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[focusLayout](p, focusLayoutJS); again.LaneBg == l.LaneBg {
					t.Fatalf("the lane looks the same in %s and %s: %s", scheme, other, l.LaneBg)
				}
			})
		}
	}
}

// The Tags menu's search narrows the tags as you type; Enter follows the
// first one left, esc and a click outside close the menu.
func TestE2ETagMenuSearch(t *testing.T) {
	env, _ := focusFixture(t)
	p := open(t, env, 1280, 900)
	p.goTo("/p/FOC/board")
	menu := `details[data-menu="tags"]`
	shown := `Array.prototype.filter.call(document.querySelectorAll('details[data-menu="tags"] [data-menu-item]'), function (li) { return !li.hidden; }).map(function (li) { return li.dataset.menuItem; }).join(",")`
	p.openDetails(menu)
	p.until(`document.activeElement === document.querySelector("[data-menu-search]")`)
	if got := eval[string](p, shown); got != "backend,docs,frontend" {
		t.Fatalf("tags %q", got)
	}
	p.key("end")
	p.until(shown + ` === "backend,frontend"`)
	p.key("zz")
	p.until(shown + ` === "" && !document.querySelector("[data-menu-empty]").hidden`)
	// Opening another menu closes this one.
	p.toggled(menu, func() { p.click(`details[data-menu="priority"] > summary`) })
	p.until(`!` + isOpen(menu) + ` && ` + isOpen(`details[data-menu="priority"]`))
	// esc closes it and puts focus on its button.
	p.toggled(`details[data-menu="priority"]`, func() { p.key(escape) })
	p.until(focusedOn(`details[data-menu="priority"] > summary`))
	// A click outside closes it.
	p.openDetails(menu)
	p.toggled(menu, func() { p.click("h1") })
	p.until(`!` + isOpen(menu))
	// Enter in the search box follows the first tag left.
	p.openDetails(menu)
	p.do(focus("[data-menu-search]"))
	eval[bool](p, `(function () { var b = document.querySelector("[data-menu-search]"); b.value = ""; b.dispatchEvent(new Event("input", {bubbles: true})); return true; })()`)
	p.key("doc")
	p.until(shown + ` === "docs"`)
	p.reloaded(func() { p.key(enter) }, `location.pathname === "/p/FOC/board" && location.search === "?tag=docs"`)
	if !eval[bool](p, `!!document.querySelector('[data-card="FOC-10"]') && !document.querySelector('[data-card="FOC-9"]')`) {
		t.Fatal("the board isn't filtered to docs")
	}
	if !eval[bool](p, `!!document.querySelector('a[aria-label="Remove filter Tag: docs"]')`) {
		t.Fatal("no removable chip for the tag")
	}
}

// Review Focus 5: Done opened and "Show 50 more" used twice; a live refresh
// keeps the 150 shown and the group open, and so does a reload.
func TestE2EListPagingSurvivesRefresh(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "PAG")
	for i := range 160 {
		f.ticket(fmt.Sprint("Done ", i+1), "Done")
	}
	f.ticket("Still to do", "")
	p := open(t, env, 1280, 900)
	p.goTo("/p/PAG/list")
	done := `details[data-list-group="` + f.status["Done"].ID + `"]`
	rows := `document.querySelectorAll(` + js(done+` [data-card]`) + `).length`
	if eval[bool](p, isOpen(done)) {
		t.Fatal("Done starts open")
	}
	p.openDetails(done)
	if n := eval[int](p, rows); n != 50 {
		t.Fatalf("%d done rows, want 50", n)
	}
	for _, want := range []int{100, 150} {
		p.click(done + ` [data-list-more] button`)
		p.until(fmt.Sprintf(`%s === %d`, rows, want))
	}
	// Marked, so the wait is for the new list, settled (htmx wires its
	// buttons as it settles).
	eval[bool](p, `(function () {
		document.getElementById("list").vqOld = true;
		window.vqSettled = 0;
		document.body.addEventListener("htmx:afterSettle", function (ev) {
			if (ev.target.id === "list" && !ev.target.vqOld) { window.vqSettled++; }
		});
		return true;
	})()`)
	f.ticket("Arrives live", "")
	p.until(`window.vqSettled > 0 && !document.getElementById("list").vqOld && document.getElementById("list").textContent.indexOf("Arrives live") >= 0`)
	if n := eval[int](p, rows); n != 150 || !eval[bool](p, isOpen(done)) {
		t.Fatalf("after the refresh: %d rows, open %v", n, eval[bool](p, isOpen(done)))
	}
	// The next page carries on from 150.
	p.click(done + ` [data-list-more] button`)
	p.until(rows + ` === 160 && !document.querySelector(` + js(done+` [data-list-more]`) + `)`)
	// Folding is remembered for the project: Todo closed, Done open.
	todo := `details[data-list-group="` + f.status["Todo"].ID + `"]`
	p.toggled(todo, func() { p.click(todo + ` > summary`) })
	p.goTo("/p/PAG/list")
	p.until(isOpen(done) + ` && !` + isOpen(todo))
}

func TestE2EShortcutGF(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)
	for _, from := range []string{"/t/WEB-1", "/p/WEB/board", "/p/WEB/list"} {
		p.goTo(from)
		p.reloaded(func() { p.key("g"); p.key("f") }, `location.pathname === "/p/WEB" && !!document.getElementById("focus")`)
	}
	p.goTo("/activity")
	p.reloaded(func() { p.key("g"); p.key("f") }, `location.pathname === "/"`)
	// The shortcuts dialog lists it.
	p.key("?")
	p.until(`document.getElementById("vq-shortcuts").open && document.getElementById("vq-shortcuts").textContent.indexOf("g f") >= 0`)
}
