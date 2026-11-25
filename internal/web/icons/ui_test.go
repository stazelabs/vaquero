package icons

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestUIGlyphsAreDrawingOnly(t *testing.T) {
	names := UINames()
	if len(names) < 30 {
		t.Fatalf("%d glyphs", len(names))
	}
	for _, n := range names {
		m, _ := UI(n)
		if m.Inner == "" || strings.Contains(m.Inner, "script") || strings.Contains(m.Inner, "on") && strings.Contains(m.Inner, "=\"javascript") {
			t.Errorf("%s: %q", n, m.Inner)
		}
	}
	if _, ok := UI("nope"); ok {
		t.Fatal("unknown name found")
	}
}

// Every glyph is drawing elements only, from the generator's allowlist,
// each with only its allowed attributes (and the filled dot's fill).
func TestUIGlyphsMatchTheAllowlistShape(t *testing.T) {
	allowed := map[string][]string{
		"path": {"d"}, "circle": {"cx", "cy", "r", "fill"}, "ellipse": {"cx", "cy", "rx", "ry"},
		"rect": {"x", "y", "width", "height", "rx", "ry"}, "line": {"x1", "y1", "x2", "y2"},
		"polyline": {"points"}, "polygon": {"points"},
	}
	elem := regexp.MustCompile(`<([a-z]+)((?: [a-z0-9-]+="[^"]*")*)></([a-z]+)>`)
	attr := regexp.MustCompile(` ([a-z0-9-]+)="([^"]*)"`)
	value := regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz0-9.,\- ]+$|^currentColor$`)
	for _, n := range UINames() {
		m, _ := UI(n)
		rest := m.Inner
		for _, e := range elem.FindAllStringSubmatch(m.Inner, -1) {
			rest = strings.Replace(rest, e[0], "", 1)
			if e[1] != e[3] || allowed[e[1]] == nil {
				t.Errorf("%s: element <%s>", n, e[1])
				continue
			}
			for _, a := range attr.FindAllStringSubmatch(e[2], -1) {
				if !slices.Contains(allowed[e[1]], a[1]) || !value.MatchString(a[2]) {
					t.Errorf("%s: <%s %s=%q>", n, e[1], a[1], a[2])
				}
			}
		}
		if rest != "" {
			t.Errorf("%s: unparsed markup %q", n, rest)
		}
	}
}
