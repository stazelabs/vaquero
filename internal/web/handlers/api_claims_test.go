package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

type summaryJSON struct {
	Key, Assignee  string
	ClaimedBy      string `json:"claimed_by"`
	ClaimExpiresAt string `json:"claim_expires_at"`
	Priority       int64
	Ready          bool
}

func TestAPIReadyOrder(t *testing.T) {
	e, tok := agentEnv(t) // VAQ-1, no priority
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	for _, pr := range []int64{4, 1, 0, 2} { // VAQ-2 low, VAQ-3 urgent, VAQ-4 none, VAQ-5 high
		if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "t", Priority: pr}); err != nil {
			t.Fatal(err)
		}
	}
	bl, _ := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "blocked", Priority: 1})
	if _, err := e.Deps.Core.AddLink(bg, e.Deps.Actor.ID, mustTicketID(t, e, 1), "blocks", core.TicketKey{Project: "VAQ", Number: bl.Number}); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	var got []summaryJSON
	decode(t, c.API("GET", "/api/v1/ready?project=vaq", tok, nil).Body, &got)
	keys := []string{}
	for _, s := range got {
		keys = append(keys, s.Key)
	}
	// VAQ-6 is blocked by VAQ-1. Urgent, high, medium, low, then no priority, each in board order.
	if strings.Join(keys, ",") != "VAQ-3,VAQ-5,VAQ-2,VAQ-1,VAQ-4" {
		t.Fatalf("%v", keys)
	}
	decode(t, c.API("GET", "/api/v1/ready?limit=2", tok, nil).Body, &got)
	if len(got) != 2 || got[0].Key != "VAQ-3" {
		t.Fatalf("%+v", got)
	}
	if r := c.API("GET", "/api/v1/ready?project=NOPE", tok, nil); r.Status != 404 {
		t.Fatalf("unknown project: %d", r.Status)
	}
}

// Review Focus 1 at the API.
func TestAPIClaimConflict(t *testing.T) {
	e, tok := agentEnv(t)
	a, b := e.Client(), e.Client()
	a.Agent, b.Agent = "builder", "reviewer"
	r := a.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil)
	var tk summaryJSON
	decode(t, r.Body, &tk)
	if r.Status != 200 || tk.ClaimedBy != "builder" || tk.Assignee != "builder" || tk.ClaimExpiresAt != "2026-11-18T12:15:00.000Z" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = b.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, map[string]int{"lease_minutes": 30})
	if r.Status != 422 || !strings.Contains(r.Body, "VAQ-1 is claimed by builder until 2026-11-18 12:15 UTC.") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := a.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, map[string]int{"lease_minutes": 500}); r.Status != 422 || !strings.Contains(r.Body, "1 to 120 minutes") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := b.API("POST", "/api/v1/tickets/VAQ-1/unclaim", tok, nil); r.Status != 422 {
		t.Fatalf("release by other: %d", r.Status)
	}
	// R-Q8: the old route is gone.
	if r := a.API("POST", "/api/v1/tickets/VAQ-1/release", tok, nil); r.Status != 404 && r.Status != 405 {
		t.Fatalf("old release route: %d", r.Status)
	}
	r = a.API("POST", "/api/v1/tickets/VAQ-1/unclaim", tok, nil)
	tk = summaryJSON{}
	decode(t, r.Body, &tk)
	if r.Status != 200 || tk.ClaimedBy != "" || tk.Assignee != "builder" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

func TestAPIHeartbeatExtends(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	c.Agent = "builder"
	c.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil)
	r := c.API("POST", "/api/v1/tickets/VAQ-1/heartbeat", tok, map[string]int{"lease_minutes": 60})
	var tk summaryJSON
	decode(t, r.Body, &tk)
	if r.Status != 200 || tk.ClaimExpiresAt != "2026-11-18T13:00:00.000Z" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/heartbeat", tok, map[string]string{"lease": "x"}); r.Status != 400 {
		t.Fatalf("unknown field: %d", r.Status)
	}
}
