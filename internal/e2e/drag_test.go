//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// Board drags with real mouse events: SortableJS runs in pointer-fallback
// mode, so a press, moves and a release drive it as a person's mouse does.

func dragFixture(t *testing.T) (*webtest.Env, *fixture) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	f.ticket("Alpha", "")             // WEB-1
	f.ticket("Bravo", "")             // WEB-2
	f.ticket("Charlie", "")           // WEB-3
	f.ticket("X-ray", "In progress")  // WEB-4
	f.ticket("Yankee", "In progress") // WEB-5
	return env, f
}

// card is the selector of a card on the board (not Sortable's drag image).
func card(key string) string {
	return `[data-board-column] > [data-card="` + key + `"]:not(.sortable-fallback)`
}

// columnIs is a page condition: the column for status lists exactly keys.
func columnIs(f *fixture, status string, keys ...string) string {
	return fmt.Sprintf(`Array.prototype.map.call(
		document.querySelectorAll('[data-board-column][data-status="%s"] > [data-card]:not(.sortable-fallback)'),
		function (el) { return el.dataset.card; }).join(",") === %s`, f.status[status].ID, js(strings.Join(keys, ",")))
}

// list is the selector of a status's card list.
func list(f *fixture, status string) string {
	return `[data-board-column][data-status="` + f.status[status].ID + `"]`
}

func (r rect) cy() float64 { return r.Top + r.Height/2 }

// grab presses on the card and waits for Sortable to start the drag.
func (p *page) grab(key string) rect {
	p.t.Helper()
	r := p.rect(card(key))
	p.mouse("press", r.cx(), r.cy())
	p.mouse("move", r.cx()+2, r.cy()+2)
	p.until(`window.vqDragging === true`)
	return r
}

// moveTo moves the held card from (x0, y0) to (x, y) in steps, then waits
// for the page to show placed.
func (p *page) moveTo(x0, y0, x, y float64, placed string) {
	p.t.Helper()
	const steps = 12
	for i := 1; i <= steps; i++ {
		p.mouse("move", x0+(x-x0)*float64(i)/steps, y0+(y-y0)*float64(i)/steps)
	}
	p.until(placed)
}

// release lets go at (x, y) and waits for the drag to end.
func (p *page) release(x, y float64) {
	p.t.Helper()
	p.mouse("release", x, y)
	p.until(`window.vqDragging === false`)
}

// markBoard tags the current #board element, so a test can tell when htmx
// has swapped in a new one.
func (p *page) markBoard() {
	p.t.Helper()
	eval[bool](p, `(document.getElementById("board").vqOld = true)`)
}

const boardSwapped = `!document.getElementById("board").vqOld`

// dropped waits for a drop's answer to be swapped in, then checks the
// page and the database agree on the columns, with one new event of kind.
func (p *page) dropped(f *fixture, events int64, kind string, cols map[string][]string) {
	p.t.Helper()
	p.until(boardSwapped)
	for status, keys := range cols {
		p.until(columnIs(f, status, keys...))
		if got := f.column(status); !equal(got, keys) {
			p.t.Fatalf("%s in the database = %v, want %v", status, got, keys)
		}
	}
	if n := f.events(); n != events+1 {
		p.t.Fatalf("events %d → %d, want one", events, n)
	}
	if got := f.lastEvent(); got != kind {
		p.t.Fatalf("event %s, want %s", got, kind)
	}
}

func TestDragReorderWithinColumn(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	events := f.events()
	p.markBoard()

	from := p.grab("WEB-1")
	to := p.rect(card("WEB-3"))
	p.moveTo(from.cx(), from.cy(), to.cx(), to.cy()+to.Height/4, columnIs(f, "Todo", "WEB-2", "WEB-3", "WEB-1"))
	p.release(to.cx(), to.cy()+to.Height/4)
	p.dropped(f, events, "ticket.reordered", map[string][]string{"Todo": {"WEB-2", "WEB-3", "WEB-1"}})
}

func TestDragToAnotherColumn(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	events := f.events()
	p.markBoard()

	from := p.grab("WEB-2")
	to := p.rect(card("WEB-5"))
	// Below Yankee, the last card of In progress.
	x, y := to.cx(), to.Bottom-4
	p.moveTo(from.cx(), from.cy(), x, y, columnIs(f, "In progress", "WEB-4", "WEB-5", "WEB-2"))
	p.release(x, y)
	p.dropped(f, events, "ticket.updated", map[string][]string{
		"Todo":        {"WEB-1", "WEB-3"},
		"In progress": {"WEB-4", "WEB-5", "WEB-2"},
	})
}

func TestDragToTopOfColumn(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	events := f.events()
	p.markBoard()

	from := p.grab("WEB-3")
	to := p.rect(card("WEB-4"))
	x, y := to.cx(), to.Top+4
	p.moveTo(from.cx(), from.cy(), x, y, columnIs(f, "In progress", "WEB-3", "WEB-4", "WEB-5"))
	p.release(x, y)
	p.dropped(f, events, "ticket.updated", map[string][]string{
		"Todo":        {"WEB-1", "WEB-2"},
		"In progress": {"WEB-3", "WEB-4", "WEB-5"},
	})
}

func TestDragToEmptyColumn(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	events := f.events()
	p.markBoard()

	from := p.grab("WEB-1")
	to := p.rect(list(f, "In review"))
	p.moveTo(from.cx(), from.cy(), to.cx(), to.cy(), columnIs(f, "In review", "WEB-1"))
	p.release(to.cx(), to.cy())
	p.dropped(f, events, "ticket.updated", map[string][]string{
		"Todo":      {"WEB-2", "WEB-3"},
		"In review": {"WEB-1"},
	})
}

// watchStream counts the live stream's change events.
func (p *page) watchStream() {
	p.t.Helper()
	p.until(`!!window.vqStream && window.vqStream.readyState === 1`)
	eval[bool](p, `(function () {
		window.vqChanges = 0;
		window.vqStream.addEventListener("change", function () { window.vqChanges++; });
		return true;
	})()`)
}

// watchBoardFetches counts htmx's requests for the board fragment, as
// they start.
func (p *page) watchBoardFetches() {
	p.t.Helper()
	eval[bool](p, `(function () {
		window.vqBoardFetches = 0;
		document.body.addEventListener("htmx:beforeRequest", function (ev) {
			var c = ev.detail.requestConfig;
			if (c.verb === "get" && c.path.indexOf("/p/WEB/board/fragment") === 0) { window.vqBoardFetches++; }
		});
		return true;
	})()`)
}

// A drop the server refuses (here the column's status was deleted while
// the card was in the air) reloads the board as the server has it, with
// the refusal as an error toast.
func TestDragRefusedReloadsWithToast(t *testing.T) {
	env, f := dragFixture(t)
	ctx := context.Background()
	triage, err := env.Deps.Core.CreateStatus(ctx, env.Deps.Actor.ID, f.project.ID, "Triage", "todo")
	if err != nil {
		t.Fatal(err)
	}
	f.reloadStatuses()
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	p.watchStream()

	from := p.grab("WEB-1")
	to := p.rect(list(f, "Triage"))
	p.moveTo(from.cx(), from.cy(), to.cx(), to.cy(), columnIs(f, "Triage", "WEB-1"))
	if err := env.Deps.Core.DeleteStatus(ctx, env.Deps.Actor.ID, triage.ID); err != nil {
		t.Fatal(err)
	}
	p.until(`window.vqChanges > 0`) // announced, and held while dragging
	events := f.events()
	p.reloaded(func() { p.release(to.cx(), to.cy()) },
		`!!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Choose a status from this project.") >= 0`)
	p.until(columnIs(f, "Todo", "WEB-1", "WEB-2", "WEB-3"))
	if eval[bool](p, `!!document.querySelector(`+js(list(f, "Triage"))+`)`) {
		t.Fatal("the deleted status's column is still on the board")
	}
	if got := f.column("Todo"); !equal(got, []string{"WEB-1", "WEB-2", "WEB-3"}) {
		t.Fatalf("Todo in the database = %v", got)
	}
	if n := f.events(); n != events {
		t.Fatalf("the refused drop wrote %d events", n-events)
	}
}

// A drop that fails (an error answer) puts its own card back where it
// was, says so and fetches the board again.
func TestDragFailedPutsCardBack(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	events := f.events()
	// The move and the board fetch both fail, so the card's place can only
	// be app.js's doing; the fetch is counted.
	p.srv.failWhen(func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/move") ||
			r.Method == http.MethodGet && r.URL.Path == "/p/WEB/board/fragment"
	})
	eval[bool](p, `(document.querySelector(`+js(card("WEB-1"))+`).vqMark = true)`)
	p.watchBoardFetches()
	fetches := p.srv.requests("GET /p/WEB/board/fragment")

	from := p.grab("WEB-1")
	to := p.rect(card("WEB-3"))
	p.moveTo(from.cx(), from.cy(), to.cx(), to.cy()+to.Height/4, columnIs(f, "Todo", "WEB-2", "WEB-3", "WEB-1"))
	p.release(to.cx(), to.cy()+to.Height/4)

	p.until(`!!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("The card couldn't be moved. Try again.") >= 0`)
	p.until(columnIs(f, "Todo", "WEB-1", "WEB-2", "WEB-3"))
	if !eval[bool](p, `document.querySelector(`+js(card("WEB-1"))+`).vqMark === true`) {
		t.Fatal("the card was redrawn, not put back")
	}
	p.until(`window.vqBoardFetches > 0`)
	if p.srv.requests("GET /p/WEB/board/fragment") <= fetches {
		t.Fatal("the board wasn't fetched again")
	}
	if n := f.events(); n != events {
		t.Fatalf("the failed drop wrote %d events", n-events)
	}
}

// A live refresh that arrives mid-drag waits for the drop: the board isn't
// swapped under the card, and once it is let go the change shows.
func TestDragHoldsLiveRefreshUntilDrop(t *testing.T) {
	env, f := dragFixture(t)
	p := open(t, env, 1600, 1000)
	p.goTo("/p/WEB/board")
	p.watchStream()
	p.watchBoardFetches()
	p.markBoard()

	from := p.grab("WEB-1")
	f.ticket("Arrived mid-drag", "")
	// The counter's listener runs after app.js's, which would have asked
	// htmx for the board by then (htmx starts the request at once).
	p.until(`window.vqChanges > 0`)
	if eval[int](p, `window.vqBoardFetches`) != 0 || eval[bool](p, boardSwapped) {
		t.Fatal("the board refreshed under the dragged card")
	}
	if !eval[bool](p, `window.vqDragging`) {
		t.Fatal("the drag ended early")
	}
	// Let go where it was picked up: no move, so the held refresh runs.
	p.release(from.cx()+2, from.cy()+2)
	p.until(boardSwapped + ` && document.getElementById("board").textContent.indexOf("Arrived mid-drag") >= 0`)
	p.until(columnIs(f, "Todo", "WEB-1", "WEB-2", "WEB-3", "WEB-6"))
}

// mouse sends one mouse event with the left button.
func (p *page) mouse(typ string, x, y float64) {
	p.t.Helper()
	opts := []chromedp.MouseOption{chromedp.ButtonLeft}
	switch typ {
	case "press":
		p.do(chromedp.MouseEvent(chromedp.MousePressed, x, y, append(opts, chromedp.ClickCount(1), buttons(1))...))
	case "move":
		p.do(chromedp.MouseEvent(chromedp.MouseMoved, x, y, append(opts, buttons(1))...))
	case "release":
		p.do(chromedp.MouseEvent(chromedp.MouseReleased, x, y, append(opts, chromedp.ClickCount(1))...))
	}
}

func buttons(n int64) chromedp.MouseOption {
	return func(p *input.DispatchMouseEventParams) { p.Buttons = n }
}

// key is t's key, like WEB-3.
func (f *fixture) key(t gen.Ticket) string {
	return core.TicketKey{Project: f.project.Key, Number: t.Number}.String()
}

// column is the keys in a status, top to bottom, as the database has them.
func (f *fixture) column(status string) []string {
	f.t.Helper()
	ctx := context.Background()
	rows, err := f.env.DB.RQ.ListColumn(ctx, f.status[status].ID)
	if err != nil {
		f.t.Fatal(err)
	}
	var keys []string
	for _, r := range rows {
		tk, err := f.env.DB.RQ.GetTicket(ctx, r.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		keys = append(keys, f.key(tk))
	}
	return keys
}

// lastEvent is the newest event's kind (the activity queries leave
// reorders out, so this reads the table).
func (f *fixture) lastEvent() string {
	f.t.Helper()
	var kind string
	if err := f.env.DB.R.QueryRowContext(context.Background(), `SELECT kind FROM events ORDER BY seq DESC LIMIT 1`).Scan(&kind); err != nil {
		f.t.Fatalf("last event: %v", err)
	}
	return kind
}

func equal(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }
