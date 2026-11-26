package core_test

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func init() {
	ops = append(ops, coreOp{"MoveTicketToProject",
		func(c *core.Core, f fixture) error {
			_, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0)
			return err
		},
		func(c *core.Core, f fixture) error {
			_, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Project.ID, 0)
			return err
		}})
}

// statusNamed is project's status called name.
func statusNamed(t *testing.T, d dbtest.DB, projectID, name string) gen.Status {
	t.Helper()
	ss, err := d.RQ.ListStatuses(bg, projectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no status %s", name)
	return gen.Status{}
}

func resolve(t *testing.T, c *core.Core, d dbtest.DB, key string) (gen.Ticket, gen.Project) {
	t.Helper()
	k, ok := core.ParseTicketKey(key)
	if !ok {
		t.Fatalf("bad key %s", key)
	}
	tk, p, err := c.ResolveTicket(bg, d.RQ, k)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return tk, p
}

// Review Focus 1: the ticket takes the target's next number, its status,
// milestone and assignee are mapped by name, its old key keeps leading to
// it, and its comments, links, tags and attachments stay attached. One
// ticket.moved event.
func TestMoveTicketToProjectMapsByName(t *testing.T) {
	c, d, f := newFixture(t)
	inProgress := statusNamed(t, d, f.Project.ID, "In progress")
	if _, err := c.CreateMilestone(bg, f.Owner.ID, f.Other.ID, core.MilestoneInput{Name: "m1"}); err != nil {
		t.Fatal(err)
	}
	st, ms, who := inProgress.ID, f.Milestone.ID, f.Owner.ID
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{StatusID: &st, MilestoneID: &ms, AssigneeID: &who}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := attach(c, f.Owner.ID, f.T1.ID, "keeps.png"); err != nil {
		t.Fatal(err)
	}
	before := eventCount(t, d)
	cur, _ := resolve(t, c, d, "VAQ-1")
	moved, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, cur.Version)
	if err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before+1 || lastKind(t, c) != core.EvTicketMoved {
		t.Fatalf("events %d, last %s", eventCount(t, d)-before, lastKind(t, c))
	}
	if moved.ID != f.T1.ID || moved.ProjectID != f.Other.ID || moved.Number != 2 || moved.Version != cur.Version+1 {
		t.Fatalf("%+v", moved)
	}
	if want := statusNamed(t, d, f.Other.ID, "In progress"); moved.StatusID != want.ID {
		t.Fatalf("status %s, want OTH's In progress", moved.StatusID)
	}
	m, err := d.RQ.GetMilestone(bg, moved.MilestoneID.String)
	if err != nil || m.ProjectID != f.Other.ID || m.Name != "m1" {
		t.Fatalf("milestone %+v %v", m, err)
	}
	if moved.AssigneeID.String != f.Owner.ID {
		t.Fatal("assignee lost")
	}
	p := lastPayload(t, c)
	if p["from"] != "VAQ-1" || p["to"] != "OTH-2" || p["from_project_id"] != f.Project.ID {
		t.Fatalf("payload %v", p)
	}
	// The old key resolves to the ticket in its new project.
	tk, pr := resolve(t, c, d, "vaq-001")
	if tk.ID != f.T1.ID || pr.Key != "OTH" {
		t.Fatalf("old key gave %s in %s", tk.ID, pr.Key)
	}
	// Comments, links, tags and attachments stay.
	if n := count(t, d, `SELECT count(*) FROM comments WHERE ticket_id = ?`, f.T1.ID); n != 1 {
		t.Fatalf("comments %d", n)
	}
	if n := count(t, d, `SELECT count(*) FROM ticket_links WHERE to_id = ?`, f.T1.ID); n != 1 {
		t.Fatalf("links %d", n)
	}
	if n := count(t, d, `SELECT count(*) FROM ticket_tags WHERE ticket_id = ? AND tag_id = ?`, f.T1.ID, f.Bug.ID); n != 1 {
		t.Fatalf("tags %d", n)
	}
	if n := count(t, d, `SELECT count(*) FROM attachments WHERE ticket_id = ?`, f.T1.ID); n != 1 {
		t.Fatalf("attachments %d", n)
	}
	// The history keeps everything from before the move, and the move.
	evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: f.T1.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range evs {
		kinds[e.Kind] = true
	}
	for _, k := range []string{core.EvTicketCreated, core.EvCommentCreated, core.EvLinkCreated, core.EvAttachmentAdded, core.EvTicketMoved} {
		if !kinds[k] {
			t.Errorf("history lacks %s: %v", k, kinds)
		}
	}
}

// A status or milestone with no namesake in the target falls back to the
// target's first status of the same category, and to no milestone.
func TestMoveTicketToProjectFallsBack(t *testing.T) {
	c, d, f := newFixture(t)
	extra := statusNamed(t, d, f.Project.ID, "Extra") // a todo status OTH lacks
	st, ms := extra.ID, f.Milestone.ID
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T1.ID, core.TicketPatch{StatusID: &st, MilestoneID: &ms}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateStatus(bg, f.Owner.ID, f.Other.ID, "Later todo", "todo"); err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := statusNamed(t, d, f.Other.ID, "Todo"); moved.StatusID != want.ID {
		t.Fatalf("status %s, want OTH's first todo", moved.StatusID)
	}
	if moved.MilestoneID.Valid {
		t.Fatal("milestone kept with no namesake")
	}
	ch := lastPayload(t, c)["changes"].(map[string]any)
	if ch["status"].(map[string]any)["to"] != "Todo" || ch["milestone"].(map[string]any)["to"] != "" {
		t.Fatalf("changes %v", ch)
	}
}

// Sub-tickets move with their parent, all the way down; a sub-ticket alone
// is refused.
func TestMoveTicketToProjectTakesSubTickets(t *testing.T) {
	c, d, f := newFixture(t)
	child, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Child", ParentID: f.T1.ID})
	if err != nil {
		t.Fatal(err)
	}
	grand, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Grandchild", ParentID: child.ID})
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	_, err = c.MoveTicketToProject(bg, f.Owner.ID, child.ID, f.Other.ID, 0)
	if msg := invalidMsg(err); msg != "Move its parent, or detach it first." {
		t.Fatalf("child alone: %v", err)
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused move wrote")
	}
	before := eventCount(t, d)
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before+1 {
		t.Fatal("not one event")
	}
	for key, want := range map[string]struct {
		id     string
		number int64
	}{"VAQ-3": {child.ID, 3}, "VAQ-4": {grand.ID, 4}, "VAQ-1": {f.T1.ID, 2}} {
		tk, p := resolve(t, c, d, key)
		if tk.ID != want.id || p.Key != "OTH" || tk.Number != want.number {
			t.Errorf("%s: %s %s-%d", key, tk.ID, p.Key, tk.Number)
		}
	}
	// Every re-keyed ticket records when it moved (N-R7).
	if n := count(t, d, `SELECT count(*) FROM ticket_old_keys WHERE moved_at = ?`, timefmt.Format(start)); n != 3 {
		t.Fatalf("%d old keys with moved_at", n)
	}
	g, _ := resolve(t, c, d, "OTH-4")
	if g.ParentID.String != child.ID {
		t.Fatal("parent lost")
	}
	// The sub-tickets' histories show the move too.
	for _, id := range []string{child.ID, grand.ID} {
		evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: id, Valid: true})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(evs, func(e gen.ListTicketEventsRow) bool { return e.Kind == core.EvTicketMoved }) {
			t.Errorf("%s's history lacks the move", id)
		}
	}
	subs := lastPayload(t, c)["sub_tickets"].([]any)
	if len(subs) != 2 {
		t.Fatalf("sub_tickets %v", subs)
	}
}

// Refusals: the same project, an archived target, a stale version, an open
// run (the hook), a missing project.
func TestMoveTicketToProjectRefusals(t *testing.T) {
	c, d, f := newFixture(t)
	snap := snapshot(t, d)
	_, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Project.ID, 0)
	if !strings.Contains(invalidMsg(err), "already in VAQ") {
		t.Fatalf("same project: %v", err)
	}
	var conflict *core.Conflict
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, f.T1.Version+5); !errors.As(err, &conflict) || conflict.Key != "VAQ-1" {
		t.Fatalf("stale: %v", err)
	}
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, "no-such-project", 0); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	restore := core.SetInOpenRun(func(context.Context, *gen.Queries, []string) (bool, error) { return true, nil })
	_, err = c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0)
	restore()
	if !strings.Contains(invalidMsg(err), "open run") {
		t.Fatalf("open run: %v", err)
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused move wrote")
	}
	if err := c.SetProjectArchived(bg, f.Owner.ID, f.Other.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); !strings.Contains(invalidMsg(err), "Restore OTH") {
		t.Fatalf("archived: %v", err)
	}
}

// Moving back takes a fresh number; every old key still leads to it.
func TestMoveTicketBackTakesAFreshNumber(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameTag(bg, f.Owner.ID, f.Other.ID, f.Bug.ID, "defect"); err != nil {
		t.Fatal(err)
	}
	back, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Project.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if back.ProjectID != f.Project.ID || back.Number != 3 {
		t.Fatalf("back as %d", back.Number)
	}
	for _, key := range []string{"VAQ-1", "OTH-2", "VAQ-3"} {
		if tk, p := resolve(t, c, d, key); tk.ID != f.T1.ID || p.Key != "VAQ" {
			t.Errorf("%s: %s in %s", key, tk.ID, p.Key)
		}
	}
	if n := count(t, d, `SELECT count(*) FROM ticket_old_keys WHERE ticket_id = ?`, f.T1.ID); n != 2 {
		t.Fatalf("old keys %d", n)
	}
	// The tag rename in OTH, while the ticket was there, stays in its
	// history.
	evs, err := d.RQ.ListTicketEvents(bg, sql.NullString{String: f.T1.ID, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(evs, func(e gen.ListTicketEventsRow) bool { return e.Kind == core.EvTagRenamed }) {
		t.Fatal("history lacks the tag rename in OTH")
	}
}

// A claim's earlier status is mapped too, so a lapse returns the ticket to
// a status of its new project.
func TestMoveTicketToProjectMapsTheClaim(t *testing.T) {
	c, d, f := newFixture(t)
	a, err := c.EnsureAgent(bg, f.Owner.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Claim(bg, a.ID, f.T1.ID, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	moved, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ClaimedBy.String != a.ID {
		t.Fatal("claim dropped")
	}
	if want := statusNamed(t, d, f.Other.ID, "Todo"); moved.ClaimPrevStatusID.String != want.ID {
		t.Fatalf("claim's earlier status %s", moved.ClaimPrevStatusID.String)
	}
}

// N-R2: an open decision naming the ticket stays in the old project, the
// move says so, and the decision can no longer approve it.
func TestMoveTicketToProjectLeavesDecisions(t *testing.T) {
	c, d, f := newFixture(t)
	dec, err := c.AskDecision(bg, f.Owner.ID, core.DecisionInput{ProjectID: f.Project.ID, TicketIDs: []string{f.T1.ID},
		Kind: core.DecisionApproval, Subject: "plan", Question: "Approve?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if n, _ := lastPayload(t, c)["open_decisions"].(float64); n != 1 {
		t.Fatalf("payload %v", lastPayload(t, c))
	}
	got, err := d.RQ.GetDecision(bg, dec.ID)
	if err != nil || got.ProjectID != f.Project.ID || got.State != "open" {
		t.Fatalf("%+v %v", got, err)
	}
	_, err = c.AnswerDecision(bg, f.Owner.ID, dec.ID, core.Answer{Option: core.ApproveLabel})
	if !strings.Contains(invalidMsg(err), "moved to another project") {
		t.Fatalf("approve after the move: %v", err)
	}
}

// Deleting the old project keeps the moved ticket and its history; deleting
// the new one removes its old keys. Either way no foreign key is left
// dangling.
func TestDeleteProjectWithMovedTickets(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	// O1 goes the other way, so VAQ holds an old key of OTH's and a
	// ticket with an old key in OTH.
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.O1.ID, f.Project.ID, 0); err != nil {
		t.Fatal(err)
	}
	history := count(t, d, `SELECT count(*) FROM events WHERE ticket_id = ?`, f.T1.ID)
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "VAQ"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM ticket_old_keys`); n != 0 {
		t.Fatalf("old keys left: %d", n)
	}
	if n := count(t, d, `SELECT count(*) FROM events WHERE ticket_id = ?`, f.T1.ID); n != history {
		t.Fatalf("moved ticket's history %d, want %d", n, history)
	}
	if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Fatalf("%d dangling foreign keys", n)
	}
	tk, p := resolve(t, c, d, "OTH-2")
	if tk.ID != f.T1.ID || p.Key != "OTH" {
		t.Fatal("moved ticket gone")
	}
	if err := c.DeleteProject(bg, f.Owner.ID, f.Other.ID, "OTH"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Fatalf("%d dangling foreign keys", n)
	}
}

// A link made by a moved ticket's old key records the key it has now.
func TestLinkByOldKeyRecordsTheKeyNow(t *testing.T) {
	c, _, f := newFixture(t)
	if _, err := c.MoveTicketToProject(bg, f.Owner.ID, f.T1.ID, f.Other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddLink(bg, f.Owner.ID, f.O1.ID, "blocks", core.TicketKey{Project: "VAQ", Number: 1}); err != nil {
		t.Fatal(err)
	}
	if p := lastPayload(t, c); p["to"] != "OTH-2" || p["to_id"] != f.T1.ID {
		t.Fatalf("%v", p)
	}
}

// Ruling N-R6, in core: an agent can't flip the workflow, change the tier
// map, or move a ticket out of the workflow; the owner can.
func TestWorkflowGateIsTheOwners(t *testing.T) {
	c, _, f := newFixture(t)
	a, err := c.EnsureAgent(bg, f.Owner.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	on, tm := true, `{"claude":{"light":"small"}}`
	if _, err := c.UpdateProject(bg, a.ID, f.Project.ID, core.ProjectPatch{Workflow: &on}); invalidMsg(err) != "Only the owner turns the agent workflow on or off." {
		t.Fatalf("workflow: %v", err)
	}
	if _, err := c.UpdateProject(bg, a.ID, f.Project.ID, core.ProjectPatch{TierMap: &tm}); invalidMsg(err) != "Only the owner changes the tier map." {
		t.Fatalf("tier map: %v", err)
	}
	if _, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{Workflow: &on, TierMap: &tm}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MoveTicketToProject(bg, a.ID, f.T1.ID, f.Other.ID, 0); invalidMsg(err) != "Only the owner moves a ticket out of a project with the agent workflow on." {
		t.Fatalf("move out: %v", err)
	}
	if _, err := c.MoveTicketToProject(bg, a.ID, f.O1.ID, f.Project.ID, 0); err != nil {
		t.Fatalf("move in: %v", err)
	}
}
