//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The board at true phone, tablet and desktop widths, light and dark.
// Tailwind's md starts at 768 px and lg at 1024 px: below md an open rail
// overlays the columns, below lg the milestones are a disclosure above
// the board.

func widthFixture(t *testing.T) (*webtest.Env, *fixture) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	ctx := context.Background()
	beta, err := env.Deps.Core.CreateMilestone(ctx, env.Deps.Actor.ID, f.project.ID, core.MilestoneInput{Name: "Beta launch"})
	if err != nil {
		t.Fatal(err)
	}
	later, err := env.Deps.Core.CreateMilestone(ctx, env.Deps.Actor.ID, f.project.ID, core.MilestoneInput{Name: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	in := func(id string) func(*core.TicketInput) { return func(in *core.TicketInput) { in.MilestoneID = id } }
	f.ticket("Ship the beta", "", in(beta.ID))                                                                                                         // WEB-1
	f.ticket("Plan what comes later", "", in(later.ID))                                                                                                // WEB-2
	f.ticket("A ticket whose title is long enough to wrap across several lines of a narrow card and then some more words", "In progress", in(beta.ID)) // WEB-3
	f.ticket("Someday maybe", "Backlog")                                                                                                               // WEB-4
	f.ticket("Another idea", "Backlog")                                                                                                                // WEB-5
	f.ticket("Dropped", "Cancelled")                                                                                                                   // WEB-6
	// A title with a word too long for a phone, the way a code path or a
	// URL is: it wraps rather than widening the page (VAQ-140).
	f.ticket("Check internal/e2e/widths_test.go::TestPhoneWidthNoSidewaysScroll_at_every_phone_width", "") // WEB-7
	f.ticket("Someday_rename_internal/web/handlers/api_search_test.go_without_any_spaces", "Backlog")      // WEB-8
	// A description holding a wide table (VAQ-203): it scrolls in its own box.
	row := func(c string) string {
		return strings.Repeat("| "+c+"_long_cell_text ", 6) + "|\n"
	}
	table := row("head") + "|---|---|---|---|---|---|\n" + row("alpha") + row("bravo")
	f.ticket("Ticket with a wide table", "", func(in *core.TicketInput) { in.Body = table }) // WEB-9
	return env, f
}

func rail(f *fixture, status string) string {
	return `details[data-rail="` + f.status[status].ID + `"]`
}

func visible(sel string) string {
	return `(function () { var el = document.querySelector(` + js(sel) + `); return !!el && el.checkVisibility(); })()`
}

const (
	disclosure = `details[data-milestones]`
	sidePanel  = `aside[aria-label="Milestones"]`
)

// milestonesDisclosure checks the disclosure shows instead of the side
// panel, opens, and filters the board.
func milestonesDisclosure(t *testing.T, p *page) {
	if !eval[bool](p, visible(disclosure)) || eval[bool](p, visible(sidePanel)) {
		t.Fatal("want the milestones disclosure, not the side panel")
	}
	p.openDetails(disclosure)
	link := disclosure + ` a[href*="milestone="]`
	if !eval[bool](p, visible(link)) {
		t.Fatal("the open disclosure shows no milestone links")
	}
	// The first link is the first open milestone, Beta launch.
	p.reloaded(func() { p.click(link) }, `location.search.indexOf("milestone=") >= 0`)
	summary := eval[string](p, `document.querySelector(`+js(disclosure+" > summary")+`).textContent`)
	if !strings.Contains(summary, "Beta launch") {
		t.Fatalf("disclosure summary = %q, want it to name Beta launch", strings.Join(strings.Fields(summary), " "))
	}
	p.until(`!!document.querySelector('[data-card="WEB-1"]') && !!document.querySelector('[data-card="WEB-3"]') && !document.querySelector('[data-card="WEB-2"]')`)
}

// overlays reports whether the rail's open panel is lifted over the
// columns (absolute, and crossing the columns' box).
func overlays(p *page, f *fixture, status string) bool {
	return eval[bool](p, fmt.Sprintf(`(function () {
		var panel = document.querySelector('[data-rail-panel="%s"]');
		var cols = document.querySelector("[data-board-scroll]").getBoundingClientRect();
		var r = panel.getBoundingClientRect();
		return getComputedStyle(panel).position === "absolute" &&
			r.left < cols.right && r.right > cols.left && r.top < cols.bottom && r.bottom > cols.top;
	})()`, f.status[status].ID))
}

func isOpen(sel string) string { return `document.querySelector(` + js(sel) + `).open` }

func focusedOn(sel string) string {
	return `document.activeElement === document.querySelector(` + js(sel) + `)`
}

func TestNarrowBoard(t *testing.T) {
	for _, w := range []int64{375, 767, 768} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			env, f := widthFixture(t)
			p := open(t, env, w, 900)
			p.goTo("/p/WEB/board")
			milestonesDisclosure(t, p)

			p.goTo("/p/WEB/board")
			backlog, cancelled := rail(f, "Backlog"), rail(f, "Cancelled")
			p.openDetails(backlog)
			if w >= 768 {
				// From md, rails open in place, and more than one can be open.
				if overlays(p, f, "Backlog") {
					t.Fatal("the backlog rail overlays the columns from md")
				}
				p.openDetails(cancelled)
				if !eval[bool](p, isOpen(backlog)) {
					t.Fatal("opening the cancelled rail closed the backlog rail from md")
				}
				return
			}
			if !overlays(p, f, "Backlog") {
				t.Fatal("the open backlog rail doesn't overlay the columns")
			}

			// Its Close button closes it and puts focus on its strip.
			p.toggled(backlog, func() { p.click(backlog + " [data-rail-close]") })
			p.until(`!` + isOpen(backlog))
			p.until(focusedOn(backlog + " > summary"))

			// Escape does the same.
			p.openDetails(cancelled)
			if !overlays(p, f, "Cancelled") {
				t.Fatal("the open cancelled rail doesn't overlay the columns")
			}
			p.toggled(cancelled, func() { p.key(escape) })
			p.until(`!` + isOpen(cancelled))
			p.until(focusedOn(cancelled + " > summary"))

			// Opening one rail closes the other.
			p.openDetails(backlog)
			p.toggled(backlog, func() { p.openDetails(cancelled) })
			p.until(isOpen(cancelled) + ` && !` + isOpen(backlog))

			// A live refresh keeps the open rail open.
			p.markBoard()
			f.ticket("Arrives live", "Backlog")
			// The new board is swapped in, then app.js reopens the rail
			// once htmx settles.
			p.until(boardSwapped + ` && document.getElementById("board").textContent.indexOf("Arrives live") >= 0 && ` + isOpen(cancelled))
			if eval[bool](p, isOpen(backlog)) {
				t.Fatal("the live refresh opened the backlog rail too")
			}
			if !overlays(p, f, "Cancelled") {
				t.Fatal("after the refresh the cancelled rail doesn't overlay the columns")
			}
		})
	}
}

// Two rails opened back to back below md, before the first one's toggle
// event has run (VAQ-141): the rail opened last stays open.
func TestNarrowRailsOpenedBackToBack(t *testing.T) {
	env, f := widthFixture(t)
	p := open(t, env, 375, 900)
	p.goTo("/p/WEB/board")
	backlog, cancelled := rail(f, "Backlog"), rail(f, "Cancelled")
	p.toggled(cancelled, func() {
		eval[bool](p, `(function () {
			document.querySelector(`+js(backlog+" > summary")+`).click();
			document.querySelector(`+js(cancelled+" > summary")+`).click();
			return true;
		})()`)
	})
	// One more task, for any toggle event queued by app.js's answer.
	eval[bool](p, `(window.vqTicked = false, setTimeout(function () { window.vqTicked = true; }, 50), true)`)
	p.until(`window.vqTicked`)
	if !eval[bool](p, isOpen(cancelled)) || eval[bool](p, isOpen(backlog)) {
		t.Fatalf("backlog open %v, cancelled open %v; want only the cancelled rail, opened last, open",
			eval[bool](p, isOpen(backlog)), eval[bool](p, isOpen(cancelled)))
	}
}

func TestWideBoard(t *testing.T) {
	env, f := widthFixture(t)
	p := open(t, env, 1280, 900)
	p.goTo("/p/WEB/board")
	if !eval[bool](p, visible(sidePanel)) || eval[bool](p, visible(disclosure)) {
		t.Fatal("want the side milestone panel, not the disclosure")
	}
	backlog, cancelled := rail(f, "Backlog"), rail(f, "Cancelled")
	p.openDetails(backlog)
	if overlays(p, f, "Backlog") {
		t.Fatal("the backlog rail overlays the columns at 1280 px")
	}
	if !eval[bool](p, visible(`[data-card="WEB-4"]`)) {
		t.Fatal("the open backlog rail doesn't show its cards")
	}
	p.openDetails(cancelled)
	if !eval[bool](p, isOpen(backlog)) {
		t.Fatal("opening the cancelled rail closed the backlog rail at 1280 px")
	}
	// Escape is for narrow screens only.
	p.key(escape)
	if !eval[bool](p, isOpen(backlog)+` && `+isOpen(cancelled)) {
		t.Fatal("Escape closed a rail at 1280 px")
	}
}

// At 1280 px with the milestone panel showing and both rails open, every
// active and done column keeps its full width; the columns scroll sideways
// instead (VAQ-142).
func TestWideBoardColumnsKeepTheirWidth(t *testing.T) {
	env, f := widthFixture(t)
	f.ticket("Shipped already", "Done")
	f.ticket("Being reviewed", "In review")
	p := open(t, env, 1280, 900)
	p.goTo("/p/WEB/board")
	p.openDetails(rail(f, "Backlog"))
	p.openDetails(rail(f, "Cancelled"))
	if !eval[bool](p, visible(sidePanel)) {
		t.Fatal("want the side milestone panel at 1280 px")
	}
	type col struct {
		Name  string
		Width float64
	}
	cols := eval[[]col](p, `Array.prototype.map.call(document.querySelectorAll("[data-board-scroll] > section"), function (s) {
		return {Name: s.getAttribute("aria-label"), Width: s.getBoundingClientRect().width};
	})`)
	if len(cols) < 4 {
		t.Fatalf("columns = %+v, want todo, in progress, in review and done", cols)
	}
	// 16rem, the width a card is drawn for (w-64).
	const minimum = 256
	for _, c := range cols {
		if c.Width < minimum {
			t.Errorf("the %s column is %.0f px wide, want at least %d", c.Name, c.Width, minimum)
		}
	}
	if w := eval[int64](p, `document.documentElement.scrollWidth`); w > 1280 {
		t.Errorf("the page is %d px wide at 1280 px; the columns should scroll in their own box", w)
	}
	if !eval[bool](p, `(function () { var s = document.querySelector("[data-board-scroll]"); return s.scrollWidth > s.clientWidth; })()`) {
		t.Error("the columns don't scroll sideways though they can't all fit")
	}
}

// colours are computed colours on the page and what the theme tokens
// resolve to in the scheme in force.
type colours struct {
	Dark                       bool
	BodyBg, BodyInk, CardBg    string
	ColumnBg                   string
	Paper, Ink, PaperAlt, Line string
	CardBorder                 string
}

func TestThemeLightAndDark(t *testing.T) {
	env, _ := widthFixture(t)
	got := map[string]colours{}
	for _, scheme := range []string{"light", "dark"} {
		p := open(t, env, 1280, 900)
		p.scheme(scheme)
		p.goTo("/p/WEB/board")
		// Each token is resolved on a probe element through the CSSOM
		// (allowed by the CSP, unlike a style attribute).
		c := eval[colours](p, `(function () {
			function token(name) {
				var d = document.createElement("div");
				d.style.color = "var(" + name + ")";
				document.body.appendChild(d);
				var v = getComputedStyle(d).color;
				d.remove();
				return v;
			}
			var body = getComputedStyle(document.body);
			var card = getComputedStyle(document.querySelector("[data-card]"));
			var col = getComputedStyle(document.querySelector("[data-board-scroll] > section"));
			return {
				Dark: matchMedia("(prefers-color-scheme: dark)").matches,
				BodyBg: body.backgroundColor, BodyInk: body.color,
				CardBg: card.backgroundColor, CardBorder: card.borderTopColor,
				ColumnBg: col.backgroundColor,
				Paper: token("--color-paper"), Ink: token("--color-ink"),
				PaperAlt: token("--color-paper-alt"), Line: token("--color-line")
			};
		})()`)
		if c.Dark != (scheme == "dark") {
			t.Fatalf("%s: prefers-color-scheme: dark matches = %v", scheme, c.Dark)
		}
		for _, chk := range []struct{ what, got, want string }{
			{"body background", c.BodyBg, c.Paper},
			{"body text", c.BodyInk, c.Ink},
			{"card background", c.CardBg, c.Paper},
			{"card border", c.CardBorder, c.Line},
			{"column background", c.ColumnBg, c.PaperAlt},
		} {
			if chk.got != chk.want {
				t.Errorf("%s: %s = %s, want the token's %s", scheme, chk.what, chk.got, chk.want)
			}
		}
		got[scheme] = c
	}
	if got["light"].Paper == got["dark"].Paper || got["light"].Ink == got["dark"].Ink {
		t.Fatalf("paper and ink are the same in light and dark: %+v", got)
	}
}

// At 320 and 375 px no page is wider than the screen (VAQ-128,
// VAQ-140). The board's columns scroll sideways inside their own box; the
// page itself never does.
func TestPhoneWidthNoSidewaysScroll(t *testing.T) {
	env, _ := widthFixture(t)
	empty := webtest.New(t) // the projects page with no projects
	for _, w := range []int64{320, 375} {
		t.Run(fmt.Sprint(w), func(t *testing.T) { phoneWidth(t, env, empty, w) })
	}
}

func phoneWidth(t *testing.T, env, empty *webtest.Env, width int64) {
	for _, c := range []struct {
		env   *webtest.Env
		paths []string
	}{
		{empty, []string{"/"}},
		{env, []string{
			"/", "/p/WEB", "/p/WEB?tag=ui&priority=2&mine=1", "/p/WEB/board", "/p/WEB/board?tag=ui&priority=2&mine=1", "/p/WEB/list", "/p/WEB/new", "/p/WEB/settings",
			"/t/WEB-3", "/t/WEB-7", "/t/WEB-1/edit", "/projects/new", "/search?q=beta", "/search?q=check", "/activity",
			"/t/WEB-9", "/help", "/help/cli", "/help/api", "/help/keyboard-shortcuts", "/admin", "/admin/tokens", "/about/licenses",
		}},
	} {
		p := open(t, c.env, width, 800)
		for _, path := range c.paths {
			p.goTo(path)
			if w := eval[int64](p, `document.documentElement.scrollWidth`); w > width {
				t.Errorf("%s is %d px wide at %d px", path, w, width)
			}
		}
	}
	// The wide table scrolls inside its own box, not the page (VAQ-203).
	tp := open(t, env, width, 800)
	tp.goTo("/t/WEB-9")
	if !eval[bool](tp, `(function () { var t = document.querySelector(".prose-vq table"); return !!t && t.scrollWidth > t.clientWidth; })()`) {
		t.Errorf("the wide table doesn't scroll inside its own box at %d px", width)
	}
	// An open rail's cards overlay the columns, and fit too.
	p := open(t, env, width, 800)
	p.goTo("/p/WEB/board")
	for _, d := range []string{"details[data-rail]:first-of-type", "details[data-milestones]"} {
		p.openDetails(d)
		if w := eval[int64](p, `document.documentElement.scrollWidth`); w > width {
			t.Errorf("the board with %s open is %d px wide at %d px", d, w, width)
		}
	}
}
