package templates

import (
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
)

// FilterBar is the filter row shared by Focus, the board and the list:
// Mine and Agents chips, then Priority, Milestone and Tags menus (and a
// Status menu with Categories, the list's), then a removable chip per
// active filter.
type FilterBar struct {
	Base       string // the page's address, without a query
	Filter     board.Filter
	Tags       []string     // every tag in the project, sorted
	Milestones []MenuOption // the open milestones
	Categories bool
	// AssigneeName names the assignee filter's actor (it is set only from
	// an address); "" shows the filter as "Assignee".
	AssigneeName string
}

// MenuOption is one choice in a filter menu: the filter value and its words.
type MenuOption struct{ Value, Label string }

// MenuItem is one link in a filter menu.
type MenuItem struct {
	Label, Href string
	On          bool
}

func (b FilterBar) href(f board.Filter) string { return FilterHref(b.Base, f) }

// toggle is a quick chip: a link to the filter with key switched.
func (b FilterBar) toggle(label, key string, on bool) Chip {
	next := b.Filter.With(key, "1")
	if on {
		next = b.Filter.With(key, "")
	}
	return Chip{Label: label, Href: b.href(next), On: on}
}

// item is a menu link setting key to value, or clearing it when it is
// already the value.
func (b FilterBar) item(label, key, value string, on bool) MenuItem {
	next := b.Filter.With(key, value)
	if on {
		next = b.Filter.With(key, "")
	}
	return MenuItem{Label: label, Href: b.href(next), On: on}
}

func (b FilterBar) priorityItems() []MenuItem {
	var out []MenuItem
	for p := int64(1); p <= 4; p++ {
		out = append(out, b.item(PriorityLabel(p), "priority", strconv.FormatInt(p, 10), b.Filter.Priority == p))
	}
	return append(out, b.item(PriorityLabel(0), "priority", "0", b.Filter.Priority == 0))
}

func (b FilterBar) milestoneItems() []MenuItem {
	var out []MenuItem
	for _, m := range b.Milestones {
		out = append(out, b.item(m.Label, "milestone", m.Value, b.Filter.Milestone == m.Value))
	}
	return append(out, b.item("No milestone", "milestone", "none", b.Filter.Milestone == "none"))
}

// tagItems lists the project's tags, keeping the active one even when no
// ticket carries it (in any case or normalisation).
func (b FilterBar) tagItems() []MenuItem {
	var out []MenuItem
	found := false
	for _, t := range b.Tags {
		on := b.Filter.Tag != "" && core.SameTag(t, b.Filter.Tag)
		found = found || on
		out = append(out, b.item(t, "tag", t, on))
	}
	if b.Filter.Tag != "" && !found {
		out = append(out, b.item(b.Filter.Tag, "tag", b.Filter.Tag, true))
	}
	return out
}

func (b FilterBar) categoryItems() []MenuItem {
	var out []MenuItem
	for _, c := range core.Categories {
		out = append(out, b.item(CategoryLabel(c), "category", c, b.Filter.Category == c))
	}
	return out
}

// active is a removable chip per filter in force beyond Mine and Agents:
// each links to the filter without it.
func (b FilterBar) active() []Chip {
	f := b.Filter
	var out []Chip
	add := func(label, key string) {
		out = append(out, Chip{Label: label, Href: b.href(f.With(key, "")), On: true})
	}
	if f.Priority >= 0 {
		add("Priority: "+PriorityLabel(f.Priority), "priority")
	}
	if f.Milestone != "" {
		name := "No milestone"
		if f.Milestone != "none" {
			name = "Unknown"
			for _, m := range b.Milestones {
				if m.Value == f.Milestone {
					name = m.Label
				}
			}
		}
		add("Milestone: "+name, "milestone")
	}
	if f.Tag != "" {
		label := f.Tag
		for _, t := range b.Tags {
			if core.SameTag(t, f.Tag) {
				label = t
			}
		}
		add("Tag: "+label, "tag")
	}
	if f.Category != "" && b.Categories {
		add("Status: "+CategoryLabel(f.Category), "category")
	}
	if f.Assignee != "" {
		label := "Assignee"
		if b.AssigneeName != "" {
			label += ": " + b.AssigneeName
		}
		add(label, "assignee")
	}
	return out
}

// menuOn says whether a menu's filter is in force, so its button reads as on.
func menuOn(items []MenuItem) bool {
	for _, i := range items {
		if i.On {
			return true
		}
	}
	return false
}

// menuButtonClass is a menu's summary, drawn like a chip.
func menuButtonClass(on bool) string {
	return chipClass(on) + " inline-flex cursor-pointer list-none items-center gap-1 [&::-webkit-details-marker]:hidden"
}

func lower(s string) string { return strings.ToLower(s) }

func menuItemClass(on bool) string {
	base := "block rounded px-2 py-1 text-sm hover:bg-paper-alt hover:text-accent focus-visible:outline-2 focus-visible:outline-accent "
	if on {
		return base + "font-semibold text-accent"
	}
	return base + "text-ink"
}
