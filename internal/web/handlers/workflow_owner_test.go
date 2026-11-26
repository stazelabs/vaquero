package handlers_test

import (
	"strings"
	"testing"
)

// Ruling N-R6: only the owner, acting directly, turns the agent workflow on
// or off or changes the tier map; an agent (the owner's token with
// Vaquero-Agent) is refused, through the API and through MCP.
func TestWorkflowSwitchIsTheOwners(t *testing.T) {
	e, tok := moveEnv(t)
	agent := e.Client()
	agent.Agent = "builder"
	for _, tc := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"workflow": true}, "Only the owner turns the agent workflow on or off."},
		{map[string]any{"tier_map": map[string]any{"claude": map[string]any{"light": "small"}}}, "Only the owner changes the tier map."},
	} {
		if r := agent.API("PATCH", "/api/v1/projects/VAQ", tok, tc.body); r.Status != 422 || !strings.Contains(r.Body, tc.want) {
			t.Errorf("agent %v: %d %s", tc.body, r.Status, r.Body)
		}
	}
	owner := e.Client()
	if r := owner.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"workflow": true}); r.Status != 200 {
		t.Fatalf("owner: %d %s", r.Status, r.Body)
	}
	if r := agent.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"workflow": false}); r.Status != 422 {
		t.Fatalf("agent turning it off: %d %s", r.Status, r.Body)
	}
	// An unchanged switch, or another field, is still the agent's to send.
	if r := agent.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"workflow": true, "description": "Ours."}); r.Status != 200 {
		t.Fatalf("agent, workflow unchanged: %d %s", r.Status, r.Body)
	}
}

// Ruling N-R6: an agent can't take a ticket out of the workflow's gate by
// moving it to a project with the workflow off; the owner can.
func TestAgentCantMoveOutOfTheWorkflow(t *testing.T) {
	e, tok := moveEnv(t)
	owner := e.Client()
	if r := owner.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"workflow": true}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	const want = "Only the owner moves a ticket out of a project with the agent workflow on."
	agent := e.Client()
	agent.Agent = "builder"
	if r := agent.API("POST", "/api/v1/tickets/VAQ-1/move-to", tok, map[string]any{"project": "OTH"}); r.Status != 422 || !strings.Contains(r.Body, want) {
		t.Fatalf("API: %d %s", r.Status, r.Body)
	}
	cs := mcpConnect(t, e, tok, "builder")
	res, text := mcpCall(t, cs, "bulk_update", map[string]any{"items": []any{map[string]any{"key": "VAQ-1", "project": "OTH"}}})
	if res.IsError || !strings.Contains(text, want) || strings.Contains(text, `"ok": true`) {
		t.Fatalf("MCP: %s", text)
	}
	// Into a workflow project is fine for an agent; out of one, the owner.
	if r := agent.API("POST", "/api/v1/tickets/OTH-1/move-to", tok, map[string]any{"project": "VAQ"}); r.Status != 200 {
		t.Fatalf("agent into VAQ: %d %s", r.Status, r.Body)
	}
	if r := owner.API("POST", "/api/v1/tickets/VAQ-1/move-to", tok, map[string]any{"project": "OTH"}); r.Status != 200 {
		t.Fatalf("owner: %d %s", r.Status, r.Body)
	}
}
