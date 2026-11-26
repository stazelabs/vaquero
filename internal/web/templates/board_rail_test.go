package templates

import (
	"regexp"
	"strings"
	"testing"
)

// VAQ-31: below md an open rail's cards overlay the columns, beside the
// rail's strip, with a close control; at md and up the rail opens in place
// as before.
func TestBoardRailOverlaysBelowMd(t *testing.T) {
	b := narrowBoard()
	page := html(t, BoardSection(b))
	for _, side := range []struct{ id, edge string }{{"s1", "max-md:left-full"}, {"s9", "max-md:right-full"}} {
		d := classOf(t, page, "details", `data-rail="`+side.id+`"`)
		for _, want := range []string{" relative ", " md:open:w-64 "} {
			if !strings.Contains(d, want) {
				t.Errorf("rail %s class %q lacks %q", side.id, d, want)
			}
		}
		if strings.Contains(d, " open:w-64 ") {
			t.Errorf("rail %s widens below md: %q", side.id, d)
		}
		p := classOf(t, page, "div", `data-rail-panel="`+side.id+`"`)
		for _, want := range []string{" max-md:absolute ", " max-md:top-0 ", " max-md:z-20 ", " max-md:w-64 ", " max-md:bg-paper-alt ", " " + side.edge + " "} {
			if !strings.Contains(p, want) {
				t.Errorf("rail %s panel class %q lacks %q", side.id, p, want)
			}
		}
	}
	if n := strings.Count(page, "data-rail-close"); n != 2 {
		t.Fatalf("%d close controls, want 2", n)
	}
	btn := regexp.MustCompile(`<button[^>]*data-rail-close[^>]*>`).FindString(page)
	for _, want := range []string{`type="button"`, `aria-label="Close Backlog"`, "md:hidden"} {
		if !strings.Contains(btn, want) {
			t.Errorf("close control %q lacks %q", btn, want)
		}
	}
	sum := classOf(t, page, "summary", "writing-mode:vertical-rl")
	if !strings.Contains(sum, " md:group-open:[writing-mode:horizontal-tb] ") || strings.Contains(sum, " group-open:[writing-mode:horizontal-tb] ") {
		t.Errorf("summary class %q: an open rail turns sideways from md only", sum)
	}
	if c := classOf(t, page, "div", `data-board-row`); !strings.Contains(c, " flex ") {
		t.Errorf("board row class %q", c)
	}
}
