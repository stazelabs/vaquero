package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

const botWarning = "Claimed by bot until 12:15 UTC."

// warnEnv is VAQ-1 claimed by the agent bot (until 12:15 UTC on the test
// clock), and a client acting as the owner.
func warnEnv(t *testing.T) (*webtest.Env, *webtest.Client, string) {
	t.Helper()
	e, tok := agentEnv(t)
	bot := e.Client()
	bot.Agent = "bot"
	if r := bot.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil); r.Status != 200 {
		t.Fatalf("claim %d %s", r.Status, r.Body)
	}
	return e, e.Client(), tok
}

type warned struct {
	Title, Status, Warning string
	ClaimedBy              string `json:"claimed_by"`
}

// VAQ-68: claims are advisory. A write to a ticket someone else holds goes
// through, and its answer says who holds it and until when.
func TestAPIWriteAnswersWarnAboutAnotherClaim(t *testing.T) {
	_, c, tok := warnEnv(t)
	for _, tc := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"PATCH", "/api/v1/tickets/VAQ-1", map[string]string{"title": "Edited anyway"}, 200},
		{"PATCH", "/api/v1/tickets/VAQ-1?view=summary", map[string]string{"title": "Again"}, 200},
		{"POST", "/api/v1/tickets/VAQ-1/move", map[string]string{"status": "In review"}, 200},
		{"POST", "/api/v1/tickets/VAQ-1/move?view=summary", map[string]string{"status": "In progress"}, 200},
		{"POST", "/api/v1/tickets/VAQ-1/comments", map[string]string{"body": "a note"}, 201},
		{"POST", "/api/v1/tickets/VAQ-1/comments?view=summary", map[string]string{"body": "another"}, 201},
	} {
		r := c.API(tc.method, tc.path, tok, tc.body)
		var got warned
		decode(t, r.Body, &got)
		if r.Status != tc.status || got.Warning != botWarning {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, r.Status, r.Body)
		}
	}
	var tk warned
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Title != "Again" || tk.ClaimedBy != "bot" || tk.Warning != "" {
		t.Fatalf("edit lost, claim lost, or a read warned: %+v", tk)
	}
}

// The claimant's own writes carry no warning, nor do writes to an
// unclaimed ticket.
func TestAPIWriteAnswersNoWarningForClaimant(t *testing.T) {
	e, c, tok := warnEnv(t)
	bot := e.Client()
	bot.Agent = "bot"
	r := bot.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]string{"title": "Mine"})
	if r.Status != 200 || strings.Contains(r.Body, "warning") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	bot.API("POST", "/api/v1/tickets/VAQ-1/release", tok, nil)
	if r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]string{"title": "Free"}); strings.Contains(r.Body, "warning") {
		t.Fatalf("%s", r.Body)
	}
}

// Bulk results carry the warning per item.
func TestAPIBulkWarnsPerItem(t *testing.T) {
	e, c, tok := warnEnv(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	ticket(t, e, p, core.TicketInput{Title: "Second"})
	var out api.BulkAnswer
	r := c.API("POST", "/api/v1/tickets/bulk", tok, map[string]any{"items": []map[string]any{
		{"key": "VAQ-1", "comment": "bulk note"},
		{"key": "VAQ-2", "comment": "bulk note"},
	}})
	decode(t, r.Body, &out)
	if r.Status != 200 || len(out.Items) != 2 || !out.Items[0].OK || out.Items[0].Warning != botWarning || out.Items[1].Warning != "" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// MCP answers are the API's, warning included.
func TestMCPWriteWarnsAboutAnotherClaim(t *testing.T) {
	e, _, tok := warnEnv(t)
	cs := mcpConnect(t, e, tok, "")
	_, text := mcpCall(t, cs, "update_ticket", map[string]any{"key": "VAQ-1", "title": "Via MCP"})
	if !strings.Contains(text, botWarning) {
		t.Fatal(text)
	}
}

// VAQ-68: the web edit form notes someone else's claim, and saving still
// works.
func TestEditFormNotesAnotherClaim(t *testing.T) {
	_, c, _ := warnEnv(t)
	contains(t, c.Get("/t/VAQ-1/edit").Body, "Claimed by bot until 12:15 UTC", "Claims are advisory")
	if r := c.Post("/t/VAQ-1", map[string][]string{"title": {"Saved"}}); r.Status != 303 {
		t.Fatalf("%d", r.Status)
	}
	contains(t, c.Get("/t/VAQ-1").Body, "Saved")
}

func TestEditFormNoNoteWhenUnclaimed(t *testing.T) {
	e, _ := agentEnv(t)
	lacks(t, e.Client().Get("/t/VAQ-1/edit").Body, "Claimed by")
}
