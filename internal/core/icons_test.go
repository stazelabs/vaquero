package core_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

// pngIcon sniffs as image/png; webpIcon as image/webp.
var (
	pngIcon  = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{7}, 64)...)
	webpIcon = append([]byte("RIFF\x10\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{1}, 32)...)
)

func init() {
	ops = append(ops,
		coreOp{"SetProjectIcon",
			func(c *core.Core, f fixture) error {
				_, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, "image", "", &core.IconImage{Data: pngIcon})
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, "image", "", &core.IconImage{Type: "image/png", Data: []byte("<svg/>")})
				return err
			}},
	)
	// The icon row is written, then the event refuses.
	lateFail["SetProjectIcon"] = func(c *core.Core, f fixture, snap func()) error {
		snap()
		_, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, "image", "", &core.IconImage{Data: pngIcon})
		return err
	}
}

func TestMonogram(t *testing.T) {
	for _, tc := range []struct{ name, key, want string }{
		{"ABC Web App", "ABW", "AW"},
		{"Vaquero", "VAQ", "VA"},
		{"", "XY", "XY"},
		{"  rodeo   clown ", "RC", "RC"},
		{"élan vital", "EV", "ÉV"},
		{"(beta) tools", "BT", "BT"},
	} {
		if got := core.Monogram(tc.name, tc.key); got != tc.want {
			t.Errorf("Monogram(%q, %q) = %q, want %q", tc.name, tc.key, got, tc.want)
		}
	}
}

// Review Focus 4: a key change never recolours a project, and an icon
// change never changes its key.
func TestTileColorStableAcrossRekey(t *testing.T) {
	c, _, f := newFixture(t)
	before := core.TileColor(f.Project.ID)
	if before < 0 || before > 7 {
		t.Fatalf("tile %d", before)
	}
	p, err := c.ChangeProjectKey(bg, f.Owner.ID, f.Project.ID, "RODEO")
	if err != nil {
		t.Fatal(err)
	}
	if got := core.TileColor(p.ID); got != before {
		t.Fatalf("rekey recoloured: %d → %d", before, got)
	}
	p, err = c.SetProjectIcon(bg, f.Owner.ID, p.ID, "glyph", "rocket", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "RODEO" {
		t.Fatalf("icon change moved the key to %s", p.Key)
	}
	// The colour is spread over the 8 pairs, not stuck on one.
	seen := map[int]bool{}
	for _, id := range []string{"01A", "01B", "01C", "01D", "01E", "01F", "01G", "01H", "01J", "01K", "01M", "01N"} {
		seen[core.TileColor(id)] = true
	}
	if len(seen) < 4 {
		t.Fatalf("tile colours %v", seen)
	}
}

func TestSetProjectIconGlyphAndImage(t *testing.T) {
	c, d, f := newFixture(t)
	if f.Project.IconKind != "monogram" || f.Project.IconGlyph != "" {
		t.Fatalf("default %q %q", f.Project.IconKind, f.Project.IconGlyph)
	}
	newest := func() (string, map[string]any) {
		t.Helper()
		var kind, payload string
		if err := d.R.QueryRow(`SELECT kind, payload FROM events ORDER BY seq DESC LIMIT 1`).Scan(&kind, &payload); err != nil {
			t.Fatal(err)
		}
		var body struct{ Changes map[string]any }
		if err := json.Unmarshal([]byte(payload), &body); err != nil {
			t.Fatal(err)
		}
		return kind, body.Changes
	}
	step := func(kind, glyph string, img *core.IconImage) {
		t.Helper()
		before := eventCount(t, d)
		p, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, kind, glyph, img)
		if err != nil {
			t.Fatal(err)
		}
		if p.IconKind != kind || p.IconGlyph != glyph {
			t.Fatalf("got %q %q", p.IconKind, p.IconGlyph)
		}
		if n := eventCount(t, d) - before; n != 1 {
			t.Fatalf("%s: %d events", kind, n)
		}
		k, ch := newest()
		if k != core.EvProjectUpdated || ch["icon"] == nil || len(ch) != 1 {
			t.Fatalf("%s %v", k, ch)
		}
	}
	step("glyph", "rocket", nil)
	step("image", "", &core.IconImage{Type: "image/gif", Data: pngIcon})
	icon, err := d.RQ.GetProjectIcon(bg, f.Project.ID)
	if err != nil || icon.Type != "image/png" || icon.Etag == "" {
		t.Fatalf("%+v %v", icon, err)
	}
	// The same image again changes nothing and writes nothing.
	before := eventCount(t, d)
	if _, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, "image", "", &core.IconImage{Data: pngIcon}); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("a no-op wrote an event")
	}
	step("image", "", &core.IconImage{Data: webpIcon})
	icon2, _ := d.RQ.GetProjectIcon(bg, f.Project.ID)
	if icon2.Type != "image/webp" || icon2.Etag == icon.Etag {
		t.Fatalf("%+v", icon2)
	}
	// Replacing one image with another names both etags, so the change
	// shows though the kind stays "image".
	_, ch := newest()
	if got, _ := json.Marshal(ch["icon"]); string(got) != `{"from":{"etag":"`+icon.Etag+`","kind":"image"},"to":{"etag":"`+icon2.Etag+`","kind":"image"}}` {
		t.Fatalf("replace: %s", got)
	}
	// Back to the monogram keeps the stored image, so the image comes back
	// without an upload.
	step("monogram", "", nil)
	step("image", "", nil)

	// UpdateProject takes the kind and glyph too, in the same event as a
	// name change.
	before = eventCount(t, d)
	p, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{Name: new("Renamed"), IconKind: new("glyph"), IconGlyph: new("server")})
	if err != nil || p.IconKind != "glyph" || p.IconGlyph != "server" || p.Name != "Renamed" {
		t.Fatalf("%+v %v", p, err)
	}
	if eventCount(t, d) != before+1 {
		t.Fatal("one event for name and icon")
	}
	if _, ch := newest(); ch["icon"] == nil || ch["name"] == nil {
		t.Fatalf("%v", ch)
	}
}

func TestSetProjectIconRefusals(t *testing.T) {
	c, d, f := newFixture(t)
	for _, tc := range []struct {
		name        string
		kind, glyph string
		img         *core.IconImage
		want        string
	}{
		{"svg", "image", "", &core.IconImage{Type: "image/svg+xml", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)}, "Use a PNG, JPEG or WebP image."},
		{"300 KB", "image", "", &core.IconImage{Data: append(append([]byte{}, pngIcon...), make([]byte, 300<<10)...)}, "Use an image of 256 KB or less."},
		{"unknown glyph", "glyph", "unicorn", nil, "Choose a glyph from the list."},
		{"no glyph", "glyph", "", nil, "Choose a glyph from the list."},
		{"text named .png", "image", "", &core.IconImage{Type: "image/png", Data: []byte("just some text, not a picture")}, "Use a PNG, JPEG or WebP image."},
		{"image with none stored", "image", "", nil, "Choose an image to upload."},
		{"unknown kind", "emoji", "", nil, "Choose a monogram, a glyph or an image."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := snapshot(t, d)
			_, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, tc.kind, tc.glyph, tc.img)
			inv, ok := errors.AsType[*core.Invalid](err)
			if !ok || inv.Fields["icon"] != tc.want {
				t.Fatalf("%v", err)
			}
			if got := snapshot(t, d); !maps.Equal(got, snap) {
				t.Fatal("a refusal changed rows")
			}
		})
	}
	if _, err := c.SetProjectIcon(bg, f.Owner.ID, "nope", "monogram", "", nil); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("%v", err)
	}
	if _, err := c.UpdateProject(bg, f.Owner.ID, f.Project.ID, core.ProjectPatch{IconKind: new("glyph"), IconGlyph: new("unicorn")}); err == nil {
		t.Fatal("UpdateProject took an unknown glyph")
	}
}

func TestProjectGlyphsAreCurated(t *testing.T) {
	if n := len(core.ProjectGlyphs); n < 30 || n > 60 {
		t.Fatalf("%d glyphs", n)
	}
	seen := map[string]bool{}
	for _, g := range core.ProjectGlyphs {
		if seen[g] || g == "" {
			t.Fatalf("glyph %q twice or empty", g)
		}
		seen[g] = true
	}
}

func TestDeleteProjectRemovesItsIcon(t *testing.T) {
	c, d, f := newFixture(t)
	if _, err := c.SetProjectIcon(bg, f.Owner.ID, f.Project.ID, "image", "", &core.IconImage{Data: pngIcon}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteProject(bg, f.Owner.ID, f.Project.ID, "VAQ"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow(`SELECT count(*) FROM project_icons`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}
