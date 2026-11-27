package readiness_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
)

var bg = context.Background()

// world is a workflow project on a fresh database whose clock moves a
// second each time it is read, so answers are ordered.
type world struct {
	t     *testing.T
	c     *core.Core
	d     dbtest.DB
	owner gen.Actor
	p     gen.Project
	files readiness.Files // nil: pins unchecked
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d := dbtest.New(t)
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	c := core.New(d.W, func() time.Time { now = now.Add(time.Second); return now })
	owner, err := c.EnsureOwner(bg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.CreateProject(bg, owner.ID, core.ProjectInput{Key: "VAQ", Name: "Vaquero"})
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if p, err = c.UpdateProject(bg, owner.ID, p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		t.Fatal(err)
	}
	return &world{t: t, c: c, d: d, owner: owner, p: p}
}

func (w *world) ticket(in core.TicketInput) gen.Ticket {
	w.t.Helper()
	tk, err := w.c.CreateTicket(bg, w.owner.ID, w.p.ID, in)
	if err != nil {
		w.t.Fatal(err)
	}
	return tk
}

func (w *world) plan(tk gen.Ticket, body string, base int64) {
	w.t.Helper()
	if _, err := w.c.SavePlan(bg, w.owner.ID, tk.ID, body, base); err != nil {
		w.t.Fatal(err)
	}
}

// answer asks a plan approval of the tickets and answers it with option.
func (w *world) answer(option string, tks ...gen.Ticket) {
	w.t.Helper()
	ids := make([]string, len(tks))
	for i, tk := range tks {
		ids[i] = tk.ID
	}
	d, err := w.c.AskDecision(bg, w.owner.ID, core.DecisionInput{
		ProjectID: w.p.ID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?",
	})
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.c.AnswerDecision(bg, w.owner.ID, d.ID, core.Answer{Option: option}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) approve(tks ...gen.Ticket) { w.t.Helper(); w.answer(core.ApproveLabel, tks...) }

func (w *world) edit(tk gen.Ticket, patch core.TicketPatch) {
	w.t.Helper()
	if _, err := w.c.PatchTicket(bg, w.owner.ID, tk.ID, patch, 0); err != nil {
		w.t.Fatal(err)
	}
}

// check is tk's readiness, through Check and through CheckAll, which must
// agree.
func (w *world) check(tk gen.Ticket) readiness.Readiness {
	w.t.Helper()
	cur, err := w.d.RQ.GetTicket(bg, tk.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	one, err := readiness.Check(bg, w.d.RQ, w.files, cur)
	if err != nil {
		w.t.Fatal(err)
	}
	all, err := readiness.CheckAll(bg, w.d.RQ, w.files, []string{tk.ID})
	if err != nil {
		w.t.Fatal(err)
	}
	if got := all[tk.ID]; got.Ready != one.Ready || !slices.Equal(got.Reasons, one.Reasons) || !slices.Equal(got.Files, one.Files) ||
		got.Action != one.Action || got.Epic != one.Epic {
		w.t.Fatalf("Check %+v, CheckAll %+v", one, got)
	}
	return one
}

// expect checks got's reasons: none is ready.
func expect(t *testing.T, got readiness.Readiness, reasons ...string) {
	t.Helper()
	if got.Ready != (len(reasons) == 0) || !slices.Equal(got.Reasons, reasons) || got.Reasons == nil {
		t.Fatalf("got %+v, want reasons %q", got, reasons)
	}
}

// want is what a readiness answer should say: its one reason ("" for
// ready), its action and the epic to ask on.
type want struct{ reason, action, epic string }

var ready = want{}

// ask is "Ask for plan approval.", on epic ("" for the ticket itself).
func ask(epic string) want { return want{readiness.AskForApproval, readiness.ActionAsk, epic} }

// again is reason with "ask again", on epic.
func again(reason, epic string) want { return want{reason, readiness.ActionAskAgain, epic} }

func (w *world) expect(tk gen.Ticket, x want) {
	w.t.Helper()
	got := w.check(tk)
	var reasons []string
	if x.reason != "" {
		reasons = []string{x.reason}
	}
	if got.Ready != (x.reason == "") || !slices.Equal(got.Reasons, reasons) && !(len(reasons) == 0 && len(got.Reasons) == 0) ||
		got.Reasons == nil || got.Action != x.action || got.Epic != x.epic {
		w.t.Fatalf("%s: got %+v, want %+v", w.key(tk), got, x)
	}
}

// key is tk's key in VAQ.
func (w *world) key(tk gen.Ticket) string {
	return core.TicketKey{Project: w.p.Key, Number: tk.Number}.String()
}

func TestReasons(t *testing.T) {
	forms := []string{"Ask for plan approval.", "Changed since approval: {what}. Ask again.",
		"Its epic changed since approval: {what}. Ask again.", "Can't check its files: {why}."}
	if !slices.Equal(readiness.Reasons, forms) {
		t.Fatalf("Reasons %q", readiness.Reasons)
	}
	if got := readiness.Changed(readiness.WhatDescription); got != "Changed since approval: the description. Ask again." {
		t.Fatal(got)
	}
	if got := readiness.EpicChanged(readiness.WhatParent); got != "Its epic changed since approval: its parent. Ask again." {
		t.Fatal(got)
	}
	if got := readiness.Unchecked(readiness.WhyNoRepo); got != "Can't check its files: no repository is set." {
		t.Fatal(got)
	}
}

func TestCheckAllEmpty(t *testing.T) {
	w := newWorld(t)
	all, err := readiness.CheckAll(bg, w.d.RQ, w.files, nil)
	if err != nil || len(all) != 0 {
		t.Fatalf("%v %v", all, err)
	}
}

// status moves tk to the project's first status of category.
func (w *world) status(tk gen.Ticket, category string) {
	w.t.Helper()
	st, err := w.d.RQ.FirstStatusInCategory(bg, gen.FirstStatusInCategoryParams{ProjectID: w.p.ID, Category: category})
	if err != nil {
		w.t.Fatal(err)
	}
	w.edit(tk, core.TicketPatch{StatusID: &st.ID})
}

func (w *world) parent(tk gen.Ticket, parentID string) {
	w.t.Helper()
	w.edit(tk, core.TicketPatch{ParentID: &parentID})
}

// other is the project OTH, workflow on, made on first use.
func (w *world) other() gen.Project {
	w.t.Helper()
	p, err := w.d.RQ.GetProjectByKey(bg, "OTH")
	if err == nil {
		return p
	}
	if p, err = w.c.CreateProject(bg, w.owner.ID, core.ProjectInput{Key: "OTH", Name: "Other"}); err != nil {
		w.t.Fatal(err)
	}
	on := true
	if p, err = w.c.UpdateProject(bg, w.owner.ID, p.ID, core.ProjectPatch{Workflow: &on}); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) moveTo(tk gen.Ticket, p gen.Project) {
	w.t.Helper()
	if _, err := w.c.MoveTicketToProject(bg, w.owner.ID, tk.ID, p.ID, 0); err != nil {
		w.t.Fatal(err)
	}
}

// Ready is the board's ready and a current approval: the tier gates
// nothing, and a ticket that isn't a todo is not ready, with no reason.
func TestReadyRule(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world) gen.Ticket
		want  want
	}{
		{"approved plan", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns", Tier: "standard"})
			w.plan(tk, "1. Columns.", 0)
			w.approve(tk)
			return tk
		}, ready},
		{"no tier", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.plan(tk, "1. Columns.", 0)
			w.approve(tk)
			return tk
		}, ready},
		{"design tier", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns", Tier: "design"})
			w.approve(tk)
			return tk
		}, ready},
		{"changes outside the requirement", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.approve(tk)
			w.edit(tk, core.TicketPatch{Priority: new(int64(2)), Tags: []string{"ui"}, SetTags: true, Tier: new("heavy")})
			return tk
		}, ready},
		{"nothing asked", func(w *world) gen.Ticket {
			return w.ticket(core.TicketInput{Title: "Columns"})
		}, ask("")},
		{"plan not approved", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.plan(tk, "1. Columns.", 0)
			return tk
		}, ask("")},
		{"backlog", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.status(tk, "backlog")
			return tk
		}, ready},
		{"done, approved", func(w *world) gen.Ticket {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.approve(tk)
			w.status(tk, "done")
			return tk
		}, ready},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tk := tc.setup(w)
			got := w.check(tk)
			cur, _ := w.d.RQ.GetTicket(bg, tk.ID)
			if st, _ := w.d.RQ.GetStatus(bg, cur.StatusID); st.Category != "todo" {
				if got.Ready || len(got.Reasons) != 0 || got.Action != "" || got.Epic != "" {
					t.Fatalf("not a todo: %+v", got)
				}
				return
			}
			w.expect(tk, tc.want)
		})
	}
}

// Which approval counts: its own with a plan or an approval asked naming
// it; else the nearest ancestor with a plan, which must list it.
func TestWhichApprovalCounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(w *world) (gen.Ticket, string) // the ticket, and the epic's key when asking there
		want  func(epic string) want
	}{
		{"own approval, no plan, no parent", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Typo"})
			w.approve(tk)
			return tk, ""
		}, func(string) want { return ready }},
		{"own approval under an unapproved epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Rails.", 0)
			c := w.ticket(core.TicketInput{Title: "Rails", ParentID: e.ID})
			w.approve(c)
			return c, ""
		}, func(string) want { return ready }},
		{"covered by its epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			return c, w.key(e)
		}, func(string) want { return ready }},
		{"epic not approved", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			return c, w.key(e)
		}, ask},
		{"epic's changes requested", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.answer("Request changes", e)
			return c, w.key(e)
		}, ask},
		{"made under the epic after its approval", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			w.approve(e)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			return c, w.key(e)
		}, ask},
		// Ruling R-Q12: an ancestor with an answered plan approval naming
		// it is where to look, plan or not. Made after that approval, the
		// child isn't among the descendants it covered: ask on the epic.
		{"no ancestor with a plan", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.approve(e)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			return c, w.key(e)
		}, ask},
		{"plan-less root approved: its child", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.ticket(core.TicketInput{Title: "Sorting", ParentID: c.ID})
			w.approve(e)
			return c, w.key(e)
		}, func(string) want { return ready }},
		{"plan-less root approved: its grandchild", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Sorting", ParentID: c.ID})
			w.approve(e)
			return g, w.key(e)
		}, func(string) want { return ready }},
		{"plan-less epic, no approval anywhere", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			return c, ""
		}, ask},
		{"own changes requested", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.answer("Request changes", c) // asked naming it: its own answer counts
			return c, ""
		}, ask},
		// Review focus 1: a ticket with its own plan under an approved
		// epic is not covered by the epic.
		{"own plan under an approved epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.plan(c, "1. Sorted columns.", 0)
			w.approve(e)
			return c, ""
		}, ask},
		// Review focus 2: a grandchild with no plan is covered, with no
		// approval on the parent between.
		{"grandchild covered", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			return g, w.key(e)
		}, func(string) want { return ready }},
		{"grandchild under a planned child", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			w.plan(c, "1. Grandchild.", 0)
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			return g, w.key(c)
		}, ask},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tk, epic := tc.setup(w)
			w.expect(tk, tc.want(epic))
		})
	}
}

// Current: approved, and nothing it pinned changed since; a cancelled
// ancestor covers nothing, a done one still does.
func TestApprovalCurrent(t *testing.T) {
	desc := readiness.Changed(readiness.WhatDescription)
	for _, tc := range []struct {
		name  string
		setup func(w *world) (gen.Ticket, string)
		want  func(epic string) want
	}{
		{"description edited", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Columns", Body: "Show columns."})
			w.approve(tk)
			w.edit(tk, core.TicketPatch{Body: new("Show columns and rails.")})
			return tk, ""
		}, func(string) want { return again(desc, "") }},
		{"title edited", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.approve(tk)
			w.edit(tk, core.TicketPatch{Title: new("Board columns")})
			return tk, ""
		}, func(string) want { return again(desc, "") }},
		{"description edited and back", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Columns", Body: "Show columns."})
			w.approve(tk)
			w.edit(tk, core.TicketPatch{Body: new("Other.")})
			w.edit(tk, core.TicketPatch{Body: new("Show columns.")})
			return tk, ""
		}, func(string) want { return ready }},
		{"plan revised", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.plan(tk, "1. Columns.", 0)
			w.approve(tk)
			w.plan(tk, "1. Columns.\n2. Rails.", 1)
			return tk, ""
		}, func(string) want { return again(readiness.Changed(readiness.WhatPlan), "") }},
		{"plan written after", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Typo"})
			w.approve(tk)
			w.plan(tk, "1. Fix it.", 0)
			return tk, ""
		}, func(string) want { return again(readiness.Changed(readiness.WhatPlan), "") }},
		{"description named before the plan", func(w *world) (gen.Ticket, string) {
			tk := w.ticket(core.TicketInput{Title: "Columns"})
			w.plan(tk, "1. Columns.", 0)
			w.approve(tk)
			w.plan(tk, "1. Columns.\n2. Rails.", 1)
			w.edit(tk, core.TicketPatch{Body: new("Changed.")})
			return tk, ""
		}, func(string) want { return again(desc, "") }},
		{"moved to another parent", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			tk := w.ticket(core.TicketInput{Title: "Typo"})
			w.approve(tk)
			w.parent(tk, e.ID)
			return tk, ""
		}, func(string) want { return again(readiness.Changed(readiness.WhatParent), "") }},
		{"moved back where it was asked", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			tk := w.ticket(core.TicketInput{Title: "Typo", ParentID: e.ID})
			w.approve(tk)
			w.parent(tk, "")
			w.parent(tk, e.ID)
			return tk, ""
		}, func(string) want { return ready }},
		{"covered descendant edited", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.edit(c, core.TicketPatch{Body: new("Columns, sorted.")})
			return c, w.key(e)
		}, func(epic string) want { return again(desc, epic) }},
		{"covered grandchild re-parented within the epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.parent(g, e.ID)
			return g, w.key(e)
		}, func(epic string) want { return again(readiness.Changed(readiness.WhatParent), epic) }},
		{"covered parent between edited", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.edit(c, core.TicketPatch{Body: new("Child, rewritten.")})
			return g, w.key(e)
		}, func(epic string) want { return again(readiness.EpicChanged("VAQ-2's description"), epic) }},
		{"covered parent between re-parented within the epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Children.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			d := w.ticket(core.TicketInput{Title: "Other child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.parent(c, d.ID)
			return g, w.key(e)
		}, func(epic string) want { return again(readiness.EpicChanged("VAQ-2's parent"), epic) }},
		{"covered sibling elsewhere edited", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Children.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			d := w.ticket(core.TicketInput{Title: "Other child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.edit(d, core.TicketPatch{Body: new("Rewritten.")})
			return g, w.key(e)
		}, func(string) want { return ready }},
		{"epic edited", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.edit(e, core.TicketPatch{Body: new("A board with rails.")})
			return c, w.key(e)
		}, func(epic string) want { return again(readiness.EpicChanged(readiness.WhatDescription), epic) }},
		{"epic's plan revised, seen by a grandchild", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.plan(e, "1. Child.\n2. More.", 1)
			return g, w.key(e)
		}, func(epic string) want { return again(readiness.EpicChanged(readiness.WhatPlan), epic) }},
		{"epic re-parented", func(w *world) (gen.Ticket, string) {
			top := w.ticket(core.TicketInput{Title: "Top"})
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.parent(e, top.ID)
			return c, w.key(e)
		}, func(epic string) want { return again(readiness.EpicChanged(readiness.WhatParent), epic) }},
		{"epic cancelled", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.status(e, "cancelled")
			return c, ""
		}, ask},
		{"parent between cancelled", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.status(c, "cancelled")
			return g, ""
		}, ask},
		{"cancelled epic, its own approval since", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.status(e, "cancelled")
			w.approve(c)
			return c, ""
		}, func(string) want { return ready }},
		{"cancelled epic back in todo", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Columns", ParentID: e.ID})
			w.approve(e)
			w.status(e, "cancelled")
			w.status(e, "todo")
			return c, w.key(e)
		}, func(string) want { return ready }},
		{"own plan under a cancelled epic", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Board"})
			w.plan(e, "1. Columns.", 0)
			c := w.ticket(core.TicketInput{Title: "Rails", ParentID: e.ID})
			w.plan(c, "1. Rails.", 0)
			w.approve(c)
			w.status(e, "cancelled")
			return c, ""
		}, func(string) want { return ready }},
		{"done epic still covers", func(w *world) (gen.Ticket, string) {
			e := w.ticket(core.TicketInput{Title: "Epic"})
			w.plan(e, "1. Child.", 0)
			c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
			g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
			w.approve(e)
			w.status(e, "done")
			w.status(c, "done")
			return g, ""
		}, func(string) want { return ready }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tk, epic := tc.setup(w)
			w.expect(tk, tc.want(epic))
		})
	}
}

// Batched, every ticket gets its own answer.
func TestCheckAllAnswersEach(t *testing.T) {
	w := newWorld(t)
	e := w.ticket(core.TicketInput{Title: "Epic"})
	w.plan(e, "1. Child.", 0)
	c := w.ticket(core.TicketInput{Title: "Child", ParentID: e.ID})
	g := w.ticket(core.TicketInput{Title: "Grandchild", ParentID: c.ID})
	w.approve(e)
	w.plan(e, "1. Child.\n2. More.", 1)
	all, err := readiness.CheckAll(bg, w.d.RQ, w.files, []string{g.ID, c.ID, e.ID})
	if err != nil {
		t.Fatal(err)
	}
	stale := readiness.EpicChanged(readiness.WhatPlan)
	for id, x := range map[string]want{
		g.ID: again(stale, w.key(e)),
		c.ID: again(stale, w.key(e)),
		e.ID: again(readiness.Changed(readiness.WhatPlan), ""),
	} {
		got := all[id]
		if got.Ready || !slices.Equal(got.Reasons, []string{x.reason}) || got.Action != x.action || got.Epic != x.epic {
			t.Errorf("%s: %+v, want %+v", id, got, x)
		}
	}
}

// Review focus 3: a ticket moved to another project and back is not ready
// until asked again; so are the sub-tickets it carried, and a sub-ticket
// detached, moved out and back alone, and attached again.
func TestMovedAndBackNeedsApprovalAgain(t *testing.T) {
	w := newWorld(t)
	vaq := w.p
	epic := w.ticket(core.TicketInput{Title: "Board"})
	w.plan(epic, "1. Columns.", 0)
	sub := w.ticket(core.TicketInput{Title: "Columns", ParentID: epic.ID})
	x := w.ticket(core.TicketInput{Title: "Rails", ParentID: epic.ID})
	w.approve(epic)
	w.expect(epic, ready)
	w.expect(sub, ready)
	w.expect(x, ready)

	w.moveTo(epic, w.other())
	w.moveTo(epic, vaq)
	epic, _ = w.d.RQ.GetTicket(bg, epic.ID)
	w.expect(epic, ask(""))
	w.expect(sub, ask(w.key(epic)))
	w.approve(epic)
	w.expect(epic, ready)
	w.expect(sub, ready)

	// x detached, out and back, and attached again: the epic's approval
	// no longer covers it.
	w.parent(x, "")
	w.moveTo(x, w.other())
	w.moveTo(x, vaq)
	x, _ = w.d.RQ.GetTicket(bg, x.ID)
	w.parent(x, epic.ID)
	w.expect(x, ask(w.key(epic)))
	w.expect(sub, ready)
	w.approve(epic)
	w.expect(x, ready)

	// In the other project, an approval asked there counts there.
	w.moveTo(epic, w.other())
	w.p = w.other()
	epic, _ = w.d.RQ.GetTicket(bg, epic.ID)
	w.expect(epic, ask(""))
	w.approve(epic)
	w.expect(epic, ready)
}

// Review focus 4: an approval asked, then the description edited, then
// Approve: refused; after asking again, accepted, and the ticket is ready.
func TestEditedBetweenAskAndAnswer(t *testing.T) {
	w := newWorld(t)
	tk := w.ticket(core.TicketInput{Title: "Columns", Body: "Show columns."})
	d, err := w.c.AskDecision(bg, w.owner.ID, core.DecisionInput{
		ProjectID: w.p.ID, TicketIDs: []string{tk.ID}, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve?",
	})
	if err != nil {
		t.Fatal(err)
	}
	w.edit(tk, core.TicketPatch{Body: new("Show columns and rails.")})
	_, err = w.c.AnswerDecision(bg, w.owner.ID, d.ID, core.Answer{Option: core.ApproveLabel})
	var inv *core.Invalid
	if !errors.As(err, &inv) || inv.Fields["answer"] != "The plan or requirement for VAQ-1 changed since this was asked; ask again." {
		t.Fatalf("approve after an edit: %v", err)
	}
	w.expect(tk, ask(""))
	w.approve(tk)
	w.expect(tk, ready)
}
