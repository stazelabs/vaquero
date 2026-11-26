package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// VAQ-145: a page of decisions is read in a fixed number of queries,
// whatever its size: not a read per decision for its project, people,
// tickets, approvals and what a plan approval asked.
func TestDecisionsListBatched(t *testing.T) {
	count := func(n int) int {
		db := dbtest.New(t)
		now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
		clock := func() time.Time { now = now.Add(time.Second); return now }
		c := core.New(db.W, clock)
		ctx := context.Background()
		owner, err := c.EnsureOwner(ctx)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := c.EnsureAgent(ctx, owner.ID, "builder")
		if err != nil {
			t.Fatal(err)
		}
		var projects []gen.Project
		for _, k := range []string{"VAQ", "OPS"} {
			p, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: k, Name: k})
			if err != nil {
				t.Fatal(err)
			}
			projects = append(projects, p)
		}
		for i := range n {
			p := projects[i%2]
			epic, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: fmt.Sprintf("Epic %d", i)})
			if err != nil {
				t.Fatal(err)
			}
			for j := range 2 {
				if _, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: fmt.Sprintf("Child %d.%d", i, j), ParentID: epic.ID}); err != nil {
					t.Fatal(err)
				}
			}
			by := owner.ID
			if i%3 == 0 {
				by = agent.ID
			}
			dec, err := c.AskDecision(ctx, by, core.DecisionInput{
				ProjectID: p.ID, TicketIDs: []string{epic.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?",
			})
			if err != nil {
				t.Fatal(err)
			}
			if i%2 == 0 {
				a := core.Answer{Option: core.ApproveLabel}
				if i%4 == 0 {
					a.RelayedBy, a.Words = agent.ID, "Approve."
				}
				if _, err := c.AnswerDecision(ctx, owner.ID, dec.ID, a); err != nil {
					t.Fatal(err)
				}
			}
		}
		cdb := &countingDB{DBTX: db.R}
		d := Deps{R: gen.New(cdb), Logger: slog.New(slog.DiscardHandler), Now: clock}
		w := httptest.NewRecorder()
		APIDecisions(d)(w, httptest.NewRequest("GET", "/api/v1/decisions?limit=500", nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var out []api.Decision
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != n {
			t.Fatalf("%d decisions, want %d", len(out), n)
		}
		// Each item is what the decision reads alone.
		answered := map[string]int{}
		for _, item := range out {
			answered[item.Answered]++
			dec, err := db.RQ.GetDecision(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			one, err := decisionDTO(ctx, Deps{R: db.RQ}, dec)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := json.Marshal(one)
			b, _ := json.Marshal(item)
			if string(a) != string(b) {
				t.Fatalf("list item\n%s\nalone\n%s", b, a)
			}
			if len(item.Asked) != 1 || len(item.Asked[0].Children) != 2 || item.AskedBy == "" {
				t.Fatalf("%+v", item)
			}
		}
		if answered["direct"] == 0 || answered["relayed"] == 0 || answered[""] == 0 {
			t.Fatalf("answered %v", answered)
		}
		return cdb.n
	}
	few, many := count(5), count(50)
	if few != many {
		t.Fatalf("%d queries for 5 decisions, %d for 50", few, many)
	}
	// The page, then its projects, people, ticket keys, approvals, what
	// the plan approvals asked, and the tickets those name.
	if many != 7 {
		t.Fatalf("%d queries, want 7", many)
	}
}

// VAQ-145: a plan approval's answer, written out by hand, through the list
// and alone: the children it asked about in number order, less one deleted
// since, and nothing else the batch could mix up.
func TestDecisionAnswerGolden(t *testing.T) {
	db := dbtest.New(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	c := core.New(db.W, func() time.Time { now = now.Add(time.Second); return now })
	ctx := context.Background()
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := c.EnsureAgent(ctx, owner.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	epic, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: "Board"})
	if err != nil {
		t.Fatal(err)
	}
	var kids []gen.Ticket
	for _, title := range []string{"Columns", "Rails", "Filters"} {
		k, err := c.CreateTicket(ctx, owner.ID, p.ID, core.TicketInput{Title: title, ParentID: epic.ID})
		if err != nil {
			t.Fatal(err)
		}
		kids = append(kids, k)
	}
	if _, err := c.SavePlan(ctx, owner.ID, epic.ID, "1. Columns.", 0); err != nil {
		t.Fatal(err)
	}
	dec, err := c.AskDecision(ctx, agent.ID, core.DecisionInput{
		ProjectID: p.ID, TicketIDs: []string{epic.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A question the owner answered, so the list holds more than one.
	q, err := c.AskDecision(ctx, owner.ID, core.DecisionInput{ProjectID: p.ID, Kind: core.DecisionQuestion, Question: "Which order?",
		Options: []core.Option{{Label: "By age"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerDecision(ctx, owner.ID, q.ID, core.Answer{Option: "By age"}); err != nil {
		t.Fatal(err)
	}
	// Rails is gone: there is no core operation for it, so straight in.
	rails := kids[1].ID
	for _, stmt := range []string{
		"DELETE FROM event_tickets WHERE seq IN (SELECT seq FROM events WHERE ticket_id = ?)",
		"DELETE FROM events WHERE ticket_id = ?",
		"DELETE FROM tickets WHERE id = ?",
	} {
		if _, err := db.W.ExecContext(ctx, stmt, rails); err != nil {
			t.Fatal(err)
		}
	}
	want := `{"id":"` + dec.ID + `","project":"VAQ","tickets":["VAQ-1"],"kind":"approval","subject":"plan",` +
		`"question":"Approve the plan?","options":[{"label":"Approve"},{"label":"Request changes"}],"approves":"Approve",` +
		`"state":"open","asked_by":"builder","asked_at":"` + dec.AskedAt + `",` +
		`"asked":[{"ticket":"VAQ-1","plan_revision":1,"children":[{"key":"VAQ-2","title":"Columns"},{"key":"VAQ-4","title":"Filters"}]}]}`

	d := Deps{R: db.RQ, Logger: slog.New(slog.DiscardHandler)}
	one, err := decisionDTO(ctx, d, dec)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(one); string(b) != want {
		t.Errorf("alone\n%s\nwant\n%s", b, want)
	}
	w := httptest.NewRecorder()
	APIDecisions(d)(w, httptest.NewRequest("GET", "/api/v1/decisions", nil))
	var list []json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var found bool
	for _, item := range list {
		var probe struct{ ID string }
		_ = json.Unmarshal(item, &probe)
		var buf bytes.Buffer
		if err := json.Compact(&buf, item); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if probe.ID == dec.ID {
			found = true
			if got != want {
				t.Errorf("listed\n%s\nwant\n%s", got, want)
			}
		} else if !strings.Contains(got, `"answered_by":"Owner"`) || !strings.Contains(got, `"answered":"direct"`) {
			t.Errorf("question %s", got)
		}
	}
	if !found {
		t.Fatalf("%s", w.Body)
	}
}
