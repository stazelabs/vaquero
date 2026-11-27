package handlers_test

import (
	"net/url"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
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
	contains(t, c.Follow(r).Body, "already waiting")
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
	lacks(t, e.Client().Get("/t/VAQ-1").Body, "data-readiness", "data-approval", "approval/ask")
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
