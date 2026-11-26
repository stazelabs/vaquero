package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestSearchPage(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Board columns", Body: "drag between columns"})
	page := e.Client().Get("/search?q=column").Body
	contains(t, page, `href="/t/VAQ-1"`, "Board", "<mark", `value="column"`)
	contains(t, e.Client().Get("/search?q=nothinglikethis").Body, "No tickets match.")
	contains(t, e.Client().Get("/search").Body, `name="q"`)
}

// Review Focus 1.
func TestSearchOddInput(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	for _, q := range []string{`"`, "AND (", "*", "-x", "NEAR(a b)", strings.Repeat("y", 500), "", "%00"} {
		if r := c.Get("/search?q=" + url.QueryEscape(q)); r.Status != 200 {
			t.Errorf("%q: %d", q, r.Status)
		}
	}
}

func TestSnippetEscaped(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Use <b>bold</b> carefully"})
	page := e.Client().Get("/search?q=carefully").Body
	lacks(t, page, "<b>bold</b>")
	contains(t, page, "&lt;b&gt;bold")
}

func TestShortcutsDialogPresent(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	for _, path := range []string{"/", "/p/VAQ", "/activity"} {
		contains(t, e.Client().Get(path).Body, `id="vq-shortcuts"`, `action="/search"`, "Keyboard shortcuts")
	}
	contains(t, e.Client().Get("/p/VAQ").Body, `data-project="VAQ"`)
}

// A long query is cut to 500 characters, not 500 bytes.
func TestSearchClipsCharacters(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	for _, ch := range []string{"é", "🐄"} {
		page := e.Client().Get("/search?q=" + url.QueryEscape(strings.Repeat(ch, 501))).Body
		if !strings.Contains(page, `value="`+strings.Repeat(ch, 500)+`"`) {
			t.Errorf("%s: the query isn't kept to 500 characters", ch)
		}
	}
}

// VAQ-40: a search with more matches than it shows says so.
func TestSearchSaysShowingOfTotal(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	for range 52 {
		ticket(t, e, p, core.TicketInput{Title: "many"})
	}
	contains(t, e.Client().Get("/search?q=many").Body, "Showing 50 of 52 matching tickets.")
	lacks(t, e.Client().Get("/search?q=nothingmatches").Body, "Showing")
}

// Help articles that match show above the tickets, linking the article or
// its heading (VAQ-63).
func TestSearchShowsHelp(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Compact the cache"})
	page := e.Client().Get("/search?q=compact").Body
	contains(t, page, `data-help-hits`, `href="/help/data-and-backups#compacting-the-database"`, "Compacting the database", `href="/t/VAQ-1"`)
	if strings.Index(page, "data-help-hits") > strings.Index(page, `href="/t/VAQ-1"`) {
		t.Fatal("help hits are not above the tickets")
	}
	// Help alone still says no tickets match.
	page = e.Client().Get("/search?q=keyboard").Body
	contains(t, page, `href="/help/keyboard-shortcuts"`, "No tickets match.")
	// No help match: no Help section.
	lacks(t, e.Client().Get("/search?q=cache").Body, "data-help-hits")
}
