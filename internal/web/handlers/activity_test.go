package handlers_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestActivityShowsRecentEvents(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	c.Post("/projects", url.Values{"key": {"VAQ"}, "name": {"Vaquero"}})
	c.Post("/p/VAQ/tickets", url.Values{"title": {"Fix login"}})
	ss := statuses(t, e, mustProject(t, e))
	c.Post("/t/VAQ-1", url.Values{"title": {"Fix login"}, "status": {ss[2].ID}})
	c.Post("/t/VAQ-1/comments", url.Values{"body": {"On it."}})
	page := c.Get("/activity").Body
	lines := []string{"commented on VAQ-1", "moved VAQ-1 from Todo to In progress", "created VAQ-1 Fix login", "created project VAQ"}
	last := -1
	for _, l := range lines {
		i := strings.Index(page, l)
		if i < 0 {
			t.Fatalf("page lacks %q", l)
		}
		if i < last {
			t.Fatalf("%q is out of order (newest first)", l)
		}
		last = i
	}
	contains(t, page, `href="/t/VAQ-1"`, "Owner")
}

func TestActivityInNav(t *testing.T) {
	contains(t, webtest.New(t).Client().Get("/").Body, `href="/activity"`)
}
