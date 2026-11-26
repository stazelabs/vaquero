//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// Every shortcut in the keyboard-shortcuts article, pressed as real keys.

func shortcutsFixture(t *testing.T) (*webtest.Env, *fixture) {
	env := webtest.New(t)
	f := newProject(t, env, "WEB")
	f.ticket("First card", "")
	f.ticket("Second card", "")
	f.ticket("Third card", "")
	return env, f
}

// handled records whether app.js claimed each key: the listener sits on
// window, after app.js's on document, so it sees defaultPrevented.
func (p *page) recordKeys() {
	p.t.Helper()
	eval[bool](p, `(function () {
		window.vqKeys = [];
		window.addEventListener("keydown", function (ev) {
			window.vqKeys.push({key: ev.key, handled: ev.defaultPrevented});
		});
		return true;
	})()`)
}

// lastKeyHandled is whether app.js handled the newest key press of key.
func (p *page) lastKeyHandled(key string) bool {
	p.t.Helper()
	return eval[bool](p, fmt.Sprintf(`(function () {
		var ks = window.vqKeys.filter(function (k) { return k.key === %s; });
		if (!ks.length) { throw new Error("no keydown for " + %s); }
		return ks[ks.length - 1].handled;
	})()`, js(key), js(key)))
}

func TestShortcutCNewTicketOrProject(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)

	p.goTo("/p/WEB")
	p.reloaded(func() { p.key("c") }, `location.pathname === "/p/WEB/new"`)

	p.goTo("/activity")
	p.reloaded(func() { p.key("c") }, `location.pathname === "/projects/new"`)
}

func TestShortcutSlashFocusesSearch(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/p/WEB")
	p.key("/")
	p.until(`document.activeElement && document.activeElement.id === "vq-search"`)
	if v := eval[string](p, `document.getElementById("vq-search").value`); v != "" {
		t.Fatalf("the / was typed into the search box: %q", v)
	}
}

func TestShortcutGoBoardAndList(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)

	// From a ticket, g b and g l reach its project's board and list.
	p.goTo("/t/WEB-1")
	p.reloaded(func() { p.key("g"); p.key("l") }, `location.pathname === "/p/WEB/list"`)
	p.reloaded(func() { p.key("g"); p.key("b") }, `location.pathname === "/p/WEB/board"`)

	// Outside a project they reach the projects list.
	p.goTo("/activity")
	p.reloaded(func() { p.key("g"); p.key("b") }, `location.pathname === "/"`)
	p.goTo("/activity")
	p.reloaded(func() { p.key("g"); p.key("l") }, `location.pathname === "/"`)
}

func TestShortcutJKMoveFocusAndEnterOpens(t *testing.T) {
	for _, view := range []string{"/p/WEB", "/p/WEB/board", "/p/WEB/list"} {
		t.Run(view, func(t *testing.T) {
			env, _ := shortcutsFixture(t)
			p := open(t, env, 1280, 800)
			p.goTo(view)
			focused := `(document.activeElement && document.activeElement.closest("[data-card]") || {dataset: {}}).dataset.card === `
			p.key("j")
			p.until(focused + `"WEB-1"`)
			p.key("j")
			p.until(focused + `"WEB-2"`)
			p.key("j")
			p.until(focused + `"WEB-3"`)
			p.key("j") // stays on the last
			p.until(focused + `"WEB-3"`)
			p.key("k")
			p.until(focused + `"WEB-2"`)
			// Enter is left to the focused link: it opens the card.
			p.reloaded(func() { p.key(enter) }, `location.pathname === "/t/WEB-2"`)
		})
	}
}

func TestShortcutQuestionMarkOpensAndEscapeCloses(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/p/WEB")
	p.recordKeys()
	p.key("?")
	p.until(`document.getElementById("vq-shortcuts").open`)
	// While a dialog is open the other shortcuts stand aside.
	p.key("c")
	if p.lastKeyHandled("c") {
		t.Fatal("c was handled with the shortcuts dialog open")
	}
	p.key(escape)
	p.until(`!document.getElementById("vq-shortcuts").open`)
	if got := p.path(); got != "/p/WEB" {
		t.Fatalf("path = %s, want /p/WEB", got)
	}
}

func TestShortcutEnterLeftToFocusedButtons(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/p/WEB")
	p.recordKeys()
	// The header's shortcuts button: Enter does what the button does.
	p.do(focus(`[data-shortcuts-open]`))
	p.key(enter)
	p.until(`document.getElementById("vq-shortcuts").open`)
	if p.lastKeyHandled("Enter") {
		t.Fatal("app.js claimed Enter on a focused button")
	}
}

func TestShortcutsIgnoredWhileTyping(t *testing.T) {
	env, _ := shortcutsFixture(t)
	p := open(t, env, 1280, 800)
	p.goTo("/p/WEB")
	p.recordKeys()
	p.typeInto("#vq-search", "cgbj?")
	if v := eval[string](p, `document.getElementById("vq-search").value`); v != "cgbj?" {
		t.Fatalf("search box = %q, want every key typed", v)
	}
	for _, k := range []string{"c", "g", "b", "j", "?"} {
		if p.lastKeyHandled(k) {
			t.Errorf("%s was handled while typing", k)
		}
	}
	if eval[bool](p, `document.getElementById("vq-shortcuts").open`) {
		t.Fatal("? opened the dialog while typing")
	}
}

func TestShortcutEscapeClosesRailWhenNarrow(t *testing.T) {
	env, f := shortcutsFixture(t)
	f.ticket("Someday", "Backlog")
	p := open(t, env, 375, 800)
	p.goTo("/p/WEB/board")
	backlog := fmt.Sprintf(`details[data-rail="%s"]`, f.status["Backlog"].ID)
	p.openDetails(backlog)
	p.toggled(backlog, func() { p.key(escape) })
	p.until(`!document.querySelector(` + js(backlog) + `).open`)
	p.until(`document.activeElement === document.querySelector(` + js(backlog+" > summary") + `)`)
}
