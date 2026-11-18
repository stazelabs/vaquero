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
