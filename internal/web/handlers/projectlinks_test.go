package handlers_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// projectLinkWorld is VAQ, plus OTH and APP, with no links yet.
func projectLinkWorld(t *testing.T) (*webtest.Env, gen.Project, gen.Project) {
	t.Helper()
	e := webtest.New(t)
	p := project(t, e)
	oth, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "APP", Name: "Mobile app"}); err != nil {
		t.Fatal(err)
	}
	return e, p, oth
}

// VAQ-119: links through the API, readable on the project's answer (and
// the list's) so agents see them, one event each.
func TestAPIProjectLinks(t *testing.T) {
	e, _, _ := projectLinkWorld(t)
	tok := e.Token(t)
	c := e.Client()
	seq := seqNow(t, e)

	r := c.API("POST", "/api/v1/projects/app/links", tok, map[string]string{"kind": "depends on", "project": "vaq"})
	var l api.ProjectLink
	decode(t, r.Body, &l)
	if r.Status != 201 || l.ID == "" || l.Kind != "depends on" || l.Direction != "out" || l.Key != "VAQ" || l.Name != "Vaquero" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, core.EvProjectLinked, "Owner")
	if r := c.API("POST", "/api/v1/projects/VAQ/links", tok, map[string]string{"kind": "relates", "project": "OTH"}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, core.EvProjectLinked, "Owner")

	refused(t, c.API("POST", "/api/v1/projects/VAQ/links", tok, map[string]string{"kind": "relates", "project": "VAQ"}), "project", "A project can't link to itself.")
	refused(t, c.API("POST", "/api/v1/projects/OTH/links", tok, map[string]string{"kind": "relates", "project": "VAQ"}), "project", "OTH and VAQ are already related.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/links", tok, map[string]string{"kind": "depends on", "project": "APP"}), "project", "That would make VAQ depend on itself: APP already depends on VAQ.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/links", tok, map[string]string{"kind": "blocks", "project": "APP"}), "kind", "Choose depends on or relates.")
	if r := c.API("POST", "/api/v1/projects/NOPE/links", tok, map[string]string{"kind": "relates", "project": "VAQ"}); r.Status != 404 {
		t.Fatalf("unknown project: %d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/projects/VAQ/links", tok, map[string]string{"kind": "relates", "to": "OTH"}); r.Status != 400 {
		t.Fatalf("unknown field: %d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)

	var p api.Project
	decode(t, c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body, &p)
	if len(p.Links) != 2 || p.Links[0].Kind != "depends on" || p.Links[0].Direction != "in" || p.Links[0].Key != "APP" || p.Links[0].Name != "Mobile app" ||
		p.Links[1].Kind != "relates" || p.Links[1].Direction != "out" || p.Links[1].Key != "OTH" || p.Links[0].ID != l.ID {
		t.Fatalf("%+v", p.Links)
	}
	var ps []api.Project
	decode(t, c.API("GET", "/api/v1/projects", tok, nil).Body, &ps)
	for _, x := range ps {
		if x.Key == "APP" && (len(x.Links) != 1 || x.Links[0].Key != "VAQ" || x.Links[0].Direction != "out") {
			t.Fatalf("list: %+v", x.Links)
		}
	}
	// A project with no links answers an empty list, not null.
	var raw map[string]any
	decode(t, c.API("POST", "/api/v1/projects", tok, map[string]string{"key": "NEW", "name": "New"}).Body, &raw)
	if ls, ok := raw["links"].([]any); !ok || len(ls) != 0 {
		t.Fatalf("%v", raw["links"])
	}
	seq = seqNow(t, e)

	// Removed from either end; another project's link is a 404.
	if r := c.API("DELETE", "/api/v1/projects/OTH/links/"+l.ID, tok, nil); r.Status != 404 {
		t.Fatalf("through another project: %d %s", r.Status, r.Body)
	}
	if r := c.API("DELETE", "/api/v1/projects/vaq/links/"+l.ID, tok, nil); r.Status != 204 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, core.EvProjectUnlinked, "Owner")
	if r := c.API("DELETE", "/api/v1/projects/VAQ/links/"+l.ID, tok, nil); r.Status != 404 {
		t.Fatalf("again: %d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)
}

// N-R3: the settings page lists the project's links and adds and removes
// them, removal behind the confirm dialog.
func TestProjectLinkSettings(t *testing.T) {
	e, _, _ := projectLinkWorld(t)
	c := e.Client()
	page := c.Get("/p/VAQ/settings").Body
	contains(t, page, `id="links"`, "No links yet.", `value="OTH"`, `value="APP"`, "Add link")
	if strings.Contains(page, `<option value="VAQ"`) {
		t.Fatal("the project is offered as its own link")
	}

	r := c.Post("/p/VAQ/links", url.Values{"kind": {"depends on"}, "project": {"OTH"}})
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page = c.Follow(r).Body
	contains(t, page, "Link added.", "Depends on")
	links, err := e.Deps.R.ListAllProjectLinks(bg)
	if err != nil || len(links) != 1 {
		t.Fatalf("%v %v", links, err)
	}
	id := links[0].ID
	f := formWithConfirm(page, "/p/VAQ/links/"+id+"/delete")
	if !strings.Contains(f, `method="post"`) || !strings.Contains(f, "Remove the link between VAQ and OTH?") {
		t.Fatalf("remove form: %q", f)
	}
	// The other end shows it too, as used by.
	contains(t, c.Get("/p/OTH/settings").Body, "Used by", "/p/OTH/links/"+id+"/delete")

	// A refusal re-renders the form with its message and the choice kept.
	r = c.Post("/p/OTH/links", url.Values{"kind": {"depends on"}, "project": {"VAQ"}})
	if r.Status != 422 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, r.Body, "That would make OTH depend on itself: VAQ already depends on OTH.")

	// Removal through another project's URL is refused.
	r = c.Post("/p/APP/links/"+id+"/delete", nil)
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "That no longer exists.")
	r = c.Post("/p/OTH/links/"+id+"/delete", nil)
	if r.Status != 303 || r.Location != "/p/OTH/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Link removed.", "No links yet.")
}

// N-R3: the projects page shows "depends on X · used by Y" on cards and
// list rows.
func TestProjectsPageShowsLinks(t *testing.T) {
	e, p, oth := projectLinkWorld(t)
	app, err := e.Deps.R.GetProjectByKey(bg, "APP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AddProjectLink(bg, e.Deps.Actor.ID, app.ID, "depends on", "VAQ"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AddProjectLink(bg, e.Deps.Actor.ID, p.ID, "depends on", "OTH"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AddProjectLink(bg, e.Deps.Actor.ID, oth.ID, "relates", "APP"); err != nil {
		t.Fatal(err)
	}
	text := func(s string) string {
		s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
		return strings.Join(strings.Fields(s), " ")
	}
	for _, view := range []struct{ query, item string }{{"cards", "data-project-card"}, {"list", "data-project-row"}} {
		body := e.Client().Get("/?view=" + view.query).Body
		for key, want := range map[string]string{
			"VAQ": "Depends on OTH · Used by APP",
			"APP": "Depends on VAQ · Relates to OTH",
			"OTH": "Used by VAQ · Relates to APP",
		} {
			item := regexp.MustCompile(`(?s)<li[^>]*` + view.item + `="` + key + `".*?</li>`).FindString(body)
			links := regexp.MustCompile(`(?s)<p[^>]*data-project-links.*?</p>`).FindString(item)
			if got := text(links); got != want {
				t.Errorf("%s %s: %q, want %q", view.query, key, got, want)
			}
			if !strings.Contains(links, `href="/p/`) {
				t.Errorf("%s %s: the keys aren't links: %s", view.query, key, links)
			}
		}
	}
}
