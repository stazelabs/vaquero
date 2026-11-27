//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// inboxFixture is a workflow-on project with three questions and a plan
// approval waiting, oldest first, and a claim that lapsed two hours ago.
func inboxFixture(t *testing.T) (env *webtest.Env, qs []gen.Decision, approval gen.Decision) {
	t.Helper()
	env = webtest.New(t)
	ctx := context.Background()
	c := env.Deps.Core
	owner := env.Deps.Actor.ID
	f := newProject(t, env, "APP")
	if _, err := c.UpdateProject(ctx, owner, f.project.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
		t.Fatal(err)
	}
	agent, err := c.EnsureAgent(ctx, owner, "claude")
	if err != nil {
		t.Fatal(err)
	}
	tk := f.ticket("Build it", "")
	if _, err := c.SavePlan(ctx, owner, tk.ID, "The plan.", 0); err != nil {
		t.Fatal(err)
	}
	lapsed := f.ticket("Lapsed", "")
	at := func(ago time.Duration, fn func()) {
		c.Now = func() time.Time { return webtest.Start.Add(-ago) }
		defer func() { c.Now = func() time.Time { return webtest.Start } }()
		fn()
	}
	at(3*time.Hour, func() {
		if _, err := c.Claim(ctx, agent.ID, lapsed.ID, 0); err != nil {
			t.Fatal(err)
		}
	})
	at(2*time.Hour, func() {
		if _, err := c.ExpireClaims(ctx); err != nil {
			t.Fatal(err)
		}
	})
	for i, o := range [][2]string{{"Yes", "No"}, {"Left", "Right"}, {"Red", "Blue"}} {
		at(time.Duration(5-i)*time.Hour, func() {
			d, err := c.AskDecision(ctx, agent.ID, core.DecisionInput{ProjectID: f.project.ID, Kind: core.DecisionQuestion,
				Question: fmt.Sprintf("Question %d?", i+1), Options: []core.Option{{Label: o[0]}, {Label: o[1]}}})
			if err != nil {
				t.Fatal(err)
			}
			qs = append(qs, d)
		})
	}
	at(time.Hour, func() {
		approval, err = c.AskDecision(ctx, agent.ID, core.DecisionInput{ProjectID: f.project.ID, TicketIDs: []string{tk.ID},
			Kind: core.DecisionApproval, Subject: "plan", Question: "Approve the plan for APP-1?"})
		if err != nil {
			t.Fatal(err)
		}
	})
	return env, qs, approval
}

func decision(t *testing.T, env *webtest.Env, id string) gen.Decision {
	t.Helper()
	d, err := env.DB.RQ.GetDecision(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func cardSel(id string) string { return "#decision-" + id }

// VAQ-172: the inbox answers in place, by click and by key; keys typed in
// a note box type (Review Focus 5); a refusal shows on its card, and a
// second tab's answer is refused there (Review Focus 2); Dismiss hides a
// lapse for good in this browser. At phone, tablet and desktop widths,
// light and dark, with no sideways scroll.
func TestInbox(t *testing.T) {
	for _, w := range []int64{375, 768, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env, qs, ap := inboxFixture(t)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/inbox")
				p.until(`document.querySelectorAll("[data-inbox] [data-decision]").length === 4`)
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the inbox is %d px wide at %d px", sw, w)
				}

				// By click: the card leaves and the next takes the focus.
				p.click(cardSel(qs[0].ID) + ` button[value="Yes"]`)
				p.until(`!document.querySelector(` + js(cardSel(qs[0].ID)) + `)`)
				if d := decision(t, env, qs[0].ID); d.State != "answered" || d.Answer != "Yes" {
					t.Fatalf("question 1: %s %q", d.State, d.Answer)
				}
				p.until(`document.activeElement === document.querySelector(` + js(cardSel(qs[1].ID)) + `)`)

				// By key: j to the first card, 1 for its first option.
				eval[bool](p, `(document.activeElement.blur(), true)`)
				p.key("j")
				p.until(`document.activeElement === document.querySelector(` + js(cardSel(qs[1].ID)) + `)`)
				p.key("1")
				p.until(`!document.querySelector(` + js(cardSel(qs[1].ID)) + `)`)
				if d := decision(t, env, qs[1].ID); d.Answer != "Left" || d.RelayedBy.Valid {
					t.Fatalf("question 2: %q", d.Answer)
				}

				// n opens the note; 1 and j typed there are text.
				p.until(`document.activeElement === document.querySelector(` + js(cardSel(qs[2].ID)) + `)`)
				p.key("n")
				note := cardSel(qs[2].ID) + ` textarea[name="note"]`
				p.until(`document.activeElement === document.querySelector(` + js(note) + `)`)
				p.key("1j")
				if v := eval[string](p, `document.querySelector(`+js(note)+`).value`); v != "1j" {
					t.Fatalf("the note reads %q", v)
				}
				if n := p.srv.requests("POST /inbox/" + qs[2].ID + "/answer"); n != 0 {
					t.Fatalf("typing in the note answered %d times", n)
				}

				// A refusal: the plan changed since it was asked.
				if _, err := env.Deps.Core.SavePlan(context.Background(), env.Deps.Actor.ID, decisionTicket(t, env, ap.ID), "A new plan.", 1); err != nil {
					t.Fatal(err)
				}
				p.click(cardSel(ap.ID) + ` button[value="Approve"]`)
				p.until(`(document.querySelector(` + js(cardSel(ap.ID)+" [data-card-error]") + `) || {}).textContent === "The plan or requirement for APP-1 changed since this was asked; ask again."`)
				if d := decision(t, env, ap.ID); d.State != "open" {
					t.Fatalf("the approval is %s", d.State)
				}

				// Two tabs: the second, holding a note, answers after the first.
				p2 := open(t, env, w, 900)
				p2.scheme(scheme)
				p2.goTo("/inbox")
				p2.openDetails(cardSel(qs[2].ID) + " details[data-note]")
				p2.typeInto(cardSel(qs[2].ID)+` textarea[name="note"]`, "x")
				p.click(cardSel(qs[2].ID) + ` button[value="Red"]`)
				p.until(`!document.querySelector(` + js(cardSel(qs[2].ID)) + `)`)
				p2.click(cardSel(qs[2].ID) + ` button[value="Blue"]`)
				p2.until(`(document.querySelector(` + js(cardSel(qs[2].ID)+" [data-card-error]") + `) || {}).textContent === "This decision is already answered."`)
				if d := decision(t, env, qs[2].ID); d.Answer != "Red" || d.AnswerNote != "1j" {
					t.Fatalf("question 3: %q %q", d.Answer, d.AnswerNote)
				}

				// Withdraw asks first, then posts with htmx: the card leaves.
				p.click(cardSel(ap.ID) + ` button[form]`)
				p.until(`document.getElementById("vq-confirm").open`)
				if n := p.srv.requests("POST /inbox/" + ap.ID + "/withdraw"); n != 0 {
					t.Fatalf("Withdraw posted %d times before the confirm", n)
				}
				p.click(`[data-confirm-ok]`)
				p.until(`!document.querySelector(` + js(cardSel(ap.ID)) + `)`)
				if d := decision(t, env, ap.ID); d.State != "withdrawn" {
					t.Fatalf("the approval is %s", d.State)
				}

				// Dismiss hides the lapse, and it stays hidden.
				p.until(`!!document.querySelector("[data-lapse] [data-dismiss]:not([hidden])")`)
				p.click(`[data-lapse] [data-dismiss]`)
				p.until(`document.querySelector("[data-lapse]").hidden && document.querySelector('[data-inbox-group="check"]').hidden`)
				p.goTo("/inbox")
				p.until(`document.querySelector("[data-lapse]").hidden && document.querySelector('[data-inbox-group="check"]').hidden`)
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the inbox is %d px wide at %d px", sw, w)
				}
			})
		}
	}
}

// decisionTicket is the one ticket decision id names.
func decisionTicket(t *testing.T, env *webtest.Env, id string) string {
	t.Helper()
	ids, err := env.DB.RQ.ListDecisionTicketIDs(context.Background(), id)
	if err != nil || len(ids) != 1 {
		t.Fatalf("%v %v", ids, err)
	}
	return ids[0]
}

// VAQ-172: the ticket page lists the decisions naming its ticket as the
// inbox's cards and answers them in place, at phone and desktop widths:
// the card leaves, the page doesn't reload and goes no wider.
func TestTicketPageAnswersItsDecisions(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			env := webtest.New(t)
			ctx := context.Background()
			f := newProject(t, env, "APP")
			tk := f.ticket("Build it", "")
			for _, q := range []string{"Which way?", "Which colour?"} {
				if _, err := env.Deps.Core.AskDecision(ctx, env.Deps.Actor.ID, core.DecisionInput{ProjectID: f.project.ID, TicketIDs: []string{tk.ID},
					Kind: core.DecisionQuestion, Question: q, Options: []core.Option{{Label: "Left"}, {Label: "Right"}}}); err != nil {
					t.Fatal(err)
				}
			}
			p := open(t, env, w, 900)
			p.goTo("/t/APP-1")
			const cards = `[data-ticket-decisions] [data-decision]`
			p.until(`document.querySelectorAll("` + cards + `").length === 2`)
			if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
				t.Fatalf("the page is %d px wide at %d px", sw, w)
			}
			eval[bool](p, `window.vqSamePage = true`)
			p.click(cards + ` button[value="Left"]`)
			p.until(`document.querySelectorAll("` + cards + `").length === 1`)
			if !eval[bool](p, `window.vqSamePage === true`) {
				t.Fatal("the page reloaded")
			}
			p.click(cards + ` button[value="Right"]`)
			p.until(`document.querySelectorAll("` + cards + `").length === 0 && !document.querySelector("[data-ticket-decisions] h2")`)
			if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
				t.Fatalf("the page is %d px wide at %d px", sw, w)
			}
		})
	}
}
