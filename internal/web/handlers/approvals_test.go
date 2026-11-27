package handlers_test

import (
	"net/url"
	"strconv"
	"strings"
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
	contains(t, page, "Approve the plan for VAQ-1", "data-decision", `action="/inbox/`, `value="Approve"`, `value="Request changes"`, `name="note"`)
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
	r := c.Post("/inbox/"+id+"/answer", url.Values{"option": {"Approve"}, "note": {"ok"}, "back": {"/t/VAQ-1"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page := c.Follow(r).Body
	contains(t, page, "Answered", "data-readiness", "Ready")
	lacks(t, page, "Ask for plan approval.", "data-decision")
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
	r := c.Post("/inbox/"+id+"/answer", url.Values{"option": {"Approve"}, "back": {"/t/VAQ-1"}})
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
	contains(t, c.Follow(r).Body, "Approve the plan for VAQ-1", "data-decision")
	if n := dbCount(t, e, `SELECT count(*) FROM decision_tickets dt JOIN decisions d ON d.id = dt.decision_id WHERE d.state = 'open' AND dt.ticket_id = ?`, epic.ID); n != 1 {
		t.Fatalf("asked on the epic: %d", n)
	}
}

func TestTicketPageWorkflowOffHasNoReadiness(t *testing.T) {
	e := webtest.New(t)
	ticket(t, e, project(t, e), core.TicketInput{Title: "Plain"})
	lacks(t, e.Client().Get("/t/VAQ-1").Body, "data-readiness", `id="readiness"`, "data-decision", "approval/ask")
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
	for _, path := range []string{"/t/VAQ-1/approval/ask", "/inbox/anything/answer"} {
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
	r := c.Post("/inbox/"+old+"/answer", url.Values{"option": {"Approve"}, "back": {"/t/VAQ-1"}})
	page := c.Follow(r).Body
	contains(t, page, "changed since this was asked; ask again.", "data-decision",
		`action="/t/VAQ-1/approval/ask-again"`, "Ask again")
	lacks(t, page, `action="/t/VAQ-1/approval/ask"`)

	r = c.Post("/t/VAQ-1/approval/ask-again", nil)
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	contains(t, c.Follow(r).Body, "Asked again for approval of the plan for VAQ-1.", "data-decision")
	if n := openApprovals(t, e); n != 1 {
		t.Fatalf("%d open approvals", n)
	}
	if n := dbCount(t, e, `SELECT count(*) FROM decisions WHERE id = ? AND state = 'withdrawn'`, old); n != 1 {
		t.Fatal("the stale approval wasn't withdrawn")
	}
	id := openApprovalID(t, e)
	r = c.Post("/inbox/"+id+"/answer", url.Values{"option": {"Approve"}, "back": {"/t/VAQ-1"}})
	page = c.Follow(r).Body
	contains(t, page, "Answered", "Ready")
	lacks(t, page, "data-decision")

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
	lacks(t, c.Get("/t/VAQ-1").Body, "data-readiness", "data-decision")
	if _, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: p.ID, TicketIDs: []string{tk.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan for VAQ-1?",
	}); err != nil {
		t.Fatal(err)
	}
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, "data-decision", "Approve the plan for VAQ-1?", `value="Approve"`, `action="/t/VAQ-1/approval/ask-again"`)
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
	lacks(t, page, "data-decision", `action="/inbox/`)
	contains(t, page, `action="/t/`+key+`/approval/ask"`)

	r := c.Post("/t/"+key+"/approval/ask", nil)
	contains(t, c.Follow(r).Body, "data-decision")
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

// VAQ-172: the ticket page lists every open decision naming the ticket as
// the inbox's cards, answered in place with the page as their back; the
// readiness block keeps Ready or its reason and Ask again, and has no
// answer form of its own.
func TestTicketPageDecisionCards(t *testing.T) {
	e, c, tk := approvalWorld(t)
	other := ticket(t, e, mustProject(t, e), core.TicketInput{Title: "Elsewhere"})
	q, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: tk.ProjectID, TicketIDs: []string{tk.ID}, Kind: core.DecisionQuestion,
		Question: "Which way?", Options: []core.Option{{Label: "Left"}, {Label: "Right"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: tk.ProjectID, TicketIDs: []string{other.ID}, Kind: core.DecisionQuestion, Question: "Unrelated?", Options: []core.Option{{Label: "Yes"}},
	}); err != nil {
		t.Fatal(err)
	}
	c.Post("/t/VAQ-1/approval/ask", nil)
	ap := openApprovalID(t, e)

	page := c.Get("/t/VAQ-1").Body
	contains(t, page, `id="ticket-decisions"`, `data-decision="`+q.ID+`"`, `data-decision="`+ap+`"`,
		`hx-post="/inbox/`+q.ID+`/answer"`, `name="back" value="/t/VAQ-1"`, `value="Left"`, `value="Approve"`, "Add a note", "Withdraw",
		`action="/t/VAQ-1/approval/ask-again"`)
	lacks(t, page, "data-approval", `action="/decisions/`, "Unrelated?")
	if n := strings.Count(page, "data-decision="); n != 2 {
		t.Fatalf("%d cards, want 2", n)
	}

	// Answering from the page: the card leaves, the page's live parts
	// refresh, and the readiness block reads Ready.
	r := c.PostHX("/inbox/"+ap+"/answer", url.Values{"option": {"Approve"}, "back": {"/t/VAQ-1"}})
	if r.Status != 200 || strings.TrimSpace(r.Body) != "" || !refreshesAfterSettle(r) {
		t.Fatalf("%d %q %q", r.Status, r.Body, r.Header.Get("HX-Trigger-After-Settle"))
	}
	page = c.Get("/t/VAQ-1").Body
	contains(t, page, "Ready", `data-decision="`+q.ID+`"`)
	lacks(t, page, `data-decision="`+ap+`"`, "Ask for plan approval.")

	// Without htmx the answer returns to the page.
	r = c.Post("/inbox/"+q.ID+"/answer", url.Values{"option": {"Left"}, "back": {"/t/VAQ-1"}})
	if r.Status != 303 || r.Location != "/t/VAQ-1" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page = c.Follow(r).Body
	contains(t, page, "Answered.", `id="ticket-decisions"`)
	lacks(t, page, "data-decision=", "Decisions")
}

// The cards are for any ticket, the workflow on or not.
func TestTicketPageDecisionCardsWorkflowOff(t *testing.T) {
	e := webtest.New(t)
	p := project(t, e)
	tk := ticket(t, e, p, core.TicketInput{Title: "Plain"})
	c := e.Client()
	empty := c.Get("/t/VAQ-1").Body
	lacks(t, empty, "data-decision=", "Decisions")
	// The part stays for a decision asked later, and takes no space empty:
	// no children at all, so :empty matches, and empty:hidden hides it.
	contains(t, empty, `data-live class="empty:hidden"></div>`)
	q, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: p.ID, TicketIDs: []string{tk.ID}, Kind: core.DecisionQuestion, Question: "Which way?", Options: []core.Option{{Label: "Left"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	page := c.Get("/t/VAQ-1").Body
	contains(t, page, `data-decision="`+q.ID+`"`, "Decisions")
	lacks(t, page, "data-readiness")
}
