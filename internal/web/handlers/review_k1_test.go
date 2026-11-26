package handlers_test

import (
	"fmt"
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
	// The ticket page still shows the diff, once it is opened.
	contains(t, c.Get("/t/VAQ-1").Body, "Show changes")
	seq, _ := e.Deps.R.MaxEventSeq(bg)
	contains(t, c.GetHX(fmt.Sprintf("/t/VAQ-1/changes/%d", seq)).Body, "secret before")
}

// K1 review M1: the other write answers on a claimed ticket warn too, and a
// comment's answer names the claim.
func TestAPIOtherWritesWarnAboutAnotherClaim(t *testing.T) {
	e, c, tok := warnEnv(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	ticket(t, e, p, core.TicketInput{Title: "Second"})

	var l api.Link
	r := c.API("POST", "/api/v1/tickets/VAQ-1/links", tok, map[string]string{"kind": "relates", "to": "VAQ-2"})
	decode(t, r.Body, &l)
	if r.Status != 201 || l.Warning != botWarning {
		t.Fatalf("link: %d %s", r.Status, r.Body)
	}

	var cm api.Comment
	r = c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]string{"body": "first"})
	decode(t, r.Body, &cm)
	if r.Status != 201 || cm.Warning != botWarning || cm.ClaimedBy != "bot" || cm.ClaimExpiresAt != "2026-11-18T12:15:00.000Z" {
		t.Fatalf("comment: %d %s", r.Status, r.Body)
	}
	r = c.API("PATCH", "/api/v1/comments/"+cm.ID, tok, map[string]string{"body": "edited"})
	var ed api.Comment
	decode(t, r.Body, &ed)
	if r.Status != 200 || ed.Warning != botWarning || ed.ClaimedBy != "bot" || ed.Body != "edited" {
		t.Fatalf("comment edit: %d %s", r.Status, r.Body)
	}

	var a api.Attachment
	r = c.UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "note.txt", "text/plain", []byte("hello"))
	decode(t, r.Body, &a)
	if r.Status != 201 || a.Warning != botWarning {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}

	// Unclaimed: no warning, no claim fields.
	r = c.API("POST", "/api/v1/tickets/VAQ-2/comments", tok, map[string]string{"body": "free"})
	if strings.Contains(r.Body, "warning") || strings.Contains(r.Body, "claimed_by") {
		t.Fatalf("%s", r.Body)
	}
}
