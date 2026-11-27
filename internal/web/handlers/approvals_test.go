package handlers_test

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// approvalWorld is a workflow-on project with a todo ticket, VAQ-1.
func approvalWorld(t *testing.T) (*webtest.Env, *webtest.Client, gen.Ticket) {
	t.Helper()
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Build it", Tier: "light"})
	workflowOn(t, e)
	return e, e.Client(), tk
}

func openApprovals(t *testing.T, e *webtest.Env) int {
	return dbCount(t, e, `SELECT count(*) FROM decisions WHERE state = 'open' AND kind = 'approval'`)
}

func TestTicketPageShowsReadinessAndAsks(t *testing.T) {
	e, c, _ := approvalWorld(t)
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "data-readiness", "Ask for plan approval.", `action="/t/VAQ-1/approval/ask"`, "Ask for approval")

	r := c.Post("/t/VAQ-1/approval/ask", nil)
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	if n := openApprovals(t, e); n != 1 {
		t.Fatalf("%d open approvals", n)
	}
	page = c.Follow(r).Body
	contains(t, page, "Approve the plan for VAQ-1", "data-approval", `action="/decisions/`, `value="Approve"`, `value="Request changes"`, `name="note"`)
	lacks(t, page, `action="/t/VAQ-1/approval/ask"`)

	// A second ask shows the open one and writes nothing.
	r = c.Post("/t/VAQ-1/approval/ask", nil)
	if n := openApprovals(t, e); n != 1 || r.Status != 303 {
		t.Fatalf("%d open approvals, status %d", n, r.Status)
	}
	contains(t, c.Follow(r).Body, "already waiting; answer it below, or use Ask again.")
}

func TestAnswerApprovalMakesTicketReady(t *testing.T) {
	e, c, _ := approvalWorld(t)
	c.Post("/t/VAQ-1/approval/ask", nil)
	id := openApprovalID(t, e)
	r := c.Post("/decisions/"+id+"/answer", url.Values{"option": {"Approve"}, "note": {"ok"}, "ticket": {"VAQ-1"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page := c.Follow(r).Body
	contains(t, page, "Answered", "data-readiness", "Ready")
	lacks(t, page, "Ask for plan approval.", "data-approval")
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE answered_by IS NOT NULL AND relayed_by IS NULL AND answer_note = 'ok'`); n != 1 {
		t.Fatalf("answer not direct: %d", n)
	}
}

func TestAnswerRefusalIsAToast(t *testing.T) {
	e, c, tk := approvalWorld(t)
	c.Post("/t/VAQ-1/approval/ask", nil)
	id := openApprovalID(t, e)
	title := "Changed"
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{Title: &title}, 0); err != nil {
		t.Fatal(err)
	}
	r := c.Post("/decisions/"+id+"/answer", url.Values{"option": {"Approve"}, "ticket": {"VAQ-1"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "changed since this was asked; ask again.")
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE state = 'answered'`); n != 0 {
		t.Fatal("answered")
	}
}

func TestAskOnTheEpicWhenReadinessSaysSo(t *testing.T) {
	e, c, epic := approvalWorld(t)
	p := mustProject(t, e)
	ticket(t, e, p, core.TicketInput{Title: "Child", ParentID: epic.ID, Tier: "light"})
	if _, err := e.Deps.Core.SavePlan(bg, e.Deps.Actor.ID, epic.ID, "The plan.", 0); err != nil {
		t.Fatal(err)
	}
	approvePlan(t, e, epic.ID)
	title := "Epic, edited"
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, epic.ID, core.TicketPatch{Title: &title}, 0); err != nil {
		t.Fatal(err)
	}
	contains(t, c.Get("/t/VAQ-2").Body, "Its epic changed since approval", "Ask for approval")
	r := c.Post("/t/VAQ-2/approval/ask", nil)
	if r.Status != 303 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, c.Follow(r).Body, "Approve the plan for VAQ-1", "data-approval")
	if n := dbCount(t, e, `SELECT count(*) FROM decision_tickets dt JOIN decisions d ON d.id = dt.decision_id WHERE d.state = 'open' AND dt.ticket_id = ?`, epic.ID); n != 1 {
		t.Fatalf("asked on the epic: %d", n)
	}
}

func TestTicketPageWorkflowOffHasNoReadiness(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Plain"})
	lacks(t, e.Client().Get("/t/VAQ-1").Body, "data-readiness", `id="readiness"`, "data-approval", "approval/ask")
	// The routes refuse with the workflow off.
	c := e.Client()
	r := c.Post("/t/VAQ-1/approval/ask", nil)
	contains(t, c.Follow(r).Body, "agent workflow")
	if n := openApprovals(t, e); n != 0 {
		t.Fatal("asked with the workflow off")
	}
}

func TestApprovalFormsRefuseCrossSite(t *testing.T) {
	e, c, _ := approvalWorld(t)
	for _, path := range []string{"/t/VAQ-1/approval/ask", "/decisions/anything/answer"} {
		if r := c.PostCrossSite(path, url.Values{"option": {"Approve"}}); r.Status != 403 {
			t.Errorf("%s: %d, want 403", path, r.Status)
		}
	}
	if n := openApprovals(t, e); n != 0 {
		t.Fatal("asked")
	}
}

func openApprovalID(t *testing.T, e *webtest.Env) string {
	t.Helper()
	var id string
	if err := e.Deps.DB.QueryRow(`SELECT id FROM decisions WHERE state = 'open' AND kind = 'approval'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Ruling R-Q14: an open approval asked before the ticket changed can't be
// approved; Ask again withdraws it and asks a new one, which approves.
func TestAskAgainReplacesAStaleOpenApproval(t *testing.T) {
	e, c, tk := approvalWorld(t)
	c.Post("/t/VAQ-1/approval/ask", nil)
	old := openApprovalID(t, e)
	body := "Now with rails."
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		t.Fatal(err)
	}
	r := c.Post("/decisions/"+old+"/answer", url.Values{"option": {"Approve"}, "ticket": {"VAQ-1"}})
	page := c.Follow(r).Body
	contains(t, page, "changed since this was asked; ask again. Use Ask again.", "data-approval",
		`action="/t/VAQ-1/approval/ask-again"`, "Ask again")
	lacks(t, page, `action="/t/VAQ-1/approval/ask"`)

	r = c.Post("/t/VAQ-1/approval/ask-again", nil)
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Asked again for approval of the plan for VAQ-1.", "data-approval")
	if n := openApprovals(t, e); n != 1 {
		t.Fatalf("%d open approvals", n)
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'withdrawn'`, old); n != 1 {
		t.Fatal("the stale approval wasn't withdrawn")
	}
	id := openApprovalID(t, e)
	r = c.Post("/decisions/"+id+"/answer", url.Values{"option": {"Approve"}, "ticket": {"VAQ-1"}})
	page = c.Follow(r).Body
	contains(t, page, "Answered", "Ready")
	lacks(t, page, "data-approval")

	// With nothing waiting, Ask again says so and asks nothing.
	r = c.Post("/t/VAQ-1/approval/ask-again", nil)
	contains(t, c.Follow(r).Body, "No plan approval for VAQ-1 is waiting.")
	if n := openApprovals(t, e); n != 0 {
		t.Fatalf("%d open approvals", n)
	}
}

// Ruling R-Q15: a ticket's own open plan approval shows whatever its
// status; the Ready line, the reason and the Ask button are for todos.
func TestOpenApprovalShowsOnABacklogTicket(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	workflowOn(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Later", StatusID: statusID(t, e, "Backlog")})
	c := e.Client()
	lacks(t, c.Get("/t/VAQ-1").Body, "data-readiness", "data-approval")
	if _, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: p.ID, TicketIDs: []string{tk.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan for VAQ-1?",
	}); err != nil {
		t.Fatal(err)
	}
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "data-approval", "Approve the plan for VAQ-1?", `value="Approve"`, `action="/t/VAQ-1/approval/ask-again"`)
	lacks(t, page, "Ask for plan approval.", `action="/t/VAQ-1/approval/ask"`, ">Ready<")
	// Asking from the page is for todos alone: readiness says nothing to ask.
	r := c.Post("/t/VAQ-1/approval/ask", nil)
	contains(t, c.Follow(r).Body, "VAQ-1 has nothing to ask for.")
	if n := openApprovals(t, e); n != 1 {
		t.Fatalf("%d open approvals", n)
	}
}

// Ruling R-Q16: files that can't be checked because the repo settings
// changed offer Ask again on the page.
func TestRepoChangedOffersAsk(t *testing.T) {
	e, c, tk := approvalWorld(t)
	r := gittest.Repo(t)
	r.WriteCommit("docs/a.md", "# A\n", "a")
	setRepo(t, e, r.Dir)
	if _, err := e.Deps.Core.AddRef(bg, e.Deps.Actor.ID, tk.ID, core.RefInput{Kind: "spec", Target: "docs/a.md"}); err != nil {
		t.Fatal(err)
	}
	approvePlan(t, e, tk.ID)
	contains(t, c.Get("/t/VAQ-1").Body, "Ready")
	clone := gittest.Repo(t)
	clone.WriteCommit("docs/a.md", "# A\n", "a")
	setRepo(t, e, clone.Dir)
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "Can&#39;t check its files: the repository settings changed since approval.", `action="/t/VAQ-1/approval/ask"`)
	resp := c.Post("/t/VAQ-1/approval/ask", nil)
	contains(t, c.Follow(resp).Body, "Asked for approval of the plan for VAQ-1.")
}

// VAQ-195: a move leaves the ticket's open decisions in the old project;
// the ticket page in the new project shows only its own project's.
func TestApprovalsAreTheCurrentProjectsAfterAMove(t *testing.T) {
	e, c, tk := approvalWorld(t)
	other, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "OTH", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, other.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
	c.Post("/t/VAQ-1/approval/ask", nil)
	old := openApprovalID(t, e)
	moved, err := e.Deps.Core.MoveTicketToProject(bg, e.Deps.Actor.ID, tk.ID, other.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := "OTH-" + strconv.FormatInt(moved.Number, 10)

	page := c.Get("/t/" + key).Body
	lacks(t, page, "data-approval", `action="/decisions/`)
	contains(t, page, `action="/t/`+key+`/approval/ask"`)

	r := c.Post("/t/"+key+"/approval/ask", nil)
	contains(t, c.Follow(r).Body, "data-approval")
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'open'`, old); n != 1 {
		t.Fatal("the old project's decision was touched")
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE state = 'open' AND project_id = ?`, other.ID); n != 1 {
		t.Fatalf("%d open in the new project", n)
	}

	r = c.Post("/t/"+key+"/approval/ask-again", nil)
	contains(t, c.Follow(r).Body, "Asked again for approval of the plan for "+key+".")
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'open'`, old); n != 1 {
		t.Fatal("Ask again withdrew the old project's decision")
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE state = 'open' AND project_id = ?`, other.ID); n != 1 {
		t.Fatalf("%d open in the new project", n)
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE state = 'withdrawn' AND project_id = ?`, other.ID); n != 1 {
		t.Fatalf("%d withdrawn in the new project", n)
	}
}
