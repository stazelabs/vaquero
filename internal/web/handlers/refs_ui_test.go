package handlers_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestTicketPageReferences(t *testing.T) {
	e := webtest.New(t)
	linkTickets(t, e)
	c := e.Client()
	if page := c.Get("/t/VAQ-1").Body; strings.Contains(page, "References</h2>") {
		t.Fatal("a ticket with none shows a References section")
	}
	repo := gittest.Repo(t)
	repo.WriteCommit("a.md", "a", "first")
	path, main := repo.Dir, "main"
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{RepoPath: &path, MainBranch: &main}); err != nil {
		t.Fatal(err)
	}
	t1, _, _ := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: 1})
	add := func(in core.RefInput) string {
		r, err := e.Deps.Core.AddRef(bg, e.Deps.Actor.ID, t1.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	file := add(core.RefInput{Kind: "spec", Target: "docs/a b.md", Anchor: "intro", Label: "The spec"})
	add(core.RefInput{Kind: "design", Target: "https://example.com/d?x=1&y=2"})
	add(core.RefInput{Kind: "other", Target: "docs/z.md"})

	page := c.Get("/t/VAQ-1").Body
	sec := page[strings.Index(page, "References</h2>"):]
	sec = sec[:strings.Index(sec, "</section>")]
	for _, want := range []string{
		`href="/p/VAQ/files/docs/a%20b.md?ref=` + file + `#doc-intro"`, // a repo path opens the file view, from this reference
		"The spec", "spec", "#intro",
		`href="https://example.com/d?x=1&amp;y=2"`, `target="_blank"`, `rel="noopener noreferrer"`,
		"docs/z.md", // no label: the target
		`action="/t/VAQ-1/refs/` + file + `/delete"`, `data-confirm=`,
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("References lacks %q:\n%s", want, sec)
		}
	}
	if strings.Count(sec, `rel="noopener noreferrer"`) != 1 {
		t.Errorf("only the URL opens in a new tab:\n%s", sec)
	}
	if regexp.MustCompile(`href="/p/VAQ/files/[^"]*"[^>]*target=`).MatchString(sec) {
		t.Error("a repo path opens in a new tab")
	}

	// Remove: a redirect with a toast; another ticket's reference is refused.
	if r := c.Post("/t/VAQ-2/refs/"+file+"/delete", nil); r.Status != 303 {
		t.Fatalf("wrong ticket: %d", r.Status)
	}
	if page := c.Get("/t/VAQ-1").Body; !strings.Contains(page, "The spec") {
		t.Fatal("removed through another ticket")
	}
	if r := c.Post("/t/VAQ-1/refs/"+file+"/delete", nil); r.Status != 303 {
		t.Fatalf("remove: %d", r.Status)
	}
	if page := c.Get("/t/VAQ-1").Body; strings.Contains(page, "The spec") {
		t.Fatal("not removed")
	}
}
