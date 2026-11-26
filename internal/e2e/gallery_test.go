//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// galleryPNG is a w x h opaque PNG.
func galleryPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 160, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// galleryState is what the page shows of its attachments: whether the
// gallery comes before the file rows, each tile's drawn size and whether
// its preview loaded, and whether the long name is cut short.
const galleryState = `(function () {
	var g = document.querySelector("#attachments [data-gallery]");
	var row = document.querySelector('#attachments a[href$="/notes.txt"]');
	if (!g || !row) return {ok: false};
	var tiles = Array.prototype.map.call(g.querySelectorAll("img"), function (img) {
		var r = img.getBoundingClientRect();
		return {w: Math.round(r.width), h: Math.round(r.height), loaded: img.complete && img.naturalWidth > 0,
			natural: img.naturalWidth + "x" + img.naturalHeight, right: r.right, blank: img.closest("a").target === "_blank"};
	});
	var name = g.querySelector("li:last-child p");
	return {ok: true, first: !!(g.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING),
		tiles: tiles, cut: name.scrollWidth > name.clientWidth, nameWidth: name.getBoundingClientRect().width,
		border: getComputedStyle(g.querySelector("a")).boxShadow + "|" + getComputedStyle(g.querySelector("a")).backgroundColor};
})()`

type galleryView struct {
	OK, First, Cut bool
	NameWidth      float64
	Border         string
	Tiles          []struct {
		W, H          int
		Loaded, Blank bool
		Natural       string
		Right         float64
	}
}

// VAQ-110: images come first, as 96 px previews made by the server, at
// phone and desktop widths, light and dark; other files follow as rows.
func TestE2EGalleryShowsImagesFirst(t *testing.T) {
	for _, w := range []int64{375, 1280} {
		for _, scheme := range []string{"light", "dark"} {
			t.Run(fmt.Sprintf("%d-%s", w, scheme), func(t *testing.T) {
				env := webtest.New(t)
				f := newProject(t, env, "GAL")
				f.ticket("Screens", "")
				c := env.Client()
				// The text file goes up first; the page still shows the
				// images before it.
				for _, up := range []struct {
					name string
					data []byte
				}{
					{"notes.txt", []byte("words\n")},
					{"wide.png", galleryPNG(t, 900, 300)},
					{"tall.png", galleryPNG(t, 200, 600)},
					{"a-screenshot-with-a-very-long-name-indeed.png", galleryPNG(t, 300, 300)},
				} {
					if r := c.Upload("/t/GAL-1/attachments", up.name, "", up.data); r.Status != http.StatusCreated {
						t.Fatalf("%s: %d %s", up.name, r.Status, r.Body)
					}
				}
				p := open(t, env, w, 900)
				p.scheme(scheme)
				p.goTo("/t/GAL-1")
				// The previews load lazily: bring them into view first.
				_ = eval[bool](p, `(document.querySelector("#attachments").scrollIntoView(), true)`)
				p.until(`Array.prototype.every.call(document.querySelectorAll("#attachments [data-gallery] img"), function (i) { return i.complete && i.naturalWidth > 0; })`)
				g := eval[galleryView](p, galleryState)
				if !g.OK || !g.First {
					t.Fatalf("want the gallery before the file rows: %+v", g)
				}
				if len(g.Tiles) != 3 {
					t.Fatalf("want three previews, got %+v", g.Tiles)
				}
				for i, tile := range g.Tiles {
					if tile.W != 96 || tile.H != 96 || !tile.Loaded || !tile.Blank || tile.Right > float64(w) {
						t.Errorf("tile %d: %+v", i, tile)
					}
				}
				// The previews are the server's, not the originals.
				if g.Tiles[0].Natural != "192x64" || g.Tiles[1].Natural != "64x192" {
					t.Errorf("preview sizes %s %s", g.Tiles[0].Natural, g.Tiles[1].Natural)
				}
				if !g.Cut || g.NameWidth > 96 {
					t.Errorf("the long name isn't cut to the tile: %+v", g)
				}
				if sw := eval[int64](p, `document.documentElement.scrollWidth`); sw > w {
					t.Fatalf("the page is %d px wide at %d px", sw, w)
				}
				other := map[string]string{"light": "dark", "dark": "light"}[scheme]
				p.scheme(other)
				if again := eval[galleryView](p, galleryState); again.Border == g.Border {
					t.Fatalf("the tile looks the same in %s and %s: %q", scheme, other, g.Border)
				}
			})
		}
	}
}
