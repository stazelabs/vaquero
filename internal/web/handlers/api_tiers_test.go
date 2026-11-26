package handlers_test

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestAPITicketTierRoundTrip(t *testing.T) {
	_, c, tok := apiWorld(t)
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Tier != "" || tk.Model != "" {
		t.Fatalf("unset: %+v", tk)
	}
	if body := c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body; strings.Contains(body, `"tier"`) || strings.Contains(body, `"model"`) {
		t.Fatalf("unset tier and model are sent: %s", body)
	}

	r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"tier": "heavy", "model": "big-model", "version": tk.Version})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var got api.Ticket
	decode(t, r.Body, &got)
	if got.Tier != "heavy" || got.Model != "big-model" || got.Version != tk.Version+1 {
		t.Fatalf("%+v", got)
	}
	decode(t, c.API("GET", "/api/v1/tickets/vaq-1", tok, nil).Body, &got)
	if got.Tier != "heavy" || got.Model != "big-model" {
		t.Fatalf("read back: %+v", got)
	}

	r = c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"tier": "huge"})
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Fields["tier"] != "Choose design, light, standard or heavy." {
		t.Fatalf("%d %+v", r.Status, e)
	}
	r = c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"tier": ""})
	got = api.Ticket{}
	decode(t, r.Body, &got)
	if r.Status != 200 || got.Tier != "" || got.Model != "big-model" {
		t.Fatalf("clear: %d %+v", r.Status, got)
	}

	r = c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{"title": "Spec work", "tier": "design"})
	got = api.Ticket{}
	decode(t, r.Body, &got)
	if r.Status != 201 || got.Tier != "design" {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
}

func TestAPIProjectWorkflowAndTierMap(t *testing.T) {
	_, c, tok := apiWorld(t)
	var p api.Project
	decode(t, c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body, &p)
	if p.Workflow || p.TierMap["claude"]["heavy"] != "claude-opus-5-5" {
		t.Fatalf("default: %+v", p)
	}
	if body := c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body; !strings.Contains(body, `"workflow": false`) {
		t.Fatalf("workflow off is left out: %s", body)
	}
	r := c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"workflow": true, "tier_map": map[string]any{"claude": map[string]string{"heavy": "big"}}})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	p = api.Project{}
	decode(t, r.Body, &p)
	if !p.Workflow || p.TierMap["claude"]["heavy"] != "big" || p.TierMap["codex"] != nil || p.Name != "Vaquero" {
		t.Fatalf("%+v", p)
	}
	var ps []api.Project
	decode(t, c.API("GET", "/api/v1/projects", tok, nil).Body, &ps)
	if !ps[0].Workflow || ps[0].TierMap["claude"]["heavy"] != "big" {
		t.Fatalf("list: %+v", ps)
	}
	r = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"tier_map": nil})
	p = api.Project{}
	decode(t, r.Body, &p)
	if r.Status != 200 || p.TierMap["claude"]["heavy"] != "claude-opus-5-5" || !p.Workflow {
		t.Fatalf("reset: %d %+v", r.Status, p)
	}
	r = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"tier_map": map[string]any{"claude": map[string]string{"huge": "x"}}})
	var e apiErrBody
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Fields["tier_map"] != "Use only the tiers design, light, standard and heavy." {
		t.Fatalf("%d %+v", r.Status, e)
	}
	r = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"tier_map": "claude"})
	decode(t, r.Body, &e)
	if r.Status != 422 || e.Error.Fields["tier_map"] == "" {
		t.Fatalf("a string map: %d %s", r.Status, r.Body)
	}
}

func TestMCPUpdateTicketTier(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "planner")
	res, text := mcpCall(t, cs, "update_ticket", map[string]any{"key": "VAQ-1", "tier": "light", "model": "small-model"})
	if res.IsError {
		t.Fatalf("%s", text)
	}
	var tk api.Ticket
	if err := json.Unmarshal([]byte(text), &tk); err != nil || tk.Tier != "light" || tk.Model != "small-model" {
		t.Fatalf("%v %s", err, text)
	}
	res, text = mcpCall(t, cs, "create_ticket", map[string]any{"project": "VAQ", "title": "Spec", "tier": "design", "model": "m-2"})
	tk = api.Ticket{}
	if err := json.Unmarshal([]byte(text), &tk); res.IsError || err != nil || tk.Tier != "design" || tk.Model != "m-2" {
		t.Fatalf("create: %v %s", err, text)
	}
	res, text = mcpCall(t, cs, "update_ticket", map[string]any{"key": "VAQ-1", "tier": "huge"})
	if !res.IsError || !strings.Contains(text, "Choose design, light, standard or heavy.") {
		t.Fatalf("%v %s", res.IsError, text)
	}
}

// The ticket form's tier select saves, and an unset tier stays unset.
func TestTicketFormTier(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Work"})
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	contains(t, c.Get("/t/VAQ-1/edit").Body, `name="tier"`, `value="standard"`, `name="model"`)
	form := url.Values{"title": {"Work"}, "status": {tk.StatusID}, "priority": {"0"}, "tier": {"standard"}, "model": {"m-1"}, "version": {"1"}}
	if r := c.Post("/t/VAQ-1", form); r.Status != 303 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	got, _ := e.Deps.R.GetTicket(bg, tk.ID)
	if got.Tier != "standard" || got.Model != "m-1" {
		t.Fatalf("%q %q", got.Tier, got.Model)
	}
	contains(t, c.Get("/t/VAQ-1").Body, "Standard", "m-1")
	form.Set("tier", "huge")
	form.Set("version", "2")
	r := c.Post("/t/VAQ-1", form)
	if r.Status != 422 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, r.Body, "Choose design, light, standard or heavy.")
}

func TestSettingsWorkflow(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	body := c.Get("/p/VAQ/settings").Body
	contains(t, body, `action="/p/VAQ/workflow"`, `name="workflow"`, `name="tier_map"`, "claude-opus-5-5")
	if strings.Contains(body, "Add the triage statuses") {
		t.Fatal("triage statuses offered with the workflow off")
	}

	// The map as shown, saved unchanged, keeps the default.
	shown := core.DefaultTierMap().Indent()
	r := c.Post("/p/VAQ/workflow", url.Values{"workflow": {"on"}, "tier_map": {shown}})
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	got, _ := e.Deps.R.GetProject(bg, p.ID)
	if got.Workflow != 1 || got.TierMap != "" {
		t.Fatalf("%d %q", got.Workflow, got.TierMap)
	}

	r = c.Post("/p/VAQ/workflow", url.Values{"workflow": {"on"}, "tier_map": {`{"claude": `}})
	if r.Status != 422 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, r.Body, "Enter the tier map as JSON", `{&#34;claude&#34;: `, `aria-describedby="f-tier-map-error"`)

	// Unchecked is off.
	c.Post("/p/VAQ/workflow", url.Values{"tier_map": {shown}})
	if got, _ := e.Deps.R.GetProject(bg, p.ID); got.Workflow != 0 {
		t.Fatal("still on")
	}
}

func TestWorkflowOffersTriageStatuses(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, p.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateStatus(bg, e.Deps.Actor.ID, p.ID, "needs design", "backlog"); err != nil {
		t.Fatal(err)
	}
	contains(t, c.Get("/p/VAQ/settings").Body, "Add the triage statuses", `action="/p/VAQ/triage-statuses"`, "Needs decision")

	before := eventCount(t, e)
	r := c.Post("/p/VAQ/triage-statuses", nil)
	if r.Status != 303 || r.Location != "/p/VAQ/settings" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if n := eventCount(t, e); n != before+1 {
		t.Fatalf("%d events, want 1 (one status was missing)", n-before)
	}
	ss := statuses(t, e, p)
	var names []string
	for _, s := range ss {
		names = append(names, s.Name+"/"+s.Category)
	}
	// Both sit at the end of the backlog category, before Todo.
	if got := strings.Join(names, ","); !strings.HasPrefix(got, "Backlog/backlog,needs design/backlog,Needs decision/backlog,Todo/todo") {
		t.Fatalf("%s", got)
	}
	if strings.Contains(c.Get("/p/VAQ/settings").Body, "Add the triage statuses") {
		t.Fatal("still offered")
	}
	// With nothing missing, the post changes nothing.
	c.Post("/p/VAQ/triage-statuses", nil)
	if n := eventCount(t, e); n != before+1 {
		t.Fatalf("again: %d events", n-before)
	}
}

func eventCount(t *testing.T, e *webtest.Env) int64 {
	t.Helper()
	n, err := e.Deps.R.CountEvents(bg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// With the workflow off, the ticket form and page show no tier, and a save
// from the form keeps a tier the API set.
func TestNoTierUIWithWorkflowOff(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Work", Tier: "heavy", Model: "m-1"})
	c := e.Client()
	for _, path := range []string{"/p/VAQ/new", "/t/VAQ-1/edit", "/t/VAQ-1"} {
		body := c.Get(path).Body
		for _, bad := range []string{`name="tier"`, `name="model"`, "data-tier", "m-1", ">Tier<"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s has %q", path, bad)
			}
		}
	}
	form := url.Values{"title": {"Work again"}, "status": {tk.StatusID}, "priority": {"0"}, "version": {"1"}}
	if r := c.Post("/t/VAQ-1", form); r.Status != 303 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got, _ := e.Deps.R.GetTicket(bg, tk.ID); got.Tier != "heavy" || got.Model != "m-1" || got.Title != "Work again" {
		t.Fatalf("%q %q %q", got.Title, got.Tier, got.Model)
	}
}

// The triage post does nothing with the workflow off.
func TestTriageStatusesNeedTheWorkflow(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	before := eventCount(t, e)
	r := e.Client().Post("/p/VAQ/triage-statuses", nil)
	if r.Status != 303 || r.Location != "/p/VAQ/settings" || eventCount(t, e) != before {
		t.Fatalf("%d %q, %d events", r.Status, r.Location, eventCount(t, e)-before)
	}
}

// A stored empty map is sent as {}, not left out.
func TestAPIEmptyTierMap(t *testing.T) {
	_, c, tok := apiWorld(t)
	if r := c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"tier_map": map[string]any{}}); r.Status != 200 || !strings.Contains(r.Body, `"tier_map": {}`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// A project with no repo has no repo key; with one, the remote's URL comes
// without its credentials.
func TestProjectAnswerRepo(t *testing.T) {
	_, c, tok := apiWorld(t)
	if body := c.API("GET", "/api/v1/projects/VAQ", tok, nil).Body; strings.Contains(body, `"repo"`) {
		t.Fatalf("no repo: %s", body)
	}
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "first")
	r.Git("remote", "add", "origin", "https://user:secret@example.com/x.git")
	res := c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"repo_path": r.Dir, "fetch_minutes": 10})
	var p api.Project
	decode(t, res.Body, &p)
	if res.Status != 200 || p.Repo == nil || p.Repo.RemoteURL != "https://example.com/x.git" || p.Repo.MainBranch != "main" || p.Repo.FetchMinutes != 10 || !p.Repo.Available {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if strings.Contains(res.Body, "secret") {
		t.Fatal("credentials shown")
	}
	var ps []api.Project
	decode(t, c.API("GET", "/api/v1/projects", tok, nil).Body, &ps)
	if ps[0].Repo == nil || ps[0].Repo.Path == "" {
		t.Fatalf("list: %+v", ps[0])
	}
	res = c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"fetch_minutes": 2})
	var e apiErrBody
	decode(t, res.Body, &e)
	if res.Status != 422 || e.Error.Fields["fetch_minutes"] != "Fetch every 5 to 1440 minutes, or 0 for never." {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
}

func TestSettingsRepoRoundTrip(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	c := e.Client()
	body := c.Get("/p/VAQ/settings").Body
	contains(t, body, `action="/p/VAQ/repo"`, `name="repo_path"`, `name="main_branch"`, `name="remote"`, `name="fetch_minutes"`)
	r := gittest.Repo(t)
	r.WriteCommit("a.md", "a", "first")
	r.Git("remote", "add", "origin", "https://user:secret@example.com/x.git")
	res := c.Post("/p/VAQ/repo", url.Values{"repo_path": {r.Dir}, "main_branch": {"main"}, "remote": {"origin"}, "fetch_minutes": {"15"}})
	if res.Status != 303 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	got, _ := e.Deps.R.GetProject(bg, p.ID)
	if got.RepoPath == "" || got.Remote != "origin" || got.FetchMinutes != 15 {
		t.Fatalf("%+v", got)
	}
	page := c.Get("/p/VAQ/settings").Body
	contains(t, page, "https://example.com/x.git", `value="15"`)
	if strings.Contains(page, "secret") {
		t.Fatal("credentials shown")
	}
	res = c.Post("/p/VAQ/repo", url.Values{"repo_path": {r.Dir}, "main_branch": {"trunk"}, "remote": {"origin"}, "fetch_minutes": {"15"}})
	if res.Status != 422 {
		t.Fatalf("%d", res.Status)
	}
	contains(t, res.Body, "The repo has no branch trunk.", `value="trunk"`)
	res = c.Post("/p/VAQ/repo", url.Values{"repo_path": {r.Dir}, "main_branch": {"main"}, "fetch_minutes": {"abc"}})
	if res.Status != 422 {
		t.Fatalf("%d", res.Status)
	}
	contains(t, res.Body, "Fetch every 5 to 1440 minutes, or 0 for never.")
}

func TestSettingsRepoWithoutGit(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	e.Deps.Git, e.Deps.Core.Git = nil, nil
	e.Rebuild()
	contains(t, e.Client().Get("/p/VAQ/settings").Body, "installed, so repository features are off.", "disabled")
}

// Below git 2.31 the repository features are off and say why.
func TestSettingsRepoWithOldGit(t *testing.T) {
	e := webtest.New(t)
	project(t, e)
	old := gitread.NewWithVersion("git version 2.30.2")
	e.Deps.Git, e.Deps.Core.Git = old, old
	e.Rebuild()
	contains(t, e.Client().Get("/p/VAQ/settings").Body, "Git 2.31 or later is needed, so repository features are off.", "disabled")
}
