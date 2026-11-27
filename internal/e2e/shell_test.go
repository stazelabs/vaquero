//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// The sidebar shell (VAQ-171): the sidebar at desktop width, the drawer
// at phone width, and the shell's keys.

const sidebarSel = "#vq-sidebar"

// keyWith presses the letter k with modifier held (kb.ModifierCtrl,
// kb.ModifierMeta), as Chrome sends a chord: no text.
func (p *page) keyWith(modifier kb.Modifier, k string) {
	p.t.Helper()
	upper := strings.ToUpper(k)
	down := input.DispatchKeyEventParams{
		Type: input.DispatchKeyEventTypeRawKeyDown, Key: k, Code: "Key" + upper,
		WindowsVirtualKeyCode: int64(upper[0]), Modifiers: int64(modifier),
	}
	up := down
	up.Type = input.DispatchKeyEventTypeKeyUp
	for _, ev := range []input.DispatchKeyEventParams{down, up} {
		if _, err := chromedp.Call(p.ctx, input.DispatchKeyEvent, ev); err != nil {
			p.t.Fatalf("key %q: %v", k, err)
		}
	}
}

// navStyle is how the sidebar draws the element sel matches, and what the
// paper and accent tokens resolve to.
type navStyle struct {
	Bg, Shadow, Weight, Paper, Accent string
}

func (p *page) navStyle(sel string) navStyle {
	p.t.Helper()
	return eval[navStyle](p, `(function () {
		function token(name) {
			var d = document.createElement("div");
			d.style.color = "var(" + name + ")";
			document.body.appendChild(d);
			var v = getComputedStyle(d).color;
			d.remove();
			return v;
		}
		var s = getComputedStyle(document.querySelector(`+js(sel)+`));
		return {Bg: s.backgroundColor, Shadow: s.boxShadow, Weight: s.fontWeight,
			Paper: token("--color-paper"), Accent: token("--color-accent")};
	})()`)
}

func TestShell(t *testing.T) {
	env := webtest.New(t)
	ctx := context.Background()
	long := "Zz " + strings.Repeat("a very long project name ", 3)[:57] // 60 characters, sorted last
	for i := range 19 {
		if _, err := env.Deps.Core.CreateProject(ctx, env.Deps.Actor.ID, core.ProjectInput{Key: fmt.Sprintf("P%02d", i), Name: fmt.Sprintf("Project %02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := env.Deps.Core.CreateProject(ctx, env.Deps.Actor.ID, core.ProjectInput{Key: "WEB", Name: long})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, env: env, project: p}
	f.reloadStatuses()
	f.ticket("First card", "")

	t.Run("wide", func(t *testing.T) {
		for _, scheme := range []string{"light", "dark"} {
			pg := open(t, env, 1280, 700)
			pg.scheme(scheme)
			pg.goTo("/p/WEB/board")
			shot(t, pg, "body", "shell-1280-"+scheme)
			if !eval[bool](pg, visible(sidebarSel)) {
				t.Fatalf("%s: no sidebar at 1280 px", scheme)
			}
			if eval[bool](pg, visible(`[data-menu-button]`)) {
				t.Fatalf("%s: the menu button shows at 1280 px", scheme)
			}
			cur := sidebarSel + ` [data-nav-view="board"]`
			if !eval[bool](pg, `document.querySelector(`+js(cur)+`).hasAttribute("data-current")`) {
				t.Fatalf("%s: the board isn't marked", scheme)
			}
			s := pg.navStyle(cur)
			if s.Bg != s.Paper || !strings.Contains(s.Shadow, s.Accent) || s.Weight != "600" {
				t.Errorf("%s: the current view is drawn %+v", scheme, s)
			}
			// Twenty projects: the sidebar scrolls on its own, and the
			// current project, last by name, is scrolled into view.
			if !eval[bool](pg, `(function () { var s = document.querySelector(`+js(sidebarSel)+`); return s.scrollHeight > s.clientHeight && s.scrollTop > 0; })()`) {
				t.Errorf("%s: the sidebar doesn't scroll to the current project", scheme)
			}
			if !eval[bool](pg, `(function () {
				var s = document.querySelector(`+js(sidebarSel)+`).getBoundingClientRect();
				var r = document.querySelector('[data-nav-project="WEB"]').getBoundingClientRect();
				return r.top >= s.top && r.bottom <= s.bottom;
			})()`) {
				t.Errorf("%s: the current project isn't in view", scheme)
			}
			if eval[int64](pg, `window.scrollY`) != 0 {
				t.Errorf("%s: scrolling the sidebar scrolled the page", scheme)
			}
			// No text overflows the sidebar; the long name is cut.
			if bad := eval[string](pg, `(function () {
				var s = document.querySelector(`+js(sidebarSel)+`);
				var right = s.getBoundingClientRect().left + s.clientWidth;
				if (s.scrollWidth > s.clientWidth) { return "the sidebar scrolls sideways"; }
				var out = [];
				s.querySelectorAll("a, span, button, input, kbd").forEach(function (el) {
					var r = el.getBoundingClientRect();
					if (r.width && r.right > right + 0.5) { out.push(el.outerHTML.slice(0, 60)); }
				});
				var name = document.querySelector('[data-nav-project="WEB"] [data-nav-name]');
				if (!(name.scrollWidth > name.clientWidth && getComputedStyle(name).textOverflow === "ellipsis")) {
					out.push("the long name isn't cut with an ellipsis");
				}
				return out.join("; ");
			})()`); bad != "" {
				t.Errorf("%s: %s", scheme, bad)
			}
		}
	})

	// From md (768 px) the sidebar sits beside the page and the menu
	// button is gone.
	t.Run("tablet", func(t *testing.T) {
		for _, scheme := range []string{"light", "dark"} {
			pg := open(t, env, 768, 900)
			pg.scheme(scheme)
			pg.goTo("/p/WEB/board")
			if !eval[bool](pg, visible(sidebarSel)) {
				t.Fatalf("%s: no sidebar at 768 px", scheme)
			}
			if eval[bool](pg, visible(`[data-menu-button]`)) {
				t.Fatalf("%s: the menu button shows at 768 px", scheme)
			}
			if eval[bool](pg, visible(`[data-sidebar-backdrop]`)) {
				t.Fatalf("%s: the backdrop shows at 768 px", scheme)
			}
			if w := eval[int64](pg, `document.documentElement.scrollWidth`); w > 768 {
				t.Errorf("%s: the page is %d px wide at 768 px", scheme, w)
			}
		}
	})

	t.Run("last view", func(t *testing.T) {
		pg := open(t, env, 1280, 700)
		pg.goTo("/t/WEB-1")
		marked := `document.querySelector(` + js(sidebarSel) + `).querySelector("[data-nav-view][data-current]").dataset.navView`
		if v := eval[string](pg, marked); v != "focus" {
			t.Fatalf("a ticket with no last view marks %s, want focus", v)
		}
		pg.goTo("/p/WEB/list")
		pg.goTo("/t/WEB-1")
		pg.until(marked + ` === "list"`)
		pg.goTo("/p/WEB/settings")
		pg.until(marked + ` === "list"`)
	})

	t.Run("phone", func(t *testing.T) {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(scheme, func(t *testing.T) { phoneDrawer(t, env, scheme) })
		}
	})

	// The search hint names Cmd on macOS (the server's default) and Ctrl
	// elsewhere.
	t.Run("hint", func(t *testing.T) {
		for platform, want := range map[string]string{"macOS": "⌘K", "Linux": "Ctrl+K"} {
			pg := open(t, env, 1280, 700)
			if _, err := chromedp.Call(pg.ctx, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{
				UserAgent: "Mozilla/5.0", Platform: platform, UserAgentMetadata: &emulation.UserAgentMetadata{Platform: platform},
			}); err != nil {
				t.Fatal(err)
			}
			pg.goTo("/p/WEB")
			if got := eval[string](pg, `document.querySelector("[data-search-hint]").textContent`); got != want {
				t.Errorf("%s: the hint reads %q, want %q", platform, got, want)
			}
		}
	})

	t.Run("keys", func(t *testing.T) {
		pg := open(t, env, 1280, 800)
		pg.goTo("/p/WEB")
		pg.keyWith(kb.ModifierMeta, "k")
		pg.until(`document.activeElement && document.activeElement.id === "vq-search"`)
		// Ctrl+K works from inside another field, and types nothing there.
		pg.goTo("/p/WEB/new")
		pg.do(focus(`input[name="title"]`))
		pg.keyWith(kb.ModifierCtrl, "k")
		pg.until(`document.activeElement && document.activeElement.id === "vq-search"`)
		if v := eval[string](pg, `document.querySelector('input[name="title"]').value + document.getElementById("vq-search").value`); v != "" {
			t.Fatalf("Ctrl+K typed %q", v)
		}
		// g i opens the inbox (Task 4 builds the page; the address is enough).
		pg.goTo("/p/WEB")
		pg.reloaded(func() { pg.key("g"); pg.key("i") }, `location.pathname === "/inbox"`)
	})
}

// phoneDrawer is the sidebar as a drawer at 375 px in scheme: the menu
// opens it; a link, a search, Escape and the backdrop close it; the page
// never scrolls sideways.
func phoneDrawer(t *testing.T, env *webtest.Env, scheme string) {
	pg := open(t, env, 375, 800)
	pg.scheme(scheme)
	pg.goTo("/p/WEB")
	menu := `[data-menu-button]`
	closed := `!document.querySelector(` + js(sidebarSel) + `).checkVisibility() && document.querySelector(` + js(menu) + `).getAttribute("aria-expanded") === "false"`
	opened := `document.querySelector(` + js(sidebarSel) + `).checkVisibility() && document.querySelector(` + js(menu) + `).getAttribute("aria-expanded") === "true" && document.querySelector("[data-sidebar-backdrop]").checkVisibility()`
	wide := func(when string) {
		t.Helper()
		if w := eval[int64](pg, `document.documentElement.scrollWidth`); w > 375 {
			t.Errorf("%s: the page is %d px wide at 375 px", when, w)
		}
	}
	pg.until(closed)
	wide("closed")

	// The menu opens the drawer; a link closes it on its way.
	pg.click(menu)
	pg.until(opened)
	wide("open")
	shot(t, pg, "body", "shell-375-drawer-"+scheme)
	pg.reloaded(func() { pg.click(sidebarSel + ` [data-nav-view="board"]`) }, `location.pathname === "/p/WEB/board"`)
	pg.until(closed)

	// A link closes it as it is clicked, before the next page arrives
	// (here held back, so the drawer is seen to close on its own).
	pg.click(menu)
	pg.until(opened)
	eval[bool](pg, `(window.addEventListener("click", function (ev) { ev.preventDefault(); }), true)`)
	pg.click(sidebarSel + ` [data-nav-view="list"]`)
	pg.until(closed)
	if got := pg.path(); got != "/p/WEB/board" {
		t.Fatalf("path %s; the held-back link navigated", got)
	}
	pg.goTo("/p/WEB/board")

	// Escape closes it and gives focus back to the menu button.
	pg.click(menu)
	pg.until(opened)
	pg.key(escape)
	pg.until(closed)
	pg.until(focusedOn(menu))

	// So does the backdrop, beside the drawer.
	pg.click(menu)
	pg.until(opened)
	r := pg.rect(sidebarSel)
	pg.do(clickAt((r.Right+375)/2, 400))
	pg.until(closed)
	wide("closed again")

	// A search sent from the drawer closes it on its way (held back here).
	pg.click(menu)
	pg.until(opened)
	eval[bool](pg, `(window.addEventListener("submit", function (ev) { ev.preventDefault(); }), true)`)
	pg.do(focus("#vq-search"))
	pg.key("card")
	pg.key(enter)
	pg.until(closed)
	if got := pg.path(); got != "/p/WEB/board" {
		t.Fatalf("path %s; the held-back search navigated", got)
	}
}
