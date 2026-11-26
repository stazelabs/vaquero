package templates

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/help"
	"github.com/stazelabs/vaquero/internal/web/icons"
)

// Every curated glyph is drawn through internal/web/icons.
func TestProjectGlyphsHaveIcons(t *testing.T) {
	for _, g := range core.ProjectGlyphs {
		if _, ok := icons.UI("glyph-" + g); !ok {
			t.Errorf("glyph %s: no glyph-%s in ui.json", g, g)
		}
	}
}

func TestProjectIconKinds(t *testing.T) {
	v := ProjectIconView{ID: "01ABC", Key: "ABW", Name: "ABC Web App"}
	mono := html(t, ProjectIcon(v, "sm"))
	if !strings.Contains(mono, `data-monogram="AW"`) || !strings.Contains(mono, ">AW</span>") ||
		!strings.Contains(mono, tileClasses[core.TileColor("01ABC")]) || !strings.Contains(mono, "size-[22px]") {
		t.Fatalf("monogram: %s", mono)
	}
	v.Kind, v.Glyph = core.IconKindGlyph, "rocket"
	glyph := html(t, ProjectIcon(v, "md"))
	if !strings.Contains(glyph, `data-icon="glyph-rocket"`) || !strings.Contains(glyph, "size-[30px]") {
		t.Fatalf("glyph: %s", glyph)
	}
	v.Kind = core.IconKindImage
	img := html(t, ProjectIcon(v, "md"))
	if !strings.Contains(img, `src="/p/ABW/icon"`) || !strings.Contains(img, "object-cover") || !strings.Contains(img, `width="30"`) {
		t.Fatalf("image: %s", img)
	}
}

func TestIconChangeWording(t *testing.T) {
	e := EventView{Kind: core.EvProjectUpdated, ProjectKey: "VAQ", Payload: map[string]any{"changes": map[string]any{"icon": map[string]any{}}}}
	if got := EventSentence(e); got != "changed the icon of VAQ" {
		t.Fatalf("%q", got)
	}
}

// The projects article lists every glyph, in order.
func TestProjectGlyphsDocumented(t *testing.T) {
	a, ok := help.Get("projects-and-tickets")
	if !ok {
		t.Fatal("no article")
	}
	want := strings.Join(core.ProjectGlyphs[:len(core.ProjectGlyphs)-1], ", ") + " and " + core.ProjectGlyphs[len(core.ProjectGlyphs)-1] + "."
	if !strings.Contains(a.Markdown, "The glyphs: "+want) {
		t.Fatalf("the article's glyph list isn't %q", want)
	}
}
