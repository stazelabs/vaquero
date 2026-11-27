//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// VAQ-160: a todo with the workflow on says what it needs, asks for plan
// approval, is asked again after an edit makes the waiting approval stale,
// is approved from the page and reads Ready (on the board too), at phone and desktop
// widths, light and dark, with no sideways scroll.
func TestE2EAskAndApprove(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				f := newProject(t, env, "APP")
				ctx := context.Background()
				if _, err := env.Deps.Core.UpdateProject(ctx, env.Deps.Actor.ID, f.project.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
					t.Fatal(err)
				}
				tk := f.ticket("Build it", "", func(in *core.TicketInput) { in.Tier = "light" })
				if _, err := env.Deps.Core.SavePlan(ctx, env.Deps.Actor.ID, tk.ID, "The plan.", 0); err != nil {
					t.Fatal(err)
				}
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/t/APP-1")
				p.until(`document.querySelector("[data-readiness]").textContent.includes("Ask for plan approval.")`)
				p.reloaded(func() { p.click(`[data-readiness] button[type=submit]`) }, `!!document.querySelector("[data-approval]")`)
				// Ruling R-Q14: edited while the approval waits, Approve is
				// refused, and Ask again replaces it with one that approves.
				body := "The plan, with rails."
				if _, err := env.Deps.Core.PatchTicket(ctx, env.Deps.Actor.ID, tk.ID, core.TicketPatch{Body: &body}, 0); err != nil {
					t.Fatal(err)
				}
				p.goTo("/t/APP-1")
				p.reloaded(func() { p.click(`[data-approval] button[value="Approve"]`) },
					`document.body.textContent.includes("Use Ask again.") && !!document.querySelector("[data-approval]")`)
				p.reloaded(func() { p.click(`[data-ask-again] button[type=submit]`) },
					`document.body.textContent.includes("Asked again for approval") && !!document.querySelector("[data-approval]")`)
				p.reloaded(func() { p.click(`[data-approval] button[value="Approve"]`) },
					`!document.querySelector("[data-approval]") && document.querySelector("[data-readiness]").textContent.includes("Ready")`)
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the page is %d px wide at %d px", sw, w)
				}
				p.goTo("/p/APP/board")
				p.until(`!!document.querySelector('[data-card="APP-1"][data-ready]')`)
			})
		}
	}
}

// VAQ-210: the readiness block follows changes like the plan does. A ticket
// page open on Ready reads the change a new plan revision makes, without a
// reload. An answered approval gone stale offers "Ask for approval" (ledger
// ruling, VAQ-210).
func TestReadinessFollowsPlan(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "APP")
	ctx := context.Background()
	if _, err := env.Deps.Core.UpdateProject(ctx, env.Deps.Actor.ID, f.project.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
		t.Fatal(err)
	}
	tk := f.ticket("Build it", "", func(in *core.TicketInput) { in.Tier = "light" })
	if _, err := env.Deps.Core.SavePlan(ctx, env.Deps.Actor.ID, tk.ID, "The plan.", 0); err != nil {
		t.Fatal(err)
	}
	p := open(t, env, 1280, 900)
	p.goTo("/t/APP-1")
	p.until(`document.querySelector("[data-readiness]").textContent.includes("Ask for plan approval.")`)
	p.reloaded(func() { p.click(`[data-readiness] button[type=submit]`) }, `!!document.querySelector("[data-approval]")`)
	p.reloaded(func() { p.click(`[data-approval] button[value="Approve"]`) },
		`!document.querySelector("[data-approval]") && document.querySelector("[data-readiness]").textContent.includes("Ready")`)
	if _, err := env.Deps.Core.SavePlan(ctx, env.Deps.Actor.ID, tk.ID, "The plan, revised.", 1); err != nil {
		t.Fatal(err)
	}
	p.until(`document.querySelector("#readiness").textContent.includes("Changed since approval: the plan. Ask again.") && !!document.querySelector('#readiness form[action="/t/APP-1/approval/ask"] button')`)
}

// VAQ-210: a note typed in the open approval form survives a refresh that
// comes after focus has left the field, and the block refreshes once the
// note is cleared.
func TestReadinessKeepsUnsentNote(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "APP")
	ctx := context.Background()
	if _, err := env.Deps.Core.UpdateProject(ctx, env.Deps.Actor.ID, f.project.ID, core.ProjectPatch{Workflow: new(true)}); err != nil {
		t.Fatal(err)
	}
	tk := f.ticket("Build it", "", func(in *core.TicketInput) { in.Tier = "light" })
	if _, err := env.Deps.Core.SavePlan(ctx, env.Deps.Actor.ID, tk.ID, "The plan.", 0); err != nil {
		t.Fatal(err)
	}
	p := open(t, env, 1280, 900)
	p.goTo("/t/APP-1")
	p.until(`document.querySelector("[data-readiness]").textContent.includes("Ask for plan approval.")`)
	p.reloaded(func() { p.click(`[data-readiness] button[type=submit]`) }, `!!document.querySelector("[data-approval]")`)
	const note = `[data-approval] input[name=note], [data-approval] textarea[name=note]`
	p.typeInto(note, "keep me")
	eval[bool](p, `(window.vqBlock = document.getElementById("readiness"), document.activeElement.blur(), true)`)
	if _, err := env.Deps.Core.SavePlan(ctx, env.Deps.Actor.ID, tk.ID, "The plan, revised.", 1); err != nil {
		t.Fatal(err)
	}
	p.until(`document.getElementById("plan").textContent.includes("revised")`)
	if !eval[bool](p, `window.vqBlock.isConnected && document.getElementById("readiness") === window.vqBlock && document.querySelector(`+js(note)+`).value === "keep me"`) {
		t.Fatalf("the note was lost: %s", eval[string](p, `document.getElementById("readiness").textContent`))
	}
	// Clearing it and leaving the field lets the held refresh through.
	p.do(focus(note))
	eval[bool](p, `(document.querySelector(`+js(note)+`).value = "", document.activeElement.blur(), true)`)
	p.until(`document.getElementById("readiness") !== window.vqBlock && document.querySelector(` + js(note) + `).value === ""`)
}
