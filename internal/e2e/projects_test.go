//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// projectsFixture is two projects: PRJ with a long description, a ready
// ticket and an open milestone, and OTH with nothing.
func projectsFixture(t *testing.T) (*webtest.Env, *fixture) {
	env := webtest.New(t)
	f := newProject(t, env, "PRJ")
	ctx := context.Background()
	desc := "A description long enough to need more than two lines on a phone, so the card has to cut it off somewhere sensible rather than letting it run on and on down the page."
	if _, err := env.Deps.Core.UpdateProject(ctx, env.Deps.Actor.ID, f.project.ID, core.ProjectPatch{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	m, err := env.Deps.Core.CreateMilestone(ctx, env.Deps.Actor.ID, f.project.ID, core.MilestoneInput{Name: "Beta launch"})
	if err != nil {
		t.Fatal(err)
	}
	f.ticket("Ready to go", "", func(in *core.TicketInput) { in.MilestoneID = m.ID })
	f.ticket("Shipped", "Done", func(in *core.TicketInput) { in.MilestoneID = m.ID })
	newProject(t, env, "OTH")
	return env, f
}

// noSideScroll fails when the page is wider than the screen.
func noSideScroll(t *testing.T, p *page, w int64) {
	t.Helper()
	if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
		t.Fatalf("the page is %d px wide at %d px", sw, w)
	}
}

// VAQ-102: the projects page's cards and list at phone and desktop
// widths, light and dark. Neither scrolls sideways, the card cuts its
// description at two lines and the row at one, quick settings open in
// place and save back to the page, and the view switch is remembered.
func TestE2EProjectsCardsAndList(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env, _ := projectsFixture(t)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/")
				card := `[data-project-card="PRJ"]`
				if !eval[bool](p, visible(card)) || !eval[bool](p, visible(`[data-project-card="OTH"]`)) {
					t.Fatal("no cards")
				}
				noSideScroll(t, p, w)
				if r := p.rect(card); r.Right > float64(w) || r.Left < 0 {
					t.Fatalf("the card runs %.0f..%.0f px at %d px", r.Left, r.Right, w)
				}
				if clamp := eval[string](p, `getComputedStyle(document.querySelector('[data-project-card="PRJ"] [data-description]')).webkitLineClamp`); clamp != "2" {
					t.Fatalf("card description clamp %q, want 2", clamp)
				}
				// The card's colours follow the scheme.
				bg := `getComputedStyle(document.querySelector('[data-project-card="PRJ"]')).backgroundColor`
				light := eval[string](p, bg)
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[string](p, bg); again == light {
					t.Fatalf("the card looks the same in %s and %s: %q", scheme, other, light)
				}
				p.scheme(scheme)

				// Quick settings open in place, inside the screen.
				panel := `details[data-quick-settings="PRJ"]`
				p.openDetails(panel)
				form := panel + ` form[action="/p/PRJ?return=/"]`
				if !eval[bool](p, visible(form)) {
					t.Fatal("quick settings show no form")
				}
				noSideScroll(t, p, w)
				if r := p.rect(form); r.Right > float64(w) || r.Left < 0 {
					t.Fatalf("the quick settings form runs %.0f..%.0f px at %d px", r.Left, r.Right, w)
				}
				p.clear(form + ` input[name="name"]`)
				p.typeInto(form+` input[name="name"]`, "Renamed")
				p.reloaded(func() { p.click(form + ` button[type="submit"]`) }, `location.pathname === "/" && !!document.querySelector('[data-project-card="PRJ"]')`)
				if toast := eval[string](p, `(document.getElementById("vq-toast")||{}).textContent || ""`); !strings.Contains(toast, "Project saved.") {
					t.Fatalf("toast %q", toast)
				}
				if name := eval[string](p, `document.querySelector('[data-project-card="PRJ"]').textContent`); !strings.Contains(name, "Renamed") {
					t.Fatalf("the card doesn't show the new name: %q", strings.Join(strings.Fields(name), " "))
				}

				// Switch to the list; it is remembered.
				p.reloaded(func() { p.click(`a[href="/?view=list"]`) }, `!!document.querySelector('[data-project-row="PRJ"]')`)
				p.goTo("/")
				row := `[data-project-row="PRJ"]`
				if !eval[bool](p, visible(row)) {
					t.Fatal("the list isn't remembered")
				}
				noSideScroll(t, p, w)
				if r := p.rect(row); r.Right > float64(w) {
					t.Fatalf("the row runs to %.0f px at %d px", r.Right, w)
				}
				d := `getComputedStyle(document.querySelector('[data-project-row="PRJ"] [data-description]'))`
				if clamp := eval[string](p, d+`.webkitLineClamp`); clamp != "1" {
					t.Fatalf("row description clamp %q, want 1", clamp)
				}
				p.goTo("/?view=cards")
			})
		}
	}
}

// VAQ-102: the project's icon shows in the project header, the ticket
// page's breadcrumb, and beside the project in search results.
func TestE2EIconInHeaderAndBreadcrumb(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "ICO")
	if _, err := env.Deps.Core.SetProjectIcon(context.Background(), env.Deps.Actor.ID, f.project.ID, core.IconKindGlyph, "rocket", nil); err != nil {
		t.Fatal(err)
	}
	f.ticket("Findable rocket ticket", "")
	p := open(t, env, 1280, 900)
	for _, tc := range []struct{ path, sel string }{
		{"/p/ICO", `[data-project-header] [data-glyph="rocket"]`},
		{"/p/ICO/board", `[data-project-header] [data-glyph="rocket"]`},
		{"/t/ICO-1", `[data-breadcrumb] [data-glyph="rocket"]`},
		{"/search?q=findable", `[data-card="ICO-1"] [data-glyph="rocket"]`},
	} {
		p.goTo(tc.path)
		if !eval[bool](p, visible(tc.sel)) {
			t.Errorf("%s: no visible %s", tc.path, tc.sel)
		}
	}
}

// Final review: the quick settings icon grid loads when its part opens,
// and choosing a glyph from it saves and returns to the projects page.
func TestE2EQuickIconLoadsOnDemand(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			env, _ := projectsFixture(t)
			p := open(t, env, w, 900)
			p.goTo("/")
			panel := `details[data-quick-settings="PRJ"]`
			icon := panel + ` details[data-quick-icon]`
			rocket := icon + ` label[title="rocket"]`
			if eval[bool](p, `!!document.querySelector(`+js(rocket)+`)`) {
				t.Fatal("the grid is in the page before it is opened")
			}
			p.openDetails(panel)
			p.openDetails(icon)
			p.until(`!!document.querySelector(` + js(rocket) + `)`)
			noSideScroll(t, p, w)
			if r := p.rect(icon + ` form`); r.Right > float64(w) || r.Left < 0 {
				t.Fatalf("the icon form runs %.0f..%.0f px at %d px", r.Left, r.Right, w)
			}
			// Closing and opening again doesn't fetch it twice.
			n := eval[int](p, `document.querySelectorAll(`+js(rocket)+`).length`)
			p.toggled(icon, func() { p.click(icon + " > summary") })
			p.openDetails(icon)
			if again := eval[int](p, `document.querySelectorAll(`+js(rocket)+`).length`); again != n || n != 1 {
				t.Fatalf("%d rockets, then %d", n, again)
			}
			p.click(rocket)
			p.reloaded(func() { p.click(icon + ` button[type="submit"]`) }, `location.pathname === "/" && !!document.querySelector('[data-project-card="PRJ"] [data-glyph="rocket"]')`)
			if toast := eval[string](p, `(document.getElementById("vq-toast")||{}).textContent || ""`); !strings.Contains(toast, "Icon saved.") {
				t.Fatalf("toast %q", toast)
			}
		})
	}
}
