package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
)

var ctx = context.Background()

type world struct {
	d     dbtest.DB
	c     *core.Core
	owner gen.Actor
	p     gen.Project
	st    map[string]gen.Status // by name
}

func newWorld(t *testing.T) world {
	t.Helper()
	d := dbtest.New(t)
	c := core.New(d.W, func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) })
	owner, err := c.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProject(ctx, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := d.RQ.ListStatuses(ctx, p.ID)
	w := world{d: d, c: c, owner: owner, p: p, st: map[string]gen.Status{}}
	for _, s := range ss {
		w.st[s.Name] = s
	}
	return w
}

func (w world) ticket(t *testing.T, in core.TicketInput) gen.Ticket {
	t.Helper()
	tk, err := w.c.CreateTicket(ctx, w.owner.ID, w.p.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestStatusChangedAtBackfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.db")
	w, err := appdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := appdb.MigrateTo(w, 2); err != nil {
		t.Fatal(err)
	}
	q := gen.New(w)
	if _, err := w.Exec(`INSERT INTO actors (id, kind, name, created_at) VALUES ('a', 'human', 'Owner', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES ('p', 'VAQ', 'V', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO statuses (id, project_id, name, category, position) VALUES ('s', 'p', 'Todo', 'todo', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Exec(`INSERT INTO tickets (id, project_id, number, title, status_id, position, created_by, created_at, updated_at)
		VALUES ('t', 'p', 1, 'Old', 's', 1024, 'a', '2026-01-01T00:00:00Z', '2026-02-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := appdb.Migrate(w); err != nil {
		t.Fatal(err)
	}
	tk, err := q.GetTicket(ctx, "t")
	if err != nil || tk.StatusChangedAt != "2026-02-02T00:00:00Z" {
		t.Fatalf("%q %v", tk.StatusChangedAt, err)
	}
	hits, err := appdb.SearchTickets(ctx, w, `"old"*`, 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("backfilled index: %d %v", len(hits), err)
	}
}

func search(t *testing.T, w world, query string) []appdb.SearchHit {
	t.Helper()
	hits, err := appdb.SearchTickets(ctx, w.d.R, query, 50)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestSearchIndexFollowsWrites(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(t, core.TicketInput{Title: "Build the board", Body: "drag cards"})
	if n := len(search(t, w, `"drag"*`)); n != 1 {
		t.Fatalf("body: %d", n)
	}
	in := core.TicketInput{Title: "Render columns", Body: "drag cards", StatusID: tk.StatusID}
	if _, err := w.c.UpdateTicket(ctx, w.owner.ID, tk.ID, in); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, w, `"board"*`)); n != 0 {
		t.Fatalf("old title still matches: %d", n)
	}
	if hits := search(t, w, `"columns"*`); len(hits) != 1 || hits[0].Number != 1 || hits[0].ProjectKey != "VAQ" {
		t.Fatalf("new title: %+v", hits)
	}
	cm, err := w.c.AddComment(ctx, w.owner.ID, tk.ID, "Sortable is vendored")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, w, `"vendored"*`)); n != 1 {
		t.Fatalf("comment: %d", n)
	}
	if err := w.c.DeleteComment(ctx, w.owner.ID, cm.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, w, `"vendored"*`)); n != 0 {
		t.Fatalf("deleted comment still matches: %d", n)
	}
}

func TestSearchIgnoresAccents(t *testing.T) {
	w := newWorld(t)
	w.ticket(t, core.TicketInput{Title: "Café menu"})
	if n := len(search(t, w, `"cafe"*`)); n != 1 {
		t.Fatalf("%d", n)
	}
}

func TestSearchTicketOnceWhenTitleAndCommentMatch(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(t, core.TicketInput{Title: "Board work"})
	if _, err := w.c.AddComment(ctx, w.owner.ID, tk.ID, "board again"); err != nil {
		t.Fatal(err)
	}
	if n := len(search(t, w, `"board"*`)); n != 1 {
		t.Fatalf("%d hits", n)
	}
}

func board(t *testing.T, w world, p gen.ListBoardTicketsParams) []gen.ListBoardTicketsRow {
	t.Helper()
	p.ProjectID = w.p.ID
	rows, err := w.d.RQ.ListBoardTickets(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func ns(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func TestBoardFilters(t *testing.T) {
	w := newWorld(t)
	m, err := w.c.CreateMilestone(ctx, w.owner.ID, w.p.ID, core.MilestoneInput{Name: "Beta"})
	if err != nil {
		t.Fatal(err)
	}
	w.ticket(t, core.TicketInput{Title: "a", Tags: []string{"UI"}, Priority: 2})
	w.ticket(t, core.TicketInput{Title: "b", AssigneeID: w.owner.ID, MilestoneID: m.ID})
	w.ticket(t, core.TicketInput{Title: "c"})
	if n := len(board(t, w, gen.ListBoardTicketsParams{})); n != 3 {
		t.Fatalf("all: %d", n)
	}
	for name, tc := range map[string]struct {
		p    gen.ListBoardTicketsParams
		want string
	}{
		"tag":         {gen.ListBoardTicketsParams{Tag: ns("ui")}, "a"},
		"priority":    {gen.ListBoardTicketsParams{Priority: sql.NullInt64{Int64: 2, Valid: true}}, "a"},
		"assignee":    {gen.ListBoardTicketsParams{AssigneeID: ns(w.owner.ID)}, "b"},
		"kind":        {gen.ListBoardTicketsParams{AssigneeKind: ns("human")}, "b"},
		"milestone":   {gen.ListBoardTicketsParams{MilestoneID: ns(m.ID)}, "b"},
		"noMilestone": {gen.ListBoardTicketsParams{NoMilestone: true, Tag: ns("ui")}, "a"},
	} {
		rows := board(t, w, tc.p)
		if len(rows) != 1 || rows[0].Title != tc.want {
			t.Errorf("%s: %d rows", name, len(rows))
		}
	}
	if n := len(board(t, w, gen.ListBoardTicketsParams{AssigneeKind: ns("agent")})); n != 0 {
		t.Fatalf("agent: %d", n)
	}
}

func TestReadyFlag(t *testing.T) {
	w := newWorld(t)
	ready := w.ticket(t, core.TicketInput{Title: "ready"})
	w.ticket(t, core.TicketInput{Title: "assigned", AssigneeID: w.owner.ID})
	blocked := w.ticket(t, core.TicketInput{Title: "blocked"})
	blocker := w.ticket(t, core.TicketInput{Title: "blocker"})
	if _, err := w.c.AddLink(ctx, w.owner.ID, blocker.ID, "blocks", core.TicketKey{Project: "VAQ", Number: blocked.Number}); err != nil {
		t.Fatal(err)
	}
	parent := w.ticket(t, core.TicketInput{Title: "parent"})
	w.ticket(t, core.TicketInput{Title: "child", ParentID: parent.ID})
	w.ticket(t, core.TicketInput{Title: "doing", StatusID: w.st["In progress"].ID})
	_ = ready
	want := map[string]bool{"ready": true, "assigned": false, "blocked": false, "blocker": true, "parent": false, "child": true, "doing": false}
	for _, r := range board(t, w, gen.ListBoardTicketsParams{}) {
		if r.Ready != want[r.Title] {
			t.Errorf("%s ready=%v", r.Title, r.Ready)
		}
	}
	// Once the blocker is done, the blocked ticket is ready.
	in := core.TicketInput{Title: "blocker", StatusID: w.st["Done"].ID}
	if _, err := w.c.UpdateTicket(ctx, w.owner.ID, blocker.ID, in); err != nil {
		t.Fatal(err)
	}
	for _, r := range board(t, w, gen.ListBoardTicketsParams{}) {
		if r.Title == "blocked" && !r.Ready {
			t.Error("blocked ticket not ready after its blocker is done")
		}
	}
}

func TestMilestoneProgress(t *testing.T) {
	w := newWorld(t)
	m, _ := w.c.CreateMilestone(ctx, w.owner.ID, w.p.ID, core.MilestoneInput{Name: "Beta"})
	for _, s := range []string{"Done", "Done", "Todo", "Cancelled"} {
		w.ticket(t, core.TicketInput{Title: s, StatusID: w.st[s].ID, MilestoneID: m.ID})
	}
	w.ticket(t, core.TicketInput{Title: "loose"})
	w.ticket(t, core.TicketInput{Title: "loose done", StatusID: w.st["Done"].ID})
	rows, err := w.d.RQ.ListMilestoneProgress(ctx, w.p.ID)
	if err != nil || len(rows) != 1 || rows[0].Done != 2 || rows[0].Total != 3 {
		t.Fatalf("%+v %v", rows, err)
	}
	n, err := w.d.RQ.CountOpenWithoutMilestone(ctx, w.p.ID)
	if err != nil || n != 1 {
		t.Fatalf("no milestone open: %d %v", n, err)
	}
}

var _ = idgen.New
