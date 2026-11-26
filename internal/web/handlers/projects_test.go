package handlers_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

var bg = context.Background()

func contains(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("page lacks %q", w)
		}
	}
}

// project makes project VAQ through core.
func project(t *testing.T, e *webtest.Env) gen.Project {
	t.Helper()
	p, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func statuses(t *testing.T, e *webtest.Env, p gen.Project) []gen.Status {
	t.Helper()
	ss, err := e.Deps.R.ListStatuses(bg, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func TestEmptyHome(t *testing.T) {
	r := webtest.New(t).Client().Get("/")
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "No projects yet.", `href="/projects/new"`)
}

func TestCreateProjectFlow(t *testing.T) {
	c := webtest.New(t).Client()
	if r := c.Get("/projects/new"); r.Status != 200 {
		t.Fatal(r.Status)
	}
	r := c.Post("/projects", url.Values{"key": {"vaq"}, "name": {"Vaquero"}, "description": {"Herds the chaos."}})
	if r.Status != 303 || r.Location != "/p/VAQ" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page := c.Follow(r)
	contains(t, page.Body, "Project created.", "Vaquero", `id="focus"`, "Nothing ready.")
	contains(t, c.Get("/p/VAQ/board").Body, "Backlog", "Todo", "In progress", "In review", "Done", "Cancelled")
	home := c.Get("/")
	contains(t, home.Body, "VAQ", "Vaquero", `href="/p/VAQ"`)
}

func TestCreateProjectInvalidKeepsInput(t *testing.T) {
	r := webtest.New(t).Client().Post("/projects", url.Values{"key": {"9x"}, "name": {"My name"}})
	if r.Status != 422 {
		t.Fatal(r.Status)
	}
	contains(t, r.Body, "Use 2 to 10 capital letters or digits, starting with a letter.", `value="My name"`, `value="9X"`)
}

func TestProjectKeyCaseRedirect(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	r := e.Client().Get("/p/vaq/settings")
	if r.Status != 301 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
}

func TestProjectKeyCaseRedirectKeepsQuery(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	r := e.Client().Get("/p/vaq/new?parent=VAQ-1")
	if r.Status != 301 || r.Location != "/p/VAQ/new?parent=VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
}

func TestUnknownProjectRedirects(t *testing.T) {
	c := webtest.New(t).Client()
	r := c.Get("/p/NOPE")
	if r.Status != 303 || r.Location != "/" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "No project NOPE.")
}

func TestProjectUpdate(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ", url.Values{"name": {"Renamed"}, "description": {""}})
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "Project saved.", "Renamed")
	if r := c.Post("/p/VAQ", url.Values{"name": {" "}}); r.Status != 422 {
		t.Fatal(r.Status)
	}
}

func TestStatusSettings(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/statuses", url.Values{"name": {"Blocked"}, "category": {"in_progress"}})
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Status added.", "Blocked")

	ss := statuses(t, e, p)
	c.Post("/p/VAQ/statuses/"+ss[0].ID+"/rename", url.Values{"name": {"Icebox"}})
	c.Post("/p/VAQ/statuses/"+ss[0].ID+"/down", nil)
	ss = statuses(t, e, p)
	if ss[0].Name != "Todo" || ss[1].Name != "Icebox" {
		t.Fatalf("%s, %s", ss[0].Name, ss[1].Name)
	}

	// Deleting a status in use is refused with a toast.
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "x"}); err != nil {
		t.Fatal(err)
	}
	r = c.Post("/p/VAQ/statuses/"+ss[0].ID+"/delete", nil)
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	contains(t, c.Follow(r).Body, "Move this status&#39;s tickets first.")
}

func TestMilestoneSettings(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/milestones", url.Values{"name": {"Launch"}, "due_on": {"2026-12-31"}})
	contains(t, c.Follow(r).Body, "Milestone added.", "Launch", "Dec 31, 2026")
	ms, _ := e.Deps.R.ListMilestones(bg, p.ID)
	r = c.Post("/p/VAQ/milestones/"+ms[0].ID, url.Values{"name": {"Launch"}, "due_on": {"soon"}})
	contains(t, r.Body, "Use a date like 2026-12-31.")
	c.Post("/p/VAQ/milestones/"+ms[0].ID+"/close", nil)
	if m, _ := e.Deps.R.GetMilestone(bg, ms[0].ID); !m.ClosedAt.Valid {
		t.Fatal("not closed")
	}
	c.Post("/p/VAQ/milestones/"+ms[0].ID+"/reopen", nil)
	if m, _ := e.Deps.R.GetMilestone(bg, ms[0].ID); m.ClosedAt.Valid {
		t.Fatal("not reopened")
	}
}

// A refused status or milestone form re-renders settings with 422, the
// field's message and what was typed.
func TestSettingsFormsKeepTypedValues(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	m, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	ss := statuses(t, e, p)

	r := c.Post("/p/VAQ/statuses", url.Values{"name": {"tODO"}, "category": {"in_review"}})
	if r.Status != 422 {
		t.Fatalf("new status: %d", r.Status)
	}
	contains(t, r.Body, "A status with that name exists.", `value="tODO"`, `value="in_review" selected`)
	// Each message is tied to its field, and the field says it is invalid.
	contains(t, r.Body, `aria-invalid="true" aria-describedby="f-status-name-error"`, `id="f-status-name-error"`)

	r = c.Post("/p/VAQ/statuses/"+ss[0].ID+"/rename", url.Values{"name": {"dOnE"}})
	if r.Status != 422 {
		t.Fatalf("rename: %d", r.Status)
	}
	contains(t, r.Body, "A status with that name exists.", `value="dOnE"`)
	contains(t, r.Body, `aria-describedby="s-`+ss[0].ID+`-error"`, `id="s-`+ss[0].ID+`-error"`)

	r = c.Post("/p/VAQ/milestones", url.Values{"name": {"Beta"}, "due_on": {"soon"}})
	if r.Status != 422 {
		t.Fatalf("new milestone: %d", r.Status)
	}
	contains(t, r.Body, "Use a date like 2026-12-31.", `value="Beta"`, `value="soon"`)

	r = c.Post("/p/VAQ/milestones/"+m.ID, url.Values{"name": {"Launch two"}, "due_on": {"later"}})
	if r.Status != 422 {
		t.Fatalf("edit milestone: %d", r.Status)
	}
	contains(t, r.Body, "Use a date like 2026-12-31.", `value="Launch two"`, `value="later"`)
}

func TestArchiveHidesFromList(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/archive", nil)
	contains(t, c.Follow(r).Body, "Project archived.")
	home := c.Get("/")
	contains(t, home.Body, "Archived")
	c.Post("/p/VAQ/restore", nil)
	if p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ"); p.ArchivedAt.Valid {
		t.Fatal("not restored")
	}
}

func TestProjectPageListsTicketsByStatus(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "Fix the gate", Priority: 2, Tags: []string{"ui"}}); err != nil {
		t.Fatal(err)
	}
	r := e.Client().Get("/p/VAQ/board")
	contains(t, r.Body, "VAQ-1", "Fix the gate", "High", "ui", `href="/t/VAQ-1"`, `href="/p/VAQ/new"`, "No tickets.")
}

func TestKeyChangeFlow(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	c := e.Client()
	contains(t, c.Get("/p/VAQ/settings").Body, `action="/p/VAQ/key"`, "will stop working")
	r := c.Post("/p/VAQ/key", url.Values{"key": {"vq"}})
	if r.Status != 303 || r.Location != "/p/VQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Key changed to VQ.")
	contains(t, c.Follow(c.Get("/t/VAQ-1")).Body, "No ticket VAQ-1.")
	if r := c.Get("/t/VQ-1"); r.Status != 200 {
		t.Fatalf("VQ-1: %d", r.Status)
	}
	// A refusal comes back as a toast on settings.
	r = c.Post("/p/VQ/key", url.Values{"key": {"9"}})
	contains(t, c.Follow(r).Body, "Use 2 to 10 capital letters")
}

func TestDeleteFlow(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "x"})
	c := e.Client()
	contains(t, c.Get("/p/VAQ/settings").Body, `action="/p/VAQ/delete"`, "Type VAQ to delete")
	r := c.Post("/p/VAQ/delete", url.Values{"confirm": {"VAQ"}})
	if r.Status != 303 || r.Location != "/" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Project VAQ deleted. A backup was saved first.")
	contains(t, c.Follow(c.Get("/p/VAQ")).Body, "No project VAQ.")
	entries, err := os.ReadDir(e.Deps.BackupDir)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "vaquero-") {
		t.Fatalf("backups: %v %v", entries, err)
	}
}

func TestDeleteWrongConfirmation(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	r := c.Post("/p/VAQ/delete", url.Values{"confirm": {"nope"}})
	contains(t, c.Follow(r).Body, "Type VAQ to confirm.")
	if _, err := e.Deps.R.GetProjectByKey(bg, "VAQ"); err != nil {
		t.Fatal("deleted anyway")
	}
}

// Review Focus 5.
func TestDeleteWhenSnapshotFails(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.Deps.BackupDir = filepath.Join(blocker, "backups") // under a file: can't be created
	e.Rebuild()
	c := e.Client()
	r := c.Post("/p/VAQ/delete", url.Values{"confirm": {"VAQ"}})
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Couldn&#39;t save a backup in ", ", so nothing was deleted.")
	if _, err := e.Deps.R.GetProjectByKey(bg, "VAQ"); err != nil {
		t.Fatal("deleted without a backup")
	}
}

// An open board or list whose project was deleted or re-keyed elsewhere
// reloads (and shows why) instead of swapping in another page.
func TestLiveRefreshOfMissingProject(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	c := e.Client()
	for _, path := range []string{"/p/GONE/board", "/p/GONE/list"} {
		r := c.GetHX(path)
		if r.Status != 200 || r.Header.Get("HX-Refresh") != "true" {
			t.Errorf("%s: %d %v", path, r.Status, r.Header.Get("HX-Refresh"))
		}
	}
}

// Two deletes in the same second both get their own backup.
func TestTwoDeletesInOneSecond(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	if _, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	for _, key := range []string{"VAQ", "OTH"} {
		r := c.Post("/p/"+key+"/delete", url.Values{"confirm": {key}})
		if r.Status != 303 || r.Location != "/" {
			t.Fatalf("%s: %d %q", key, r.Status, r.Location)
		}
	}
	if entries, _ := os.ReadDir(e.Deps.BackupDir); len(entries) != 2 {
		t.Fatalf("%d backups", len(entries))
	}
}

// Each settings field has its own id, so every label names one field.
func TestSettingsFieldIDsUnique(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	page := e.Client().Get("/p/VAQ/settings").Body
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(` id="([^"]+)"`).FindAllStringSubmatch(page, -1) {
		if seen[m[1]] {
			t.Errorf("id %q is used twice", m[1])
		}
		seen[m[1]] = true
	}
}

// VAQ-19: the settings form edits a milestone's description too.
func TestMilestoneSettingsEditDescription(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	m, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Launch", Description: "The first cut."})
	if err != nil {
		t.Fatal(err)
	}
	page := c.Get("/p/VAQ/settings")
	contains(t, page.Body, `id="m-desc-`+m.ID+`"`, `name="description"`, "The first cut.</textarea>")

	seq := seqNow(t, e)
	r := c.Post("/p/VAQ/milestones/"+m.ID, url.Values{"name": {"Launch"}, "description": {"Board and list."}, "due_on": {""}})
	if r.Status != 303 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, c.Follow(r).Body, "Milestone saved.", "Board and list.</textarea>")
	if got, _ := e.Deps.R.GetMilestone(bg, m.ID); got.Description != "Board and list." {
		t.Fatalf("%q", got.Description)
	}
	oneEvent(t, e, seq, "milestone.updated", "Owner")

	// A refused save keeps the typed description.
	r = c.Post("/p/VAQ/milestones/"+m.ID, url.Values{"name": {"Launch"}, "description": {"Typed, not saved."}, "due_on": {"soon"}})
	if r.Status != 422 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, r.Body, "Typed, not saved.</textarea>")

	// A form without the field (a page from before it) keeps the description.
	c.Post("/p/VAQ/milestones/"+m.ID, url.Values{"name": {"Launch 1"}, "due_on": {""}})
	if got, _ := e.Deps.R.GetMilestone(bg, m.ID); got.Name != "Launch 1" || got.Description != "Board and list." {
		t.Fatalf("%+v", got)
	}
}
