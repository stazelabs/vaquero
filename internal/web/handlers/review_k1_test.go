package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

// K1 review I1: a description edit's stored text never leaves the server
// through the API or MCP; the payload says only that a diff was kept.
func TestAPIEventsHideDescriptionText(t *testing.T) {
	e, tok := agentEnv(t)
	id := mustTicketID(t, e, 1)
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, id, core.TicketPatch{Body: new("secret before")}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, id, core.TicketPatch{Body: new("secret after")}, 0); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	for _, path := range []string{"/api/v1/events?after=0", "/api/v1/events?newest=50"} {
		r := c.API("GET", path, tok, nil)
		if strings.Contains(r.Body, "body_before") || strings.Contains(r.Body, "body_after") || strings.Contains(r.Body, "secret") {
			t.Fatalf("%s leaks the text: %s", path, r.Body)
		}
		var evs []api.Event
		decode(t, r.Body, &evs)
		flagged := 0
		for _, ev := range evs {
			if ev.Payload["body_diff"] == true {
				flagged++
			}
		}
		if flagged != 2 {
			t.Fatalf("%s: %d events flagged body_diff, want 2", path, flagged)
		}
	}
	cs := mcpConnect(t, e, tok, "")
	_, text := mcpCall(t, cs, "recent_activity", map[string]any{"newest": 50})
	if strings.Contains(text, "body_before") || strings.Contains(text, "secret") || !strings.Contains(text, `"body_diff":true`) && !strings.Contains(text, `"body_diff": true`) {
		t.Fatalf("MCP: %s", text)
	}
	// The ticket page still shows the diff.
	contains(t, c.Get("/t/VAQ-1").Body, "Show changes", "secret before")
}
