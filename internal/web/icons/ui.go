package icons

import (
	"maps"
	"slices"
)

// Markup is one UI glyph from Lucide (ISC), chosen by purpose in ui.json:
// the children of a 24x24 stroke icon. The generator keeps only drawing
// elements with numeric or path-data attributes, so Inner is safe to place
// inside an <svg> as is.
type Markup struct {
	Name  string
	Inner string
}

// UI returns the glyph listed in ui.json under name.
func UI(name string) (Markup, bool) {
	inner, ok := uiIcons[name]
	if !ok {
		return Markup{}, false
	}
	return Markup{Name: name, Inner: inner}, true
}

// UINames are the listed glyphs' names, sorted.
func UINames() []string { return slices.Sorted(maps.Keys(uiIcons)) }
