package handlers_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// statusIn is the id of project p's status called name.
func statusIn(t *testing.T, e *webtest.Env, p gen.Project, name string) string {
	t.Helper()
	for _, s := range statuses(t, e, p) {
		if s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no status %s in %s", name, p.Key)
	return ""
}

// Ruling T-R3: My work is the owner's open assigned tickets across the
// non-archived projects, grouped by project, priority then newest change.
func TestMyWork(t *testing.T) {
	e := webtest.New(t)
	if body := e.Client().Get("/my").Body; !strings.Contains(body, "Nothing assigned to you.") {
		t.Fatal("no empty state")
	}
	owner := e.Deps.Actor.ID
	mk := func(key, name string) gen.Project {
		p, err := e.Deps.Core.CreateProject(bg, owner, core.ProjectInput{Key: key, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	beta, alpha, old := mk("BET", "Beta"), mk("ALP", "Alpha"), mk("OLD", "Old")
	agent, err := e.Deps.Core.EnsureAgent(bg, owner, "claude")
	if err != nil {
		t.Fatal(err)
	}
	ticket(t, e, alpha, core.TicketInput{Title: "alpha no priority", AssigneeID: owner, StatusID: statusIn(t, e, alpha, "In progress")})
	ticket(t, e, alpha, core.TicketInput{Title: "alpha urgent", AssigneeID: owner, Priority: 1})
	ticket(t, e, alpha, core.TicketInput{Title: "alpha shipped", AssigneeID: owner, StatusID: statusIn(t, e, alpha, "Done")})
	ticket(t, e, alpha, core.TicketInput{Title: "alpha dropped", AssigneeID: owner, StatusID: statusIn(t, e, alpha, "Cancelled")})
	ticket(t, e, alpha, core.TicketInput{Title: "alpha the agent's", AssigneeID: agent.ID})
	ticket(t, e, alpha, core.TicketInput{Title: "alpha nobody's"})
	ticket(t, e, beta, core.TicketInput{Title: "beta high", AssigneeID: owner, Priority: 2})
	ticket(t, e, old, core.TicketInput{Title: "old archived", AssigneeID: owner})
	if err := e.Deps.Core.SetProjectArchived(bg, owner, old.ID, true); err != nil {
		t.Fatal(err)
	}

	page := e.Client().Get("/my").Body
	contains(t, page, "<h1", "My work", `data-card="ALP-2"`, `data-card="ALP-1"`, `data-card="BET-1"`, "In progress", `href="/p/ALP"`, `href="/p/BET"`)
	lacks(t, page, "Nothing assigned to you.", "alpha shipped", "alpha dropped", "alpha the agent", "alpha nobody", "old archived")
	// Alpha before Beta; within Alpha the urgent one first.
	order := []string{`data-my-project="ALP"`, "alpha urgent", "alpha no priority", `data-my-project="BET"`, "beta high"}
	pos := -1
	for _, s := range order {
		i := strings.Index(page, s)
		if i <= pos {
			t.Fatalf("%q out of order (at %d, after %d)", s, i, pos)
		}
		pos = i
	}
	// Within one priority the newest change comes first.
	var newer gen.Ticket
	at(e, 2*time.Hour, func() { ticket(t, e, beta, core.TicketInput{Title: "beta tie older", AssigneeID: owner, Priority: 3}) })
	at(e, time.Hour, func() {
		newer = ticket(t, e, beta, core.TicketInput{Title: "beta tie newer", AssigneeID: owner, Priority: 3})
	})
	page = e.Client().Get("/my").Body
	if i, j := strings.Index(page, "beta tie newer"), strings.Index(page, "beta tie older"); i < 0 || j < 0 || i > j {
		t.Fatalf("the tie is out of order: newer at %d, older at %d", i, j)
	}
	// A claim shows beside the status.
	if _, err := e.Deps.Core.Claim(bg, owner, newer.ID, 0); err != nil {
		t.Fatal(err)
	}
	page = e.Client().Get("/my").Body
	row := page[strings.Index(page, `data-card="`+"BET-3"+`"`):]
	row = row[:strings.Index(row, "</li>")]
	contains(t, row, "In progress", `title="Claimed by `, "data-claim-expires=")

	// The sidebar marks My work.
	contains(t, sidebar(t, page), `href="/my" data-current aria-current="page"`)
}
