package handlers_test

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// apiWorld is a project VAQ with three tickets: VAQ-1 (tagged ui, blocks
// VAQ-2, one comment), VAQ-2 (a sub-ticket of VAQ-1), VAQ-3 (done).
func apiWorld(t *testing.T) (*webtest.Env, *webtest.Client, string) {
	t.Helper()
	e := webtest.New(t)
	tok := e.Token(t) // first, so the newest event is the comment
	p := project(t, e)
	one := ticket(t, e, p, core.TicketInput{Title: "Board work", Tags: []string{"ui"}, Priority: 2})
	ticket(t, e, p, core.TicketInput{Title: "Columns", ParentID: one.ID})
	ticket(t, e, p, core.TicketInput{Title: "Shipped thing", StatusID: statusID(t, e, "Done")})
	if _, err := e.Deps.Core.AddLink(bg, e.Deps.Actor.ID, one.ID, "blocks", core.TicketKey{Project: "VAQ", Number: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.AddComment(bg, e.Deps.Actor.ID, one.ID, "Looks *good*."); err != nil {
		t.Fatal(err)
	}
	return e, e.Client(), tok
}

func TestAPIProjects(t *testing.T) {
	_, c, tok := apiWorld(t)
	var ps []api.Project
	decode(t, c.API("GET", "/api/v1/projects", tok, nil).Body, &ps)
	if len(ps) != 1 || ps[0].Key != "VAQ" || ps[0].OpenCount != 2 {
		t.Fatalf("%+v", ps)
	}
	var p api.Project
	decode(t, c.API("GET", "/api/v1/projects/vaq", tok, nil).Body, &p)
	if p.Name != "Vaquero" {
		t.Fatalf("%+v", p)
	}
	var ss []api.Status
	decode(t, c.API("GET", "/api/v1/projects/VAQ/statuses", tok, nil).Body, &ss)
	if len(ss) != 6 || ss[0].Name != "Backlog" || ss[0].Category != "backlog" {
		t.Fatalf("%+v", ss)
	}
	if r := c.API("GET", "/api/v1/projects/NOPE", tok, nil); r.Status != 404 {
		t.Fatal(r.Status)
	}
}

func TestAPITicketList(t *testing.T) {
	_, c, tok := apiWorld(t)
	list := func(q string) []api.TicketSummary {
		t.Helper()
		var ts []api.TicketSummary
		r := c.API("GET", "/api/v1/projects/VAQ/tickets"+q, tok, nil)
		if r.Status != 200 {
			t.Fatalf("%s: %d %s", q, r.Status, r.Body)
		}
		decode(t, r.Body, &ts)
		return ts
	}
	if n := len(list("")); n != 3 {
		t.Fatalf("all: %d", n)
	}
	if n := len(list("?open=1")); n != 2 {
		t.Fatalf("open: %d", n)
	}
	if ts := list("?tag=ui"); len(ts) != 1 || ts[0].Key != "VAQ-1" || ts[0].Priority != 2 || ts[0].Version < 1 || ts[0].Status != "Todo" {
		t.Fatalf("tag: %+v", ts)
	}
	if ts := list("?status=done"); len(ts) != 1 || ts[0].Key != "VAQ-3" {
		t.Fatalf("status: %+v", ts)
	}
	if n := len(list("?status=Nonsense")); n != 0 {
		t.Fatalf("unknown status: %d", n)
	}
	if n := len(list("?limit=1")); n != 1 {
		t.Fatalf("limit: %d", n)
	}
}

func TestAPITicketGet(t *testing.T) {
	_, c, tok := apiWorld(t)
	var tk api.Ticket
	r := c.API("GET", "/api/v1/tickets/vaq-1", tok, nil)
	decode(t, r.Body, &tk)
	if tk.Key != "VAQ-1" || tk.Title != "Board work" || len(tk.Tags) != 1 || len(tk.SubTickets) != 1 || tk.SubTickets[0].Key != "VAQ-2" {
		t.Fatalf("%+v", tk)
	}
	if len(tk.Links) != 1 || tk.Links[0].Kind != "blocks" || tk.Links[0].Direction != "out" || tk.Links[0].Key != "VAQ-2" {
		t.Fatalf("links %+v", tk.Links)
	}
	if len(tk.Comments) != 1 || tk.Comments[0].Body != "Looks *good*." || tk.Comments[0].Author != "Owner" {
		t.Fatalf("comments %+v", tk.Comments)
	}
	var two api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &two)
	if two.Parent != "VAQ-1" || len(two.Links) != 1 || two.Links[0].Direction != "in" {
		t.Fatalf("%+v", two)
	}
	for _, k := range []string{"VAQ-99", "nonsense"} {
		if r := c.API("GET", "/api/v1/tickets/"+k, tok, nil); r.Status != 404 {
			t.Errorf("%s: %d", k, r.Status)
		}
	}
}

func TestAPISearch(t *testing.T) {
	_, c, tok := apiWorld(t)
	var hits []api.SearchHit
	decode(t, c.API("GET", "/api/v1/search?q=column", tok, nil).Body, &hits)
	if len(hits) != 1 || hits[0].Key != "VAQ-2" || !strings.Contains(hits[0].Snippet, "[Columns]") {
		t.Fatalf("%+v", hits)
	}
	for _, q := range []string{`"`, "AND (", ""} {
		if r := c.API("GET", "/api/v1/search?q="+url.QueryEscape(q), tok, nil); r.Status != 200 {
			t.Errorf("%q: %d", q, r.Status)
		}
	}
}

func TestAPIEventsAfter(t *testing.T) {
	_, c, tok := apiWorld(t)
	var all []api.Event
	decode(t, c.API("GET", "/api/v1/events", tok, nil).Body, &all)
	if len(all) < 6 || all[0].Seq > all[1].Seq {
		t.Fatalf("%d events, oldest first?", len(all))
	}
	last := all[len(all)-1]
	if last.Text != "commented on VAQ-1" || last.Ticket != "VAQ-1" {
		t.Fatalf("%+v", last)
	}
	var after []api.Event
	decode(t, c.API("GET", fmt.Sprintf("/api/v1/events?after=%d", all[len(all)-3].Seq), tok, nil).Body, &after)
	if len(after) != 2 {
		t.Fatalf("after: %d", len(after))
	}
	var some []api.Event
	decode(t, c.API("GET", "/api/v1/events?limit=2", tok, nil).Body, &some)
	if len(some) != 2 || some[0].Seq != all[0].Seq {
		t.Fatalf("limit: %+v", some)
	}
}

// "mine" is the token's actor, not the server's owner.
func TestAPIMineIsTheCaller(t *testing.T) {
	e, c, tok := apiWorld(t)
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, mustTicketID(t, e, 1), core.TicketPatch{AssigneeID: new(e.Deps.Actor.ID)}, 0); err != nil {
		t.Fatal(err)
	}
	agent, err := e.DB.Q.CreateActor(bg, gen.CreateActorParams{ID: idgen.New(), Kind: "agent", Name: "builder", CreatedAt: "2026-11-18T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	agentTok, _, err := e.Deps.Core.CreateToken(bg, e.Deps.Actor.ID, agent.ID, "agent")
	if err != nil {
		t.Fatal(err)
	}
	var mine []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/projects/VAQ/tickets?mine=1", tok, nil).Body, &mine)
	var theirs []api.TicketSummary
	decode(t, c.API("GET", "/api/v1/projects/VAQ/tickets?mine=1", agentTok, nil).Body, &theirs)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("owner %d, agent %d", len(mine), len(theirs))
	}
}

func mustTicketID(t *testing.T, e *webtest.Env, n int64) string {
	t.Helper()
	tk, _, err := e.Deps.Core.ResolveTicket(bg, e.Deps.R, core.TicketKey{Project: "VAQ", Number: n})
	if err != nil {
		t.Fatal(err)
	}
	return tk.ID
}

func TestAPIEventsNewest(t *testing.T) {
	_, c, tok := apiWorld(t)
	var all, newest []api.Event
	decode(t, c.API("GET", "/api/v1/events?limit=500", tok, nil).Body, &all)
	decode(t, c.API("GET", "/api/v1/events?newest=2", tok, nil).Body, &newest)
	if len(newest) != 2 || newest[1].Seq != all[len(all)-1].Seq || newest[0].Seq != all[len(all)-2].Seq {
		t.Fatalf("%+v", newest)
	}
}

// The API article promises milestone and assignee filters by name.
func TestAPITicketFiltersByName(t *testing.T) {
	e, c, tok := apiWorld(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	m, err := e.Deps.Core.CreateMilestone(bg, e.Deps.Actor.ID, p.ID, core.MilestoneInput{Name: "Beta"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, mustTicketID(t, e, 1), core.TicketPatch{MilestoneID: &m.ID, AssigneeID: new(e.Deps.Actor.ID)}, 0); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{"milestone=beta": 1, "milestone=none": 2, "assignee=owner": 1, "assignee=Owner&milestone=Beta": 1} {
		var got []api.TicketSummary
		r := c.API("GET", "/api/v1/projects/VAQ/tickets?"+q, tok, nil)
		decode(t, r.Body, &got)
		if r.Status != 200 || len(got) != want {
			t.Errorf("%s: %d, %d tickets", q, r.Status, len(got))
		}
	}
	for _, q := range []string{"milestone=Gamma", "assignee=nobody"} {
		if r := c.API("GET", "/api/v1/projects/VAQ/tickets?"+q, tok, nil); r.Status != 422 {
			t.Errorf("%s: %d", q, r.Status)
		}
	}
}
