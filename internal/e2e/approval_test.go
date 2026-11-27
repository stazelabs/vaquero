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
// approval, is approved from the page and reads Ready, at phone and desktop
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
				p.reloaded(func() { p.click(`[data-approval] button[value="Approve"]`) },
					`!document.querySelector("[data-approval]") && document.querySelector("[data-readiness]").textContent.includes("Ready")`)
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the page is %d px wide at %d px", sw, w)
				}
			})
		}
	}
}
