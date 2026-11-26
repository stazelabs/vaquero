package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

type planConflictBody struct {
	Error struct {
		Code, Message string
		Current       *api.Plan
	}
}

func TestAPIPlanRoundTripAndConflict409(t *testing.T) {
	_, c, tok := apiWorld(t)
	if r := c.API("GET", "/api/v1/tickets/VAQ-1/plan", tok, nil); r.Status != 404 || !strings.Contains(r.Body, "VAQ-1 has no plan.") {
		t.Fatalf("no plan: %d %s", r.Status, r.Body)
	}
	r := c.API("PUT", "/api/v1/tickets/vaq-1/plan", tok, map[string]any{"body": "Step one."})
	if r.Status != 200 {
		t.Fatalf("create: %d %s", r.Status, r.Body)
	}
	var p api.Plan
	decode(t, r.Body, &p)
	if p.Ticket != "VAQ-1" || p.Revision != 1 || p.Body != "Step one." || p.UpdatedBy != "Owner" || p.UpdatedAt == "" {
		t.Fatalf("%+v", p)
	}
	r = c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": "Step one.\nStep two.", "revision": 1})
	decode(t, r.Body, &p)
	if r.Status != 200 || p.Revision != 2 {
		t.Fatalf("revise: %d %s", r.Status, r.Body)
	}
	var got api.Plan
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1/plan", tok, nil).Body, &got)
	if got != p {
		t.Fatalf("read back %+v, want %+v", got, p)
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1/plan/revisions/1", tok, nil).Body, &got)
	if got.Revision != 1 || got.Body != "Step one." || got.Ticket != "VAQ-1" {
		t.Fatalf("revision 1: %+v", got)
	}
	for _, path := range []string{"/plan/revisions/3", "/plan/revisions/0", "/plan/revisions/x"} {
		if r := c.API("GET", "/api/v1/tickets/VAQ-1"+path, tok, nil); r.Status != 404 {
			t.Errorf("%s: %d %s", path, r.Status, r.Body)
		}
	}

	// A save based on revision 1 is stale: 409 with the current plan.
	r = c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": "Another take.", "revision": 1})
	var e planConflictBody
	decode(t, r.Body, &e)
	if r.Status != 409 || e.Error.Code != "conflict" || e.Error.Current == nil || e.Error.Current.Revision != 2 || e.Error.Current.Body != "Step one.\nStep two." {
		t.Fatalf("stale: %d %s", r.Status, r.Body)
	}
	// An explicit revision 0 is refused: leave it out for a first plan.
	if r := c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": "Another take.", "revision": 0}); r.Status != 422 || !strings.Contains(r.Body, `"revision": "Send the revision you read`) {
		t.Fatalf("revision 0: %d %s", r.Status, r.Body)
	}
	// So is creating one when there is one.
	if r := c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": "Another take."}); r.Status != 409 {
		t.Fatalf("create over: %d %s", r.Status, r.Body)
	}
	r = c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": " "})
	var inv apiErrBody
	decode(t, r.Body, &inv)
	if r.Status != 422 || inv.Error.Fields["body"] != "Write the plan." {
		t.Fatalf("empty: %d %s", r.Status, r.Body)
	}
	if r := c.API("PUT", "/api/v1/tickets/VAQ-1/plan", tok, map[string]any{"body": "x", "text": "y"}); r.Status != 400 {
		t.Fatalf("unknown field: %d %s", r.Status, r.Body)
	}
	if r := c.API("PUT", "/api/v1/tickets/VAQ-99/plan", tok, map[string]any{"body": "x"}); r.Status != 404 {
		t.Fatalf("no ticket: %d %s", r.Status, r.Body)
	}

	// Events carry the revision, never the text.
	ev := c.API("GET", "/api/v1/events?newest=5", tok, nil).Body
	if strings.Contains(ev, "Step one") || !strings.Contains(ev, `"plan.revised"`) || !strings.Contains(ev, `"revision": 2`) {
		t.Fatalf("events: %s", ev)
	}
	if !strings.Contains(ev, "revised the plan for VAQ-1 (revision 2)") || !strings.Contains(ev, "wrote the plan for VAQ-1") {
		t.Fatalf("event text: %s", ev)
	}
}

func TestMCPPlanToolsMatchTheAPI(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	cs := mcpConnect(t, e, tok, "")
	res, text := mcpCall(t, cs, "save_plan", map[string]any{"key": "VAQ-1", "body": "First."})
	if res.IsError || !strings.Contains(text, `"revision": 1`) {
		t.Fatalf("save: %s", text)
	}
	res, text = mcpCall(t, cs, "save_plan", map[string]any{"key": "VAQ-1", "body": "Second.", "revision": 1})
	if res.IsError || !strings.Contains(text, `"revision": 2`) {
		t.Fatalf("revise: %s", text)
	}
	for _, tc := range []struct {
		args map[string]any
		path string
	}{
		{map[string]any{"key": "VAQ-1"}, "/api/v1/tickets/VAQ-1/plan"},
		{map[string]any{"key": "VAQ-1", "revision": 1}, "/api/v1/tickets/VAQ-1/plan/revisions/1"},
	} {
		res, text := mcpCall(t, cs, "get_plan", tc.args)
		if res.IsError {
			t.Fatalf("get_plan: %s", text)
		}
		if want := c.API("GET", tc.path, tok, nil).Body; text != want {
			t.Errorf("get_plan %v:\n got %s\nwant %s", tc.args, text, want)
		}
		checkStructured(t, "get_plan", res, text)
	}
	res, text = mcpCall(t, cs, "save_plan", map[string]any{"key": "VAQ-1", "body": "Stale.", "revision": 1})
	if !res.IsError || !strings.Contains(text, "changed since you read it") {
		t.Fatalf("stale: %s", text)
	}
}

func TestTicketPageShowsPlan(t *testing.T) {
	e, c, _ := apiWorld(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	tk, _ := e.Deps.R.GetTicketByNumber(bg, gen.GetTicketByNumberParams{ProjectID: p.ID, Number: 1})
	// Without a plan the live wrapper is there, empty, so a plan saved
	// elsewhere appears on the next change.
	if body := c.Get("/t/VAQ-1").Body; !strings.Contains(body, `class="empty:hidden"></section>`) || strings.Contains(body, "Revision 1") {
		t.Fatal("no empty plan wrapper without a plan")
	}
	if _, err := e.Deps.Core.SavePlan(bg, e.Deps.Actor.ID, tk.ID, "## Approach\n\nUse the *board*.", 0); err != nil {
		t.Fatal(err)
	}
	body := c.Get("/t/VAQ-1").Body
	if !strings.Contains(body, `id="plan"`) || !strings.Contains(body, "<em>board</em>") || !strings.Contains(body, "Revision 1") {
		t.Fatalf("plan: %s", body)
	}
	if strings.Contains(body, "data-plan-diff") {
		t.Fatal("changes shown for a first revision")
	}
	if _, err := e.Deps.Core.SavePlan(bg, e.Deps.Actor.ID, tk.ID, "## Approach\n\nUse the *list*.", 1); err != nil {
		t.Fatal(err)
	}
	body = c.Get("/t/VAQ-1").Body
	if !strings.Contains(body, "data-plan-diff") || !strings.Contains(body, "Use the *board*.") || !strings.Contains(body, "Revision 2") {
		t.Fatalf("diff: %s", body)
	}
}
