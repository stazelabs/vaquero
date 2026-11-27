//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/gitread/gittest"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// inView is true when the first error message in main sits inside the
// viewport and the focused element is the field it describes.
const inView = `(function () {
	var m = document.querySelector('main [role="alert"]');
	if (!m) { return false; }
	var r = m.getBoundingClientRect();
	var a = document.activeElement;
	return r.top >= 0 && r.bottom <= window.innerHeight && !!a &&
		a.getAttribute("aria-invalid") === "true";
})()`

// VAQ-209: a refused settings form re-renders with 422 and its message is
// in view, its field focused, however low on the page the form is.
func TestRefusedSettingsFormInView(t *testing.T) {
	t.Run("link loop", func(t *testing.T) {
		env := webtest.New(t)
		a := newProject(t, env, "AAA")
		newProject(t, env, "BBB")
		if _, err := env.Deps.Core.AddProjectLink(context.Background(), env.Deps.Actor.ID, a.project.ID, "depends on", "BBB"); err != nil {
			t.Fatal(err)
		}
		p := open(t, env, 1280, 700)
		p.goTo("/p/BBB/settings")
		eval[bool](p, `(document.getElementById("f-project").value = "AAA") === "AAA"`)
		p.reloaded(func() { p.click(`form[action="/p/BBB/links"] button[type=submit]`) }, `!!document.querySelector('main [role="alert"]')`)
		p.until(inView)
	})
	t.Run("fetch minutes 3", func(t *testing.T) {
		r := gittest.Repo(t)
		r.WriteCommit("a.md", "a", "first")
		env := webtest.New(t)
		newProject(t, env, "REP")
		p := open(t, env, 1280, 700)
		p.goTo("/p/REP/settings")
		p.typeInto("#f-repo-path", r.Dir)
		eval[bool](p, `(document.getElementById("f-fetch-minutes").value = "3") === "3"`)
		p.reloaded(func() { p.click("#repository button[type=submit]") }, `!!document.querySelector('main [role="alert"]')`)
		p.until(inView)
	})
	t.Run("no error never scrolls", func(t *testing.T) {
		env := webtest.New(t)
		newProject(t, env, "QUI")
		p := open(t, env, 1280, 700)
		p.goTo("/p/QUI/settings")
		if y := eval[float64](p, `window.scrollY`); y != 0 {
			t.Fatalf("scrolled to %v", y)
		}
	})
}
