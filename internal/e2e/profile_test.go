//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// avatarColours is the first monogram avatar's computed background and
// text colour under sel.
func avatarColours(sel string) string {
	return `(function () {
	var el = document.querySelector(` + js(sel+" [data-avatar-monogram]") + `);
	if (!el) return "";
	var s = getComputedStyle(el);
	return s.backgroundColor + "|" + s.color;
})()`
}

// VAQ-149: the profile page at phone and desktop widths, light and dark.
// The sidebar's avatar takes the theme's tile colours, saving a name shows
// it at once, and nothing scrolls sideways.
func TestE2EProfilePage(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/")
				if w < 768 {
					// Below md the profile link is in the sidebar's drawer.
					p.click("[data-menu-button]")
				}
				p.reloaded(func() { p.click("[data-profile-link]") }, `location.pathname === "/profile"`)
				if !eval[bool](p, visible(`#name form`)) || !eval[bool](p, visible(`#avatar form`)) {
					t.Fatal("no profile forms")
				}
				colours := eval[string](p, avatarColours("[data-profile-link]"))
				if colours == "" || colours == "rgba(0, 0, 0, 0)|rgb(0, 0, 0)" {
					t.Fatalf("the header avatar has no theme colours: %q", colours)
				}
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[string](p, avatarColours("[data-profile-link]")); again == colours {
					t.Fatalf("the avatar looks the same in %s and %s: %q", scheme, other, colours)
				}
				p.scheme(scheme)
				p.typeInto("#f-display_name", "Kim Lee")
				p.reloaded(func() { p.click(`#name button[type="submit"]`) },
					`!!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Profile saved.") >= 0`)
				if m := eval[string](p, `document.querySelector("[data-profile-link] [data-avatar-monogram]").textContent`); m != "KL" {
					t.Fatalf("header monogram %q", m)
				}
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the profile page is %d px wide at %d px", sw, w)
				}
				shot(t, p, "body", fmt.Sprintf("profile-%d-%s", w, scheme))
				if a, _ := env.Deps.R.GetActor(t.Context(), env.Deps.Actor.ID); a.DisplayName != "Kim Lee" {
					t.Fatalf("%+v", a)
				}
			})
		}
	}
}

// Avatars beside names: the owner's image in the ticket's history and an
// agent's tile on its board card, the card still fitting a phone.
func TestE2EAvatarsOnCardsAndHistory(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "AVA")
	ctx := context.Background()
	agent, err := env.Deps.Core.EnsureAgent(ctx, env.Deps.Actor.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	f.ticket("Herd the cattle", "", func(in *core.TicketInput) { in.AssigneeID = agent.ID })
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 64)...)
	if _, err := env.Deps.Core.SetAvatar(ctx, env.Deps.Actor.ID, "", png); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int64{375, 1280} {
		p := open(t, env, w, 900)
		p.goTo("/p/AVA/board")
		if m := eval[string](p, `(document.querySelector("[data-board-column] [data-avatar-monogram]")||{}).textContent || ""`); m != "CL" {
			t.Fatalf("%d px: the card's avatar is %q", w, m)
		}
		// The board scrolls sideways inside itself, never the page.
		if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
			t.Fatalf("the board page is %d px wide at %d px", sw, w)
		}
		shot(t, p, "body", fmt.Sprintf("avatar-board-%d", w))
		p.goTo("/t/AVA-1")
		img := `img[src="/avatars/` + env.Deps.Actor.ID + `"]`
		if !eval[bool](p, `!!document.querySelector(`+js("ul li "+img)+`)`) {
			t.Fatalf("%d px: no owner avatar in the history", w)
		}
		shot(t, p, "body", fmt.Sprintf("avatar-ticket-%d", w))
		if name := eval[string](p, `document.querySelector("[data-assignee]").textContent`); !strings.Contains(name, "claude") {
			t.Fatalf("assignee field %q", name)
		}
	}
}

// shot saves sel's screenshot as {VQ_SHOTS}/{name}.png when VQ_SHOTS is set.
func shot(t *testing.T, p *page, sel, name string) {
	t.Helper()
	dir := os.Getenv("VQ_SHOTS")
	if dir == "" {
		return
	}
	buf, err := chromedp.Run(p.ctx, chromedp.Screenshot(chromedp.CSS(sel)))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(dir+"/"+name+".png", buf, 0o644)
}
