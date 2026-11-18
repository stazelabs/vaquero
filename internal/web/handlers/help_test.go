package handlers_test

import (
	"html"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestHelpIndex(t *testing.T) {
	r := webtest.New(t).Client().Get("/help")
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	for _, s := range help.Sections() {
		if !strings.Contains(r.Body, s.Title) {
			t.Errorf("index lacks section %s", s.Title)
		}
		for _, a := range s.Articles {
			if !strings.Contains(r.Body, `href="/help/`+a.Slug+`"`) || !strings.Contains(r.Body, html.EscapeString(a.Summary)) {
				t.Errorf("index lacks %s", a.Slug)
			}
		}
	}
}

func TestHelpArticle(t *testing.T) {
	r := webtest.New(t).Client().Get("/help/cli")
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	for _, want := range []string{"<h1", "The command line", `href="#exit-codes"`, `id="exit-codes"`, `href="/help/cli.md"`, `href="/help/api"`, `data-icon="article"`} {
		if !strings.Contains(r.Body, want) {
			t.Errorf("article lacks %q", want)
		}
	}
}

func TestHelpMarkdown(t *testing.T) {
	r := webtest.New(t).Client().Get("/help/cli.md")
	a, _ := help.Get("cli")
	if r.Status != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/markdown") || r.Body != a.Markdown {
		t.Fatalf("%d %q", r.Status, r.Header.Get("Content-Type"))
	}
}

// Review Focus 4.
func TestHelpUnknown(t *testing.T) {
	c := webtest.New(t).Client()
	for _, p := range []string{"/help/nope", "/help/nope.md", "/help/..%2Fgo.mod", "/help/CLI"} {
		r := c.Get(p)
		if r.Status != 404 || strings.Contains(r.Body, "module github.com") {
			t.Errorf("%s: %d", p, r.Status)
		}
	}
}

func TestContextualHelpLinks(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Original"})
	c := e.Client()
	for page, slug := range map[string]string{
		"/p/VAQ":          "the-board",
		"/p/VAQ/settings": "projects-and-tickets",
		"/admin/tokens":   "api-tokens",
		"/search":         "search",
	} {
		if r := c.Get(page); !strings.Contains(r.Body, `href="/help/`+slug+`"`) {
			t.Errorf("%s doesn't link to /help/%s", page, slug)
		}
	}
	if _, err := e.Deps.Core.UpdateTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketInput{Title: "Theirs"}); err != nil {
		t.Fatal(err)
	}
	r := c.Post("/t/VAQ-1", url.Values{"title": {"Mine"}, "version": {"1"}})
	if r.Status != 409 || !strings.Contains(r.Body, `href="/help/editing-and-conflicts"`) {
		t.Fatalf("conflict banner: %d", r.Status)
	}
}

// The side panel draws the bot glyph for an agent assignee.
func TestTicketPanelAgentGlyph(t *testing.T) {
	e := webtest.New(t)
	tk := ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	agent, err := e.DB.Q.CreateActor(bg, gen.CreateActorParams{ID: idgen.New(), Kind: "agent", Name: "builder", CreatedAt: "2026-11-18T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{AssigneeID: &agent.ID}, 0); err != nil {
		t.Fatal(err)
	}
	r := e.Client().Get("/t/VAQ-1")
	if !strings.Contains(r.Body, `data-icon="agent"`) {
		t.Fatal("agent assignee drawn without the bot glyph")
	}
}
