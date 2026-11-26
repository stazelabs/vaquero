package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-61: a project write answers the project's real open count.
func TestAPIProjectWritesCountOpenTickets(t *testing.T) {
	_, c, tok := apiWorld(t) // VAQ has two open tickets
	var p api.Project
	r := c.API("PATCH", "/api/v1/projects/VAQ", tok, map[string]any{"name": "Vaquero two"})
	decode(t, r.Body, &p)
	if r.Status != 200 || p.Name != "Vaquero two" || p.OpenCount != 2 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	r = c.API("POST", "/api/v1/projects/VAQ/key", tok, map[string]any{"key": "VQ"})
	decode(t, r.Body, &p)
	if r.Status != 200 || p.Key != "VQ" || p.OpenCount != 2 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// VAQ-61: sub-tickets in a ticket's answer are full summaries, like the
// list's: assignee, tags, ready and claim included.
func TestAPISubTicketsAreFullSummaries(t *testing.T) {
	e, c, tok := apiWorld(t)
	p := mustProject(t, e)
	ticket(t, e, p, core.TicketInput{Title: "Rows", ParentID: ticketID(t, e, 1), AssigneeID: e.Deps.Actor.ID, Tags: []string{"ux"}})
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	var list []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/projects/VAQ/tickets", tok, nil).Body, &list)
	byKey := map[string]api.TicketSummary{}
	for _, s := range list {
		byKey[s.Key] = s
	}
	if len(tk.SubTickets) != 2 || tk.SubTickets[0].Key != "VAQ-2" || tk.SubTickets[1].Key != "VAQ-4" {
		t.Fatalf("%+v", tk.SubTickets)
	}
	for _, s := range tk.SubTickets {
		want := byKey[s.Key]
		if s.Assignee != want.Assignee || strings.Join(s.Tags, ",") != strings.Join(want.Tags, ",") || s.Ready != want.Ready || s.Version != want.Version {
			t.Errorf("sub-ticket %+v, list %+v", s, want)
		}
	}
	if s := tk.SubTickets[1]; s.Assignee == "" || len(s.Tags) != 1 || s.Tags[0] != "ux" {
		t.Errorf("%+v", s)
	}
}

// VAQ-61: a move naming a before or after ticket that doesn't exist is
// refused, not read as "no neighbour".
func TestAPIMoveRefusesUnknownNeighbours(t *testing.T) {
	_, c, tok := apiWorld(t)
	for _, field := range []string{"before", "after"} {
		for key, msg := range map[string]string{"VAQ-99": "No ticket VAQ-99.", "not a key": "That isn't a ticket key."} {
			r := c.API("POST", "/api/v1/tickets/VAQ-2/move", tok, map[string]any{"status": "Todo", field: key})
			if r.Status != 422 || !strings.Contains(r.Body, `"`+field+`"`) || !strings.Contains(r.Body, msg) {
				t.Errorf("%s %q: %d %s", field, key, r.Status, r.Body)
			}
		}
	}
	if r := c.API("POST", "/api/v1/tickets/VAQ-2/move", tok, map[string]any{"status": "Todo", "before": "vaq-1"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// ticketID is VAQ-n's id.
func ticketID(t *testing.T, e *webtest.Env, n int64) string {
	t.Helper()
	tk, _, err := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: n})
	if err != nil {
		t.Fatal(err)
	}
	return tk.ID
}

// VAQ-61: a database error while resolving a name is a 500, not "no
// milestone called that".
func TestAPINameLookupErrorIsInternal(t *testing.T) {
	e, c, tok := apiWorld(t)
	if _, err := e.Deps.DB.Exec(`ALTER TABLE milestones RENAME TO milestones_gone`); err != nil {
		t.Fatal(err)
	}
	r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"milestone": "Beta"})
	if r.Status != 500 || strings.Contains(r.Body, "No milestone") {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// VAQ-61: a create names no version and no tags to remove; either is
// refused rather than ignored.
func TestAPICreateRefusesPatchOnlyFields(t *testing.T) {
	_, c, tok := apiWorld(t)
	for field, v := range map[string]any{"version": 3, "remove_tags": []string{"ui"}} {
		r := c.API("POST", "/api/v1/projects/VAQ/tickets", tok, map[string]any{"title": "New", field: v})
		if r.Status != 422 || !strings.Contains(r.Body, `"`+field+`"`) {
			t.Errorf("%s: %d %s", field, r.Status, r.Body)
		}
	}
}

// VAQ-61: a token is revoked by its exact prefix; base64url is case
// sensitive, so another case names another token.
func TestAPITokenRevokeComparesExactly(t *testing.T) {
	_, c, tok := apiWorld(t)
	var made api.Token
	decode(t, c.API("POST", "/api/v1/tokens", tok, map[string]any{"name": "ci"}).Body, &made)
	flipped := []byte(made.Prefix)
	for i := 3; i < len(flipped); i++ {
		if b := flipped[i]; b >= 'a' && b <= 'z' {
			flipped[i] = b - 32
		} else if b >= 'A' && b <= 'Z' {
			flipped[i] = b + 32
		}
	}
	if string(flipped) == made.Prefix {
		t.Skip("the prefix has no letters")
	}
	if r := c.API("POST", "/api/v1/tokens/"+string(flipped)+"/revoke", tok, nil); r.Status != 404 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("POST", "/api/v1/tokens/"+made.Prefix+"/revoke", tok, nil); r.Status != 204 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}
