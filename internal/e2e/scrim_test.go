//go:build e2e

package e2e

import "testing"

// The confirm dialog's backdrop darkens the page in both themes: the page
// seen through it (the backdrop's colour over the page's) is darker than
// the page itself. VAQ-213: with ink as the backdrop, dark mode washed the
// page light.
func TestE2EDialogBackdropDarkensThePage(t *testing.T) {
	for _, scheme := range []string{"light", "dark"} {
		t.Run(scheme, func(t *testing.T) {
			env, _, _ := confirmFixture(t)
			p := open(t, env, 1280, 900)
			p.scheme(scheme)
			p.goTo("/t/WEB-1")
			p.click(deleteButton)
			p.until(`document.getElementById("vq-confirm").open`)
			got := eval[[]float64](p, `(function () {
				function rgba(css) {
					var c = document.createElement("canvas");
					c.width = c.height = 1;
					var x = c.getContext("2d", { willReadFrequently: true });
					x.fillStyle = "#000"; x.fillRect(0, 0, 1, 1);
					x.clearRect(0, 0, 1, 1);
					x.fillStyle = css; x.fillRect(0, 0, 1, 1);
					var d = x.getImageData(0, 0, 1, 1).data;
					var a = d[3] / 255;
					return [d[0], d[1], d[2], a];
				}
				function lum(c) { return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]; }
				var page = rgba(getComputedStyle(document.body).backgroundColor);
				var back = rgba(getComputedStyle(document.getElementById("vq-confirm"), "::backdrop").backgroundColor);
				var a = back[3];
				var seen = lum(page) * (1 - a) + lum(back) * a;
				return [a > 0 && seen < lum(page) ? 1 : 0, lum(page), seen, a];
			})()`)
			if got[0] != 1 {
				t.Fatalf("the backdrop doesn't darken the page in %s mode: page luminance %.1f, seen %.1f, backdrop alpha %.2f", scheme, got[1], got[2], got[3])
			}
		})
	}
}
