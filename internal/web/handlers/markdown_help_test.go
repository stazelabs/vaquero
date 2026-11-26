package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// Every box that takes Markdown (a ticket's description, a new comment, a
// comment being edited) has a small "Markdown" link to the article under
// it; boxes that take plain text don't (VAQ-106).
func TestMarkdownLinkUnderEveryMarkdownBox(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Notes"})
	cm, err := e.Deps.Core.AddComment(t.Context(), e.Deps.Actor.ID, tk.ID, "Hello.")
	if err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	link := `<a href="/help/markdown" data-markdown-help`
	for path, want := range map[string]int{
		"/p/VAQ/new":             1, // description
		"/t/VAQ-1/edit":          1, // description
		"/t/VAQ-1":               1, // new comment
		"/t/VAQ-1?edit=" + cm.ID: 2, // new comment, comment being edited
		"/p/VAQ/settings":        0, // project and milestone descriptions are plain text
	} {
		if r := c.Get(path); r.Status != 200 {
			t.Fatalf("%s: %d", path, r.Status)
		}
		body := c.Get(path).Body
		if got := strings.Count(body, link); got != want {
			t.Errorf("%s: %d Markdown links, want %d", path, got, want)
		}
	}
}
