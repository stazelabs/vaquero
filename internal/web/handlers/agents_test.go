package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// agentEnv is a server with project VAQ and ticket VAQ-1, and the owner's token.
func agentEnv(t *testing.T) (*webtest.Env, string) {
	t.Helper()
	e := webtest.New(t)
	p, err := e.Deps.Core.CreateProject(bg, e.Deps.Actor.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "First"}); err != nil {
		t.Fatal(err)
	}
	return e, e.Token(t)
}

type me struct{ Actor struct{ Name, Kind string } }

func TestAgentHeaderActsAsAgent(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	c.Agent = "builder"
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]string{"body": "On it."}); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var tk struct {
		Comments []struct{ Author, Body string }
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.Comments) != 1 || tk.Comments[0].Author != "builder" {
		t.Fatalf("%+v", tk)
	}
	var m me
	decode(t, c.API("GET", "/api/v1/me", tok, nil).Body, &m)
	if m.Actor.Name != "builder" || m.Actor.Kind != "agent" {
		t.Fatalf("%+v", m)
	}
	c.Agent = ""
	decode(t, c.API("GET", "/api/v1/me", tok, nil).Body, &m)
	if m.Actor.Name != "Owner" || m.Actor.Kind != "human" {
		t.Fatalf("without the header: %+v", m)
	}
}

// A token that isn't the owner's keeps its own actor; the header can't
// make it someone else.
func TestAgentHeaderIgnoredForNonOwnerToken(t *testing.T) {
	e, _ := agentEnv(t)
	bot, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "ci-bot")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := e.Deps.Core.CreateToken(bg, e.Deps.Actor.ID, bot.ID, "ci")
	if err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	c.Agent = "builder"
	var m me
	decode(t, c.API("GET", "/api/v1/me", raw, nil).Body, &m)
	if m.Actor.Name != "ci-bot" {
		t.Fatalf("%+v", m)
	}
}

func TestBadAgentName400(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	c.Agent = "bad/name"
	r := c.API("GET", "/api/v1/me", tok, nil)
	var f struct {
		Error struct{ Code, Message string }
	}
	decode(t, r.Body, &f)
	if r.Status != 400 || f.Error.Code != "bad_request" || f.Error.Message != core.AgentNameRule {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

func TestClaimedTicketNotReady(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	if page := c.Get("/p/VAQ").Body; !strings.Contains(page, `data-card="VAQ-1" data-ready`) {
		t.Fatalf("not ready before the claim:\n%s", page)
	}
	c.Agent = "builder"
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil); r.Status != 200 {
		t.Fatal(r.Status)
	}
	page := c.Get("/p/VAQ").Body
	if strings.Contains(page, `data-card="VAQ-1" data-ready`) || !strings.Contains(page, "data-claim-expires") {
		t.Fatalf("%s", page)
	}
}
