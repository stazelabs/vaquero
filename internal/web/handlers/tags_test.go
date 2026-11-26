package handlers_test

import (
	"net/url"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// tagged makes VAQ with VAQ-1 tagged bug and ui, VAQ-2 tagged ui, and the
// tags by name.
func tagged(t *testing.T, e *webtest.Env) (gen.Project, map[string]string) {
	t.Helper()
	p := project(t, e)
	for _, tags := range [][]string{{"bug", "ui"}, {"ui"}} {
		if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "T", Tags: tags}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := e.Deps.R.ListProjectTags(bg, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, r := range rows {
		ids[r.Name] = r.ID
	}
	return p, ids
}

func TestSettingsListsTags(t *testing.T) {
	e := webtest.New(t)
	_, ids := tagged(t, e)
	body := e.Client().Get("/p/VAQ/settings").Body
	contains(t, body, `<section id="tags"`, ">Tags<", "1 ticket", "2 tickets",
		`action="/p/VAQ/tags/`+ids["bug"]+`/rename"`, `value="bug"`,
		`action="/p/VAQ/tags/`+ids["ui"]+`/delete"`)
	f := formWith(body, "/p/VAQ/tags/"+ids["bug"]+"/delete")
	contains(t, f, `method="post"`, `data-confirm="Delete the tag bug? It comes off 1 ticket in VAQ."`)
}

func TestSettingsNoTags(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	contains(t, e.Client().Get("/p/VAQ/settings").Body, "No tags yet. Tags are added on tickets.")
}

func TestTagRenameAndDelete(t *testing.T) {
	e := webtest.New(t)
	p, ids := tagged(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/tags/"+ids["bug"]+"/rename", url.Values{"name": {"defect"}})
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Tag renamed.", `value="defect"`)
	r = c.Post("/p/VAQ/tags/"+ids["bug"]+"/delete", nil)
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "Tag deleted.")
	contains(t, c.Get("/activity").Body, "renamed tag bug to defect in VAQ (1 ticket)", "deleted tag defect from VAQ (1 ticket)")
	if rows, _ := e.Deps.R.ListProjectTags(bg, p.ID); len(rows) != 1 || rows[0].Name != "ui" {
		t.Fatalf("%+v", rows)
	}
}

// Renaming onto a name the project already uses asks first: the page comes
// back with a merge button (behind the confirm dialog), and only that
// merges.
func TestTagRenameOntoExistingAsksToMerge(t *testing.T) {
	e := webtest.New(t)
	p, ids := tagged(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/tags/"+ids["bug"]+"/rename", url.Values{"name": {"UI"}})
	if r.Status != 422 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "VAQ already has the tag ui.", `value="UI"`)
	contains(t, r.Body, `name="merge" value="1"`)
	merge := formWithConfirm(r.Body, "/p/VAQ/tags/"+ids["bug"]+"/rename")
	contains(t, merge, `data-confirm="Merge bug into ui? Its 1 ticket in VAQ will be tagged ui instead."`)
	if rows, _ := e.Deps.R.ListProjectTags(bg, p.ID); len(rows) != 2 {
		t.Fatal("merged before confirming")
	}
	r = c.Post("/p/VAQ/tags/"+ids["bug"]+"/rename", url.Values{"name": {"UI"}, "merge": {"1"}})
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "Tags merged.", "2 tickets")
	if rows, _ := e.Deps.R.ListProjectTags(bg, p.ID); len(rows) != 1 || rows[0].Name != "ui" || rows[0].Tickets != 2 {
		t.Fatalf("%+v", rows)
	}
}

func TestTagRefusals(t *testing.T) {
	e := webtest.New(t)
	_, ids := tagged(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/tags/"+ids["bug"]+"/rename", url.Values{"name": {" "}})
	if r.Status != 422 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "Enter a name.")
	// Another project's tag id under this project's URL is not found.
	if _, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	r = c.Post("/p/OTH/tags/"+ids["bug"]+"/delete", nil)
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "That no longer exists.")
}
