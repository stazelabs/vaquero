package core_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func question(f fixture) core.DecisionInput {
	return core.DecisionInput{
		ProjectID: f.Project.ID, TicketIDs: []string{f.T1.ID}, Kind: core.DecisionQuestion,
		Question: "Which board order?", Options: []core.Option{{Label: "By priority", Tradeoff: "Urgent first"}, {Label: "By age"}},
		Recommendation: "By priority", Why: "It decides what agents pick up.",
	}
}

func init() {
	ops = append(ops,
		coreOp{"AskDecision",
			func(c *core.Core, f fixture) error {
				_, err := c.AskDecision(bg, f.Owner.ID, question(f))
				return err
			},
			func(c *core.Core, f fixture) error {
				in := question(f)
				in.Question = " "
				_, err := c.AskDecision(bg, f.Owner.ID, in)
				return err
			}},
		coreOp{"RecordRuling",
			func(c *core.Core, f fixture) error {
				in := question(f)
				in.Kind = core.DecisionRuling
				_, err := c.RecordRuling(bg, f.Owner.ID, in, core.Answer{Option: "By age"})
				return err
			},
			func(c *core.Core, f fixture) error {
				in := question(f)
				in.Kind = core.DecisionRuling
				_, err := c.RecordRuling(bg, f.Owner.ID, in, core.Answer{Option: "Neither"})
				return err
			}},
	)
	lateFail["AnswerDecision"] = func(c *core.Core, f fixture, snap func()) error {
		d, err := c.AskDecision(bg, f.Owner.ID, question(f))
		if err != nil {
			return err
		}
		snap()
		_, err = c.AnswerDecision(bg, f.Owner.ID, d.ID, core.Answer{Option: "By age"})
		return err
	}
	lateFail["WithdrawDecision"] = func(c *core.Core, f fixture, snap func()) error {
		d, err := c.AskDecision(bg, f.Owner.ID, question(f))
		if err != nil {
			return err
		}
		snap()
		return c.WithdrawDecision(bg, f.Owner.ID, d.ID)
	}
	ops = append(ops,
		coreOp{"AnswerDecision",
			func(c *core.Core, f fixture) error {
				id, err := rawQuestion(c, f)
				if err != nil {
					return err
				}
				_, err = c.AnswerDecision(bg, f.Owner.ID, id, core.Answer{Option: "By age"})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.AnswerDecision(bg, f.Owner.ID, "nope", core.Answer{Option: "By age"})
				return err
			}},
		coreOp{"WithdrawDecision",
			func(c *core.Core, f fixture) error {
				id, err := rawQuestion(c, f)
				if err != nil {
					return err
				}
				return c.WithdrawDecision(bg, f.Owner.ID, id)
			},
			func(c *core.Core, f fixture) error { return c.WithdrawDecision(bg, f.Owner.ID, "nope") }},
	)
}

// rawQuestion arranges an open question without an event, so the
// operation's is the only one.
func rawQuestion(c *core.Core, f fixture) (string, error) {
	const id = "01DEC"
	_, err := c.DB.Exec(`INSERT INTO decisions (id, project_id, kind, question, options, asked_by, asked_at)
		VALUES (?, ?, 'question', 'Which?', '[{"label":"By age","tradeoff":""}]', ?, '2026-11-18T12:00:00Z')`, id, f.Project.ID, f.Owner.ID)
	return id, err
}

func builder(t *testing.T, c *core.Core, f fixture) gen.Actor {
	t.Helper()
	return agent(t, c, f.Owner, "builder")
}

func decisionRow(t *testing.T, c *core.Core, id string) gen.Decision {
	t.Helper()
	d, err := gen.New(c.DB).GetDecision(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestAskAndAnswerQuestion(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	before := eventCount(t, d)
	q, err := c.AskDecision(bg, a.ID, question(f))
	if err != nil {
		t.Fatal(err)
	}
	if q.State != "open" || q.AskedBy != a.ID || q.Kind != "question" || q.AnsweredBy.Valid {
		t.Fatalf("%+v", q)
	}
	var opts []core.Option
	if err := json.Unmarshal([]byte(q.Options), &opts); err != nil || len(opts) != 2 || opts[0].Tradeoff != "Urgent first" {
		t.Fatalf("%s %v", q.Options, err)
	}
	kind, p := lastEvent(t, d)
	if kind != core.EvDecisionAsked || p["decision_id"] != q.ID || p["kind"] != "question" {
		t.Fatalf("%s %v", kind, p)
	}
	if ids, _ := p["ticket_ids"].([]any); len(ids) != 1 || ids[0] != f.T1.ID {
		t.Fatalf("ticket_ids %v", p["ticket_ids"])
	}
	// The option is matched in any case and stored as labelled.
	ans, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "by AGE", Note: "Simpler."})
	if err != nil {
		t.Fatal(err)
	}
	if ans.State != "answered" || ans.Answer != "By age" || ans.AnswerNote != "Simpler." ||
		ans.AnsweredBy.String != f.Owner.ID || ans.RelayedBy.Valid || ans.RelayedWords != "" || !ans.AnsweredAt.Valid {
		t.Fatalf("%+v", ans)
	}
	kind, p = lastEvent(t, d)
	if kind != core.EvDecisionAnswered || p["answer"] != "By age" || p["relayed"] != nil {
		t.Fatalf("%s %v", kind, p)
	}
	if n := eventCount(t, d); n != before+2 {
		t.Fatalf("%d events, want 2", n-before)
	}
	// Answered once: a second answer is refused.
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By priority"}); invalidMsg(err) != "This decision is already answered." {
		t.Fatalf("%v", err)
	}
	// Free text answers too; an unknown option, or nothing, is refused.
	q2, _ := c.AskDecision(bg, a.ID, question(f))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{Option: "Sideways"}); invalidMsg(err) != "Choose By priority or By age, or write an answer." {
		t.Fatalf("%v", err)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{}); invalidMsg(err) != "Choose By priority or By age, or write an answer." {
		t.Fatalf("%v", err)
	}
	if ans, err := c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{Text: "Neither; by milestone."}); err != nil || ans.Answer != "Neither; by milestone." {
		t.Fatalf("%v %+v", err, ans)
	}
}

// Review Focus 2: an agent can't answer as itself, whether or not it
// asked; only the owner answers.
func TestAgentCannotAnswerAsItself(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	q, err := c.AskDecision(bg, a.ID, question(f))
	if err != nil {
		t.Fatal(err)
	}
	other, _ := c.EnsureAgent(bg, f.Owner.ID, "reviewer")
	snap := snapshot(t, d)
	for _, by := range []string{a.ID, other.ID} {
		_, err := c.AnswerDecision(bg, by, q.ID, core.Answer{Option: "By age"})
		if invalidMsg(err) != "Only the owner answers a decision; relay the owner's answer with their words." {
			t.Fatalf("%v", err)
		}
		// Even naming itself as the relayer.
		_, err = c.AnswerDecision(bg, by, q.ID, core.Answer{Option: "By age", RelayedBy: by, Words: "By age."})
		if invalidMsg(err) == "" {
			t.Fatal("an agent answered as itself, relaying")
		}
	}
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused answer changed rows")
	}
	// The owner answers directly.
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age"}); err != nil {
		t.Fatal(err)
	}
}

func TestRelayedAnswerNeedsWords(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	q, _ := c.AskDecision(bg, a.ID, question(f))
	snap := snapshot(t, d)
	_, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age", RelayedBy: a.ID, Words: "  "})
	fieldErr(t, err, "words", "Quote the owner's answer when relaying it.")
	// Words without a relayer are refused too: a direct answer has none.
	_, err = c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age", Words: "By age."})
	fieldErr(t, err, "words", "Only a relayed answer quotes the owner's words.")
	// The relayer is an agent, never the owner.
	_, err = c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age", RelayedBy: f.Owner.ID, Words: "By age."})
	fieldErr(t, err, "relayed_by", "Only an agent relays the owner's answer.")
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused answer changed rows")
	}
}

func TestRelayedAnswerRecordsAgentAndWords(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	q, _ := c.AskDecision(bg, a.ID, question(f))
	ans, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By priority", RelayedBy: a.ID, Words: "Go with priority, it's what I'd do."})
	if err != nil {
		t.Fatal(err)
	}
	if ans.AnsweredBy.String != f.Owner.ID || ans.RelayedBy.String != a.ID || ans.RelayedWords != "Go with priority, it's what I'd do." {
		t.Fatalf("%+v", ans)
	}
	var actor string
	if err := d.R.QueryRow(`SELECT actor_id FROM events ORDER BY seq DESC LIMIT 1`).Scan(&actor); err != nil || actor != f.Owner.ID {
		t.Fatalf("event actor %q %v", actor, err)
	}
	_, p := lastEvent(t, d)
	if p["relayed"] != true || p["relayed_by"] != "builder" {
		t.Fatalf("%v", p)
	}
	// The words are on the decision, not in the event.
	if _, ok := p["words"]; ok {
		t.Fatalf("%v", p)
	}
}

func TestRulingIsCreatedAnswered(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	in := question(f)
	in.Kind = core.DecisionRuling
	r, err := c.RecordRuling(bg, a.ID, in, core.Answer{Option: "By priority", Note: "Matches the ready list."})
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "answered" || r.Kind != "ruling" || r.AnsweredBy.String != a.ID || r.AskedBy != a.ID || r.Answer != "By priority" || r.AnswerNote != "Matches the ready list." {
		t.Fatalf("%+v", r)
	}
	kind, p := lastEvent(t, d)
	if kind != core.EvDecisionRuled || p["answer"] != "By priority" {
		t.Fatalf("%s %v", kind, p)
	}
	// A ruling isn't asked, and a question isn't ruled.
	if _, err := c.AskDecision(bg, a.ID, in); invalidMsg(err) == "" {
		t.Fatal("asked a ruling")
	}
	if _, err := c.RecordRuling(bg, a.ID, question(f), core.Answer{Option: "By age"}); invalidMsg(err) == "" {
		t.Fatal("ruled a question")
	}
	// A ruling is not withdrawn: it is overruled.
	if err := c.WithdrawDecision(bg, a.ID, r.ID); invalidMsg(err) != "Only an open decision can be withdrawn." {
		t.Fatalf("%v", err)
	}
}

func TestOverruleKeepsOldAnswer(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	in := question(f)
	in.Kind = core.DecisionRuling
	r, _ := c.RecordRuling(bg, a.ID, in, core.Answer{Option: "By priority", Note: "Matches the ready list."})
	// Another agent can't overrule; the owner can, relayed or directly.
	if _, err := c.AnswerDecision(bg, a.ID, r.ID, core.Answer{Option: "By age"}); invalidMsg(err) == "" {
		t.Fatal("an agent overruled")
	}
	o, err := c.AnswerDecision(bg, f.Owner.ID, r.ID, core.Answer{Option: "By age", RelayedBy: a.ID, Words: "No, by age."})
	if err != nil {
		t.Fatal(err)
	}
	if o.Answer != "By age" || o.AnswerNote != "" || o.AnsweredBy.String != f.Owner.ID || o.RelayedBy.String != a.ID {
		t.Fatalf("%+v", o)
	}
	kind, p := lastEvent(t, d)
	old, _ := p["overruled"].(map[string]any)
	if kind != core.EvDecisionAnswered || old["answer"] != "By priority" || old["note"] != "Matches the ready list." || old["answered_by"] != a.ID {
		t.Fatalf("%s %v", kind, p)
	}
	// And again: the owner's own answer is overruled the same way.
	if _, err := c.AnswerDecision(bg, f.Owner.ID, r.ID, core.Answer{Text: "By milestone."}); err != nil {
		t.Fatal(err)
	}
	_, p = lastEvent(t, d)
	if old, _ := p["overruled"].(map[string]any); old["answer"] != "By age" || old["relayed_by"] != a.ID {
		t.Fatalf("%v", p)
	}
}

func planApproval(f fixture, ids ...string) core.DecisionInput {
	return core.DecisionInput{ProjectID: f.Project.ID, TicketIDs: ids, Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan?"}
}

func TestGroupPlanApprovalWritesRowsAndOneEvent(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	t3, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Third"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID, f.T2.ID, t3.ID, f.T1.ID))
	if err != nil {
		t.Fatal(err)
	}
	// An approval with no options gets Approve and Request changes.
	if q.Options != `[{"label":"Approve","tradeoff":""},{"label":"Request changes","tradeoff":""}]` {
		t.Fatalf("%s", q.Options)
	}
	if n := count(t, d, `SELECT count(*) FROM decision_tickets WHERE decision_id = ?`, q.ID); n != 3 {
		t.Fatalf("%d decision tickets, want 3 (duplicates dropped)", n)
	}
	before := eventCount(t, d)
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
	if n := eventCount(t, d); n != before+1 {
		t.Fatalf("%d events, want 1", n-before)
	}
	if n := count(t, d, `SELECT count(*) FROM approvals WHERE decision_id = ?`, q.ID); n != 3 {
		t.Fatalf("%d approvals, want 3", n)
	}
	var ticket any
	if err := d.R.QueryRow(`SELECT ticket_id FROM events ORDER BY seq DESC LIMIT 1`).Scan(&ticket); err != nil || ticket != nil {
		t.Fatalf("a group event names ticket %v (%v)", ticket, err)
	}
	_, p := lastEvent(t, d)
	if ids, _ := p["ticket_ids"].([]any); len(ids) != 3 || p["approved"] != true || p["subject"] != "plan" {
		t.Fatalf("%v", p)
	}

	// Any other answer records no approval.
	q2, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{Option: "Request changes"}); err != nil {
		t.Fatal(err)
	}
	q3, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q3.ID, core.Answer{Text: "Approve, I suppose."}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM approvals WHERE decision_id IN (?, ?)`, q2.ID, q3.ID); n != 0 {
		t.Fatalf("%d approvals from a refusal", n)
	}
	// An approval's options must include Approve (any case), and only it
	// approves: an approval without one is refused.
	in := planApproval(f, f.T2.ID)
	in.Options = []core.Option{{Label: "Hold off"}, {Label: "Go"}}
	_, err = c.AskDecision(bg, a.ID, in)
	fieldErr(t, err, "options", "An approval needs an Approve option.")
	in.Options = []core.Option{{Label: "Not yet"}, {Label: "approve", Tradeoff: "Ship it"}}
	q4, err := c.AskDecision(bg, a.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q4.ID, core.Answer{Option: "Not yet"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM approvals WHERE decision_id = ?`, q4.ID); n != 0 {
		t.Fatalf("%d approvals from the first option, which isn't Approve", n)
	}
	q6, _ := c.AskDecision(bg, a.ID, in)
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q6.ID, core.Answer{Option: "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM approvals WHERE decision_id = ?`, q6.ID); n != 1 {
		t.Fatalf("%d approvals from Approve", n)
	}
	// A merge approval records no approval rows.
	m := planApproval(f, f.T2.ID)
	m.Subject = "merge"
	q5, _ := c.AskDecision(bg, a.ID, m)
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q5.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT count(*) FROM approvals WHERE decision_id = ?`, q5.ID); n != 0 {
		t.Fatalf("%d approvals from a merge approval", n)
	}
	// A plan approval needs a ticket; an approval, a subject.
	if _, err := c.AskDecision(bg, a.ID, planApproval(f)); invalidMsg(err) != "Name the tickets whose plan is to be approved." {
		t.Fatalf("%v", err)
	}
	none := planApproval(f, f.T1.ID)
	none.Subject = ""
	if _, err := c.AskDecision(bg, a.ID, none); invalidMsg(err) != "Say what is to be approved: plan, merge or release." {
		t.Fatalf("%v", err)
	}
}

func TestApprovalPinsRequirementHashAndPlanRevision(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	// T1 has its own plan at revision 2; its child has none, so it pins 0
	// (readiness leans on the epic's own approval); T2 has none and no
	// parent.
	if _, err := c.SavePlan(bg, a.ID, f.T1.ID, "One.", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SavePlan(bg, a.ID, f.T1.ID, "One. Two.", 1); err != nil {
		t.Fatal(err)
	}
	child, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Café", Body: "Café menu.", ParentID: f.T1.ID})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID, child.ID, f.T2.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	// Each pins its parent and its sub-tickets then (rulings L-R8, L-R11).
	for id, want := range map[string]struct {
		hash, pins string
		rev        int64
	}{
		f.T1.ID:  {sum("First\n"), `{"parent":"","children":[{"id":"` + child.ID + `","requirement_hash":"` + sum("Café\nCafé menu.") + `"}]}`, 2},
		child.ID: {sum("Café\nCafé menu."), `{"parent":"` + f.T1.ID + `","children":[]}`, 0},
		f.T2.ID:  {sum("Second\n"), `{"parent":"","children":[]}`, 0},
	} {
		var hash, pins, at string
		var rev int64
		if err := d.R.QueryRow(`SELECT requirement_hash, plan_revision, pins, at FROM approvals WHERE decision_id = ? AND ticket_id = ?`, q.ID, id).Scan(&hash, &rev, &pins, &at); err != nil {
			t.Fatal(err)
		}
		if hash != want.hash || rev != want.rev || pins != want.pins || at == "" {
			t.Errorf("%s: %s %d %s, want %s %d %s", id, hash, rev, pins, want.hash, want.rev, want.pins)
		}
	}
	if got := core.RequirementHash("Café", "Café menu."); got != sum("Café\nCafé menu.") {
		t.Fatal(got)
	}
}

// Ruling L-R7: an approval approves what was asked. A plan revised, or a
// requirement edited, between the ask and the answer refuses an Approve;
// Request changes is still taken.
func TestApproveRefusedWhenChangedSinceAsked(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	if _, err := c.SavePlan(bg, a.ID, f.T1.ID, "One.", 0); err != nil {
		t.Fatal(err)
	}
	var hash string
	var rev int64
	q, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T2.ID, f.T1.ID))
	if err := d.R.QueryRow(`SELECT requirement_hash, plan_revision FROM decision_tickets WHERE decision_id = ? AND ticket_id = ?`, q.ID, f.T1.ID).Scan(&hash, &rev); err != nil ||
		hash != core.RequirementHash("First", "") || rev != 1 {
		t.Fatalf("asked pins %s %d %v", hash, rev, err)
	}
	if _, err := c.SavePlan(bg, a.ID, f.T1.ID, "One. Two.", 1); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, d)
	_, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"})
	fieldErr(t, err, "answer", "The plan or requirement for VAQ-1 changed since this was asked; ask again.")
	if !maps.Equal(snapshot(t, d), snap) {
		t.Fatal("a refused answer changed rows")
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Request changes"}); err != nil {
		t.Fatal(err)
	}

	q2, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T2.ID))
	body := "Now with a description."
	if _, err := c.PatchTicket(bg, f.Owner.ID, f.T2.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		t.Fatal(err)
	}
	_, err = c.AnswerDecision(bg, f.Owner.ID, q2.ID, core.Answer{Option: "Approve"})
	fieldErr(t, err, "answer", "The plan or requirement for VAQ-2 changed since this was asked; ask again.")
	// Asked again, it approves.
	q3, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T2.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q3.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
}

// Ruling L-R9: the approval covers the children and parent asked about
// too. A child added or edited, or the ticket moved to another parent,
// between the ask and the answer refuses an Approve.
func TestApproveRefusedWhenFamilyChangedSinceAsked(t *testing.T) {
	c, _, f := newFixture(t)
	a := builder(t, c, f)
	refused := func(q gen.Decision, key string) {
		t.Helper()
		_, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"})
		fieldErr(t, err, "answer", "The plan or requirement for "+key+" changed since this was asked; ask again.")
	}
	q, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID))
	child, err := c.CreateTicket(bg, f.Owner.ID, f.Project.ID, core.TicketInput{Title: "Child", ParentID: f.T1.ID})
	if err != nil {
		t.Fatal(err)
	}
	refused(q, "VAQ-1") // a child added

	q, _ = c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID))
	body := "A child, rewritten."
	if _, err := c.PatchTicket(bg, f.Owner.ID, child.ID, core.TicketPatch{Body: &body}, 0); err != nil {
		t.Fatal(err)
	}
	refused(q, "VAQ-1") // a child edited

	q, _ = c.AskDecision(bg, a.ID, planApproval(f, child.ID))
	parent := f.T2.ID
	if _, err := c.PatchTicket(bg, f.Owner.ID, child.ID, core.TicketPatch{ParentID: &parent}, 0); err != nil {
		t.Fatal(err)
	}
	refused(q, core.TicketKey{Project: "VAQ", Number: child.Number}.String()) // the asked ticket re-parented

	// Nothing changed: it approves, pinning the children as asked.
	q, _ = c.AskDecision(bg, a.ID, planApproval(f, f.T2.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionTicketsMustBeInProject(t *testing.T) {
	c, d, f := newFixture(t)
	snap := snapshot(t, d)
	in := question(f)
	in.TicketIDs = []string{f.T1.ID, f.O1.ID}
	_, err := c.AskDecision(bg, f.Owner.ID, in)
	fieldErr(t, err, "tickets", "OTH-1 isn't in VAQ.")
	in.TicketIDs = []string{"no-such-ticket"}
	_, err = c.AskDecision(bg, f.Owner.ID, in)
	fieldErr(t, err, "tickets", "One of the tickets doesn't exist.")
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused ask changed rows")
	}
	// A project-level decision names no ticket, and its event none.
	in.TicketIDs = nil
	if _, err := c.AskDecision(bg, f.Owner.ID, in); err != nil {
		t.Fatal(err)
	}
}

func TestWithdrawDecision(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	other, _ := c.EnsureAgent(bg, f.Owner.ID, "reviewer")
	q, _ := c.AskDecision(bg, a.ID, question(f))
	if err := c.WithdrawDecision(bg, other.ID, q.ID); invalidMsg(err) != "Only who asked, or the owner, can withdraw a decision." {
		t.Fatalf("%v", err)
	}
	if err := c.WithdrawDecision(bg, a.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	if kind, _ := lastEvent(t, d); kind != core.EvDecisionWithdrawn || decisionRow(t, c, q.ID).State != "withdrawn" {
		t.Fatal(kind)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age"}); invalidMsg(err) != "This decision was withdrawn." {
		t.Fatalf("%v", err)
	}
}

// Deleting a project removes its decisions, their tickets and approvals,
// a group approval's included, leaving no dangling key.
func TestDeleteProjectRemovesDecisions(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	q, _ := c.AskDecision(bg, a.ID, planApproval(f, f.T1.ID, f.T2.ID))
	if _, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "Approve"}); err != nil {
		t.Fatal(err)
	}
	keep, err := c.AskDecision(bg, a.ID, core.DecisionInput{ProjectID: f.Other.ID, TicketIDs: []string{f.O1.ID}, Kind: core.DecisionQuestion, Question: "Keep?"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "VAQ"); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM approvals`:                0,
		`SELECT count(*) FROM decision_tickets`:         1,
		`SELECT count(*) FROM decisions`:                1,
		`SELECT count(*) FROM pragma_foreign_key_check`: 0,
	} {
		if n := count(t, d, q); n != want {
			t.Errorf("%s: %d, want %d", q, n, want)
		}
	}
	if decisionRow(t, c, keep.ID).ID != keep.ID {
		t.Fatal("the other project's decision went")
	}
}

// Ruling L-R2: through MCP, an answer is relayed by an agent; the owner's
// token without one can't answer directly there.
func TestMCPDirectAnswerRefused(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	q, _ := c.AskDecision(bg, a.ID, question(f))
	mcp := core.WithOrigin(bg, core.Origin{Surface: core.SurfaceMCP})
	snap := snapshot(t, d)
	_, err := c.AnswerDecision(mcp, f.Owner.ID, q.ID, core.Answer{Option: "By age"})
	fieldErr(t, err, "agent", "Run vaquero mcp with --agent NAME to relay the owner's answer.")
	if got := snapshot(t, d); !maps.Equal(got, snap) {
		t.Fatal("a refused answer changed rows")
	}
	if _, err := c.AnswerDecision(mcp, f.Owner.ID, q.ID, core.Answer{Option: "By age", RelayedBy: a.ID, Words: "By age."}); err != nil {
		t.Fatal(err)
	}
	// The web, the CLI and the API answer directly.
	for _, s := range []string{core.SurfaceWeb, core.SurfaceCLI, core.SurfaceAPI} {
		q, _ := c.AskDecision(bg, a.ID, question(f))
		if _, err := c.AnswerDecision(core.WithOrigin(bg, core.Origin{Surface: s}), f.Owner.ID, q.ID, core.Answer{Option: "By age"}); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func TestAnswerRefusedInArchivedProject(t *testing.T) {
	c, _, f := newFixture(t)
	q, _ := c.AskDecision(bg, f.Owner.ID, question(f))
	if err := c.SetProjectArchived(bg, f.Owner.ID, f.Project.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err := c.AnswerDecision(bg, f.Owner.ID, q.ID, core.Answer{Option: "By age"})
	fieldErr(t, err, "project", "Restore this project to record decisions in it.")
}

// An overruled answer and note are cut to 200 characters in the event,
// like the payload's other texts.
func TestOverruledAnswerIsClipped(t *testing.T) {
	c, d, f := newFixture(t)
	a := builder(t, c, f)
	in := question(f)
	in.Kind = core.DecisionRuling
	long := strings.Repeat("é", 300)
	r, err := c.RecordRuling(bg, a.ID, in, core.Answer{Text: long, Note: long})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AnswerDecision(bg, f.Owner.ID, r.ID, core.Answer{Option: "By age"}); err != nil {
		t.Fatal(err)
	}
	_, p := lastEvent(t, d)
	old, _ := p["overruled"].(map[string]any)
	if old["answer"] != strings.Repeat("é", 200) || old["note"] != strings.Repeat("é", 200) {
		t.Fatalf("%v", old)
	}
}
