package icons

import (
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
