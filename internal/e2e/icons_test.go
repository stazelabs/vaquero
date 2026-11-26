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

// tileColours is the monogram tile's computed background and text colour
// on the settings page's header.
const tileColours = `(function () {
	var el = document.querySelector("[data-monogram]");
	if (!el) return "";
	var s = getComputedStyle(el);
	return s.backgroundColor + "|" + s.color;
})()`

// VAQ-103: the settings icon section at phone and desktop widths, light
// and dark. The tiles take the theme's colours, the glyph grid fits the
// screen, and choosing a glyph saves it and shows it in the header.
func TestE2ESettingsIcon(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				f := newProject(t, env, "ICO")
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/p/ICO/settings")
				if !eval[bool](p, visible(`#icon form`)) {
					t.Fatal("no icon form")
				}
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("settings are %d px wide at %d px", sw, w)
				}
				// The grid's last glyph sits inside the screen.
				if right := eval[float64](p, `(function () { var l = document.querySelectorAll('#icon input[name="icon"]'); return l[l.length-1].parentElement.getBoundingClientRect().right; })()`); right > float64(w) {
					t.Fatalf("the glyph grid runs to %.0f px at %d px", right, w)
				}
				colours := eval[string](p, tileColours)
				if colours == "" || colours == "rgba(0, 0, 0, 0)|rgb(0, 0, 0)" {
					t.Fatalf("the monogram tile has no theme colours: %q", colours)
				}
				// The same tile in the other scheme is drawn differently.
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[string](p, tileColours); again == colours {
					t.Fatalf("the tile looks the same in %s and %s: %q", scheme, other, colours)
				}
				p.scheme(scheme)

				// Choose the rocket and save.
				p.click(`#icon label[title="rocket"]`)
				if !eval[bool](p, `document.querySelector('#icon input[value="glyph:rocket"]').checked`) {
					t.Fatal("clicking the rocket didn't choose it")
				}
				p.reloaded(func() { p.click(`#icon button[type="submit"]`) }, `!!document.querySelector('p [data-glyph="rocket"]')`)
				if toast := eval[string](p, `(document.getElementById("vq-toast")||{}).textContent || ""`); !strings.Contains(toast, "Icon saved.") {
					t.Fatalf("toast %q", toast)
				}
				got, err := env.Deps.R.GetProjectByKey(t.Context(), "ICO")
				if err != nil || got.IconKind != "glyph" || got.IconGlyph != "rocket" || got.ID != f.project.ID {
					t.Fatalf("%+v %v", got, err)
				}
			})
		}
	}
}

// VAQ-146: Remove image asks first, then deletes the stored image, and a
// project showing it shows its monogram.
func TestE2ESettingsRemoveIconImage(t *testing.T) {
	env := webtest.New(t)
	f := newProject(t, env, "ICO")
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 64)...)
	if _, err := env.Deps.Core.SetProjectIcon(context.Background(), env.Deps.Actor.ID, f.project.ID, "image", "", &core.IconImage{Data: png}); err != nil {
		t.Fatal(err)
	}
	p := open(t, env, 1280, 900)
	p.goTo("/p/ICO/settings")
	const remove = `form[action="/p/ICO/icon/delete"] button[type="submit"]`
	before := f.events()
	p.click(remove)
	p.until(`document.getElementById("vq-confirm").open`)
	if msg := eval[string](p, `document.querySelector("[data-confirm-message]").textContent`); !strings.Contains(msg, "Remove the uploaded icon image of ICO?") {
		t.Fatalf("asks %q", msg)
	}
	if f.events() != before {
		t.Fatal("Remove image changed something before it was confirmed")
	}
	p.reloaded(func() { p.click("[data-confirm-ok]") },
		`!!document.getElementById("vq-toast") && document.getElementById("vq-toast").textContent.indexOf("Image removed.") >= 0`)
	if !eval[bool](p, `!!document.querySelector("[data-monogram]") && !document.querySelector('img[src="/p/ICO/icon"]') && !document.querySelector(`+js(remove)+`)`) {
		t.Fatal("after removing, the page still shows the image or the Remove image button")
	}
	if got, _ := env.Deps.R.GetProjectByKey(t.Context(), "ICO"); got.IconKind != "monogram" || f.events() != before+1 {
		t.Fatalf("%+v, %d events", got, f.events()-before)
	}
}
