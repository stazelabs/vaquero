package handlers_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

func TestAPIMilestoneCreate(t *testing.T) {
	e, c, tok := apiWorld(t)
	seq := seqNow(t, e)
	r := c.API("POST", "/api/v1/projects/vaq/milestones", tok, map[string]string{"name": "Phase 4", "description": "Sign-in.", "due_on": "2027-03-31"})
	var m api.Milestone
	decode(t, r.Body, &m)
	if r.Status != 201 || m.Name != "Phase 4" || m.Description != "Sign-in." || m.DueOn != "2027-03-31" || m.Closed || m.Total != 0 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.created", "Owner")
	if r := c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": "Phase 5"}); r.Status != 201 {
		t.Fatalf("name only: %d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.created", "Owner")

	refused(t, c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": "phase 4"}), "name", "A milestone with that name exists.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": " "}), "name", "Enter a name.")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": "X", "due_on": "soon"}), "due_on", "Use a date like 2026-12-31.")
	noEvent(t, e, seq)
}

func TestAPIMilestoneUpdate(t *testing.T) {
	e, c, tok := apiWorld(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	ms, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Beta", Description: "First cut.", DueOn: "2026-12-15"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, mustTicketID(t, e, 1), core.TicketPatch{MilestoneID: &ms.ID}, 0); err != nil {
		t.Fatal(err)
	}
	seq := seqNow(t, e)

	// Only the fields sent change.
	r := c.API("PATCH", "/api/v1/projects/VAQ/milestones/beta", tok, map[string]string{"description": "The board."})
	var m api.Milestone
	decode(t, r.Body, &m)
	if r.Status != 200 || m.Name != "Beta" || m.Description != "The board." || m.DueOn != "2026-12-15" || m.Total != 1 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.updated", "Owner")
	r = c.API("PATCH", "/api/v1/projects/VAQ/milestones/Beta", tok, map[string]string{"name": "Beta 1", "due_on": ""})
	m = api.Milestone{}
	decode(t, r.Body, &m)
	if r.Status != 200 || m.Name != "Beta 1" || m.Description != "The board." || m.DueOn != "" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.updated", "Owner")
	// Nothing changes: no event.
	if r := c.API("PATCH", "/api/v1/projects/VAQ/milestones/Beta%201", tok, map[string]string{"name": "Beta 1"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	noEvent(t, e, seq)

	if _, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "GA"}); err != nil {
		t.Fatal(err)
	}
	seq = seqNow(t, e)
	refused(t, c.API("PATCH", "/api/v1/projects/VAQ/milestones/GA", tok, map[string]string{"name": "beta 1"}), "name", "A milestone with that name exists.")
	notFound(t, c.API("PATCH", "/api/v1/projects/VAQ/milestones/Nope", tok, map[string]string{"name": "X"}), "No milestone Nope in VAQ.")
	noEvent(t, e, seq)
}

func TestAPIMilestoneCloseReopen(t *testing.T) {
	e, c, tok := apiWorld(t)
	if r := c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": "Beta"}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq := seqNow(t, e)
	r := c.API("POST", "/api/v1/projects/VAQ/milestones/Beta/close", tok, nil)
	var m api.Milestone
	decode(t, r.Body, &m)
	if r.Status != 200 || !m.Closed {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.closed", "Owner")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/milestones/Beta/close", tok, nil), "milestone", "It's already closed.")
	r = c.API("POST", "/api/v1/projects/VAQ/milestones/beta/reopen", tok, nil)
	decode(t, r.Body, &m)
	if r.Status != 200 || m.Closed {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.reopened", "Owner")
	refused(t, c.API("POST", "/api/v1/projects/VAQ/milestones/Beta/reopen", tok, nil), "milestone", "It's already open.")
	notFound(t, c.API("POST", "/api/v1/projects/VAQ/milestones/Nope/close", tok, nil), "No milestone Nope in VAQ.")
	noEvent(t, e, seq)
}

// R6: an agent's milestone changes are attributed to it.
func TestAPIMilestoneChangesByAnAgent(t *testing.T) {
	e, c, tok := apiWorld(t)
	c.Agent = "planner"
	seq := seqNow(t, e) + 1 // the agent's actor is made on first use
	if r := c.API("POST", "/api/v1/projects/VAQ/milestones", tok, map[string]string{"name": "Phase 6"}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.created", "planner")
	if r := c.API("PATCH", "/api/v1/projects/VAQ/milestones/Phase%206", tok, map[string]string{"description": "Later."}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.updated", "planner")
	if r := c.API("POST", "/api/v1/projects/VAQ/milestones/Phase%206/close", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	seq = oneEvent(t, e, seq, "milestone.closed", "planner")
	if r := c.API("POST", "/api/v1/projects/VAQ/milestones/Phase%206/reopen", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	oneEvent(t, e, seq, "milestone.reopened", "planner")
}
