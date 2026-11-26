package handlers_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/workflow-off")

// ulid matches the ids a page or answer carries, which differ per run.
var ulid = regexp.MustCompile(`\b[0-9A-HJKMNP-TV-Z]{26}\b`)

// golden compares got, its ids blanked, with testdata/workflow-off/name, rewriting it with
// -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	got = ulid.ReplaceAllString(got, "ULID")
	path := filepath.Join("testdata", "workflow-off", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s changed:\n%s\nwant\n%s", name, got, want)
	}
}

// workflowOffWorld is a project with the workflow off: a ready ticket with
// a tier, an epic with an open sub-ticket, a blocked ticket and a done one.
func workflowOffWorld(t *testing.T) (*webtest.Env, *webtest.Client, string) {
	t.Helper()
	e := webtest.New(t)
	tok := e.Token(t)
	p := project(t, e)
	epic := ticket(t, e, p, core.TicketInput{Title: "Board work", Body: "The board.", Tags: []string{"ui"}, Priority: 2, Tier: "standard"})
	ticket(t, e, p, core.TicketInput{Title: "Columns", ParentID: epic.ID})
	ticket(t, e, p, core.TicketInput{Title: "Shipped thing", StatusID: statusID(t, e, "Done")})
	ticket(t, e, p, core.TicketInput{Title: "Blocked thing", Priority: 1})
	blocker := ticket(t, e, p, core.TicketInput{Title: "Blocker"})
	if _, err := e.Deps.Core.AddLink(bg, e.Deps.Actor.ID, blocker.ID, "blocks", core.TicketKey{Project: "VAQ", Number: 4}); err != nil {
		t.Fatal(err)
	}
	ticket(t, e, p, core.TicketInput{Title: "Small fix", Priority: 3})
	return e, e.Client(), tok
}

// Review Focus 1: with the workflow off, the ready queue, the ticket
// answers, the ticket list and the board are what they were before
// readiness existed, byte for byte, with no readiness key.
func TestWorkflowOffUnchanged(t *testing.T) {
	_, c, tok := workflowOffWorld(t)
	for name, path := range map[string]string{
		"ready.json":     "/api/v1/ready",
		"ready-vaq.json": "/api/v1/ready?project=VAQ",
		"ticket-1.json":  "/api/v1/tickets/VAQ-1",
		"ticket-2.json":  "/api/v1/tickets/VAQ-2",
		"ticket-6.json":  "/api/v1/tickets/VAQ-6",
		"tickets.json":   "/api/v1/projects/VAQ/tickets",
		"todo.json":      "/api/v1/projects/VAQ/tickets?category=todo",
		"board.html":     "/p/VAQ/board/fragment",
	} {
		var body string
		if strings.HasPrefix(path, "/api/") {
			r := c.API("GET", path, tok, nil)
			if r.Status != 200 {
				t.Fatalf("%s: %d %s", path, r.Status, r.Body)
			}
			body = r.Body
		} else {
			r := c.Get(path)
			if r.Status != 200 {
				t.Fatalf("%s: %d", path, r.Status)
			}
			body = r.Body
		}
		if strings.Contains(body, "readiness") {
			t.Errorf("%s has a readiness key: %s", path, body)
		}
		golden(t, name, body)
	}
}

// workflowOn turns the VAQ project's workflow on.
func workflowOn(t *testing.T, e *webtest.Env) {
	t.Helper()
	on := true
	if _, err := e.Deps.Core.UpdateProject(bg, e.Deps.Actor.ID, mustProject(t, e).ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
}

// approvePlan asks and answers a plan approval of the tickets.
func approvePlan(t *testing.T, e *webtest.Env, ids ...string) {
	t.Helper()
	d, err := e.Deps.Core.AskDecision(bg, e.Deps.Actor.ID, core.DecisionInput{
		ProjectID: mustProject(t, e).ID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AnswerDecision(bg, e.Deps.Actor.ID, d.ID, core.Answer{Option: core.ApproveLabel}); err != nil {
		t.Fatal(err)
	}
}

func TestReadyAPIFiltersWhenWorkflowOn(t *testing.T) {
	e, c, tok := workflowOffWorld(t)
	workflowOn(t, e)
	// VAQ-6 has a tier and an approval; VAQ-2 and VAQ-5 have neither.
	tier := "light"
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, ticketID(t, e, 6), core.TicketPatch{Tier: &tier}, 0); err != nil {
		t.Fatal(err)
	}
	approvePlan(t, e, ticketID(t, e, 6))
	var ready []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/ready?project=VAQ", tok, nil).Body, &ready)
	if len(ready) != 1 || ready[0].Key != "VAQ-6" || ready[0].Tier != "light" || ready[0].Readiness == nil ||
		!ready[0].Readiness.Ready || ready[0].Readiness.Reasons == nil || len(ready[0].Readiness.Reasons) != 0 {
		t.Fatalf("%+v", ready)
	}
	var all []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/ready", tok, nil).Body, &all)
	if len(all) != 1 || all[0].Key != "VAQ-6" {
		t.Fatalf("%+v", all)
	}

	// The ticket answer carries the reasons, and ready agrees with them.
	var two api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &two)
	if two.Ready || two.Readiness == nil || two.Readiness.Ready ||
		!slices.Equal(two.Readiness.Reasons, []string{readiness.NoTier, readiness.PlanNotApproved}) {
		t.Fatalf("%+v", two)
	}
	var one api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &one)
	if one.Readiness == nil || len(one.SubTickets) != 1 || one.SubTickets[0].Ready || one.SubTickets[0].Readiness == nil ||
		!slices.Equal(one.SubTickets[0].Readiness.Reasons, []string{readiness.NoTier, readiness.PlanNotApproved}) {
		t.Fatalf("%+v", one)
	}
	// The list's todo summaries carry it; the done ticket's doesn't.
	var list []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/projects/VAQ/tickets", tok, nil).Body, &list)
	for _, s := range list {
		if (s.Category == "todo") != (s.Readiness != nil) || s.Ready != (s.Key == "VAQ-6") {
			t.Errorf("%+v", s)
		}
	}
	// The board marks only VAQ-6 ready and shows the tiers.
	page := c.Get("/p/VAQ/board").Body
	if n := strings.Count(page, "data-ready"); n != 1 {
		t.Errorf("%d ready cards", n)
	}
	contains(t, page, `data-tier="light"`, `data-tier="standard"`)
	// MCP's ready_tickets is the same answer.
	_, text := mcpCall(t, mcpConnect(t, e, tok, ""), "ready_tickets", map[string]any{"project": "VAQ"})
	if !strings.Contains(text, `"readiness"`) || strings.Contains(text, "VAQ-2") {
		t.Errorf("mcp: %s", text)
	}
}

// With the workflow off the board shows no tier.
func TestBoardHidesTierWhenWorkflowOff(t *testing.T) {
	_, c, _ := workflowOffWorld(t)
	if page := c.Get("/p/VAQ/board").Body; strings.Contains(page, "data-tier") {
		t.Error("tier shown with the workflow off")
	}
}
