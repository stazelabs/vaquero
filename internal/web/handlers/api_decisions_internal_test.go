package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
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
