package handlers_test

import (
	"net/url"
	"os"
	"regexp"
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
	contains(t, r.Body, "VAQ already has the tag ui.", `value="UI"`, "data-event-seq=")
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

// Tag changes show in the history of each ticket they changed (VAQ-134),
// and the help says so.
func TestHelpSaysTagChangesShowInTicketHistory(t *testing.T) {
	b, err := os.ReadFile("../../help/articles/projects-and-tickets.md")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`Renaming, merging or deleting a tag[^\n]*history of each ticket it changed`).Match(b) {
		t.Error("the help doesn't say tag changes show in each changed ticket's history")
	}
}

// VAQ-134: a rename, a merge and a delete each show a line in the history
// of every ticket they changed, and only those; the Activity page keeps
// its project-level sentence.
func TestTagChangesInTicketHistory(t *testing.T) {
	e := webtest.New(t)
	p, ids := tagged(t, e)
	by := e.Deps.Actor.ID
	if err := e.Deps.Core.RenameTag(bg, by, p.ID, ids["bug"], "defect"); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.Deps.R.ListProjectTags(bg, p.ID)
	for _, r := range rows {
		ids[r.Name] = r.ID
	}
	if err := e.Deps.Core.MergeTag(bg, by, p.ID, ids["defect"], ids["ui"]); err != nil {
		t.Fatal(err)
	}
	three := ticket(t, e, p, core.TicketInput{Title: "T", Tags: []string{"wontfix"}})
	rows, _ = e.Deps.R.ListProjectTags(bg, p.ID)
	for _, r := range rows {
		ids[r.Name] = r.ID
	}
	if err := e.Deps.Core.DeleteTag(bg, by, p.ID, ids["wontfix"]); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	contains(t, c.Get("/t/VAQ-1").Body, "renamed tag bug → defect", "merged tag defect into ui")
	lacks(t, c.Get("/t/VAQ-2").Body, "renamed tag", "merged tag", "deleted tag")
	contains(t, c.Get("/t/"+core.TicketKey{Project: "VAQ", Number: three.Number}.String()).Body, "deleted tag wontfix")
	lacks(t, c.Get("/t/VAQ-1").Body, "deleted tag")
	contains(t, c.Get("/activity").Body, "renamed tag bug to defect in VAQ (1 ticket)", "merged tag defect into ui in VAQ (1 ticket)",
		"deleted tag wontfix from VAQ (1 ticket)")

	// A tag event from before ticket_ids were recorded names no ticket, so
	// it shows on no ticket's history (and breaks nothing).
	if _, err := e.Deps.Core.DB.Exec(`UPDATE events SET payload = json_remove(payload, '$.ticket_ids') WHERE kind = 'tag.renamed'`); err != nil {
		t.Fatal(err)
	}
	page := c.Get("/t/VAQ-1")
	if page.Status != 200 {
		t.Fatal(page.Status)
	}
	lacks(t, page.Body, "renamed tag")
	contains(t, page.Body, "merged tag defect into ui")
}

// VAQ-134: a ?tag= filter naming no tag in the project (a saved link from
// before a rename) says so on Focus, the board and the list, with a link
// that clears only the tag, instead of showing empty lanes.
func TestFilterNamingNoTag(t *testing.T) {
	e := webtest.New(t)
	tagged(t, e)
	c := e.Client()
	clear := func(href string) *regexp.Regexp {
		return regexp.MustCompile(`href="` + regexp.QuoteMeta(href) + `"[^>]*>Clear the tag filter<`)
	}
	for _, tc := range []struct{ path, clear, live string }{
		{"/p/VAQ?tag=nope", "/p/VAQ", `id="focus"`},
		{"/p/VAQ/board?tag=nope", "/p/VAQ/board", `id="board"`},
		{"/p/VAQ/list?tag=nope", "/p/VAQ/list", `id="list"`},
		{"/p/VAQ/board?tag=nope&mine=1", "/p/VAQ/board?mine=1", `id="board"`},
	} {
		r := c.Get(tc.path)
		if r.Status != 200 {
			t.Fatalf("%s: %d", tc.path, r.Status)
		}
		contains(t, r.Body, "No tag nope in this project.")
		if !clear(tc.clear).MatchString(r.Body) {
			t.Errorf("%s: no link to %s", tc.path, tc.clear)
		}
		lacks(t, r.Body, tc.live)
	}
	// A tag matches in any case; an escaped name stays text.
	lacks(t, c.Get("/p/VAQ/board?tag=BUG").Body, "in this project.")
	contains(t, c.Get("/p/VAQ/board?tag=%3Cb%3E").Body, "No tag &lt;b&gt; in this project.")
}
