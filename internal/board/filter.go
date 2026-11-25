package board

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/units"
)

// Filter narrows the board and list views. It lives in the page's query
// string, so a filtered view can be bookmarked.
type Filter struct {
	Mine, Agents bool
	Tag          string // "" any
	Priority     int64  // -1 any; 0 none .. 4 low
	Milestone    string // "" any; "none"; or a milestone id
	Assignee     string // "" any; an actor id
	Category     string // "" any; one of core.Categories
}

// maxValue bounds a filter value read from a URL, in characters.
const maxValue = 100

// ParseFilter reads a filter from a query string. A malformed or
// out-of-range value means "any"; an unknown id or tag is kept and simply
// matches nothing. Only the first of a repeated key counts.
func ParseFilter(q url.Values) Filter {
	f := Filter{Priority: -1}
	f.Mine = truthy(q.Get("mine"))
	f.Agents = truthy(q.Get("agents"))
	f.Tag = clip(q.Get("tag"))
	f.Milestone = clip(q.Get("milestone"))
	f.Assignee = clip(q.Get("assignee"))
	if n, err := strconv.ParseInt(q.Get("priority"), 10, 64); err == nil && n >= 0 && n <= 4 {
		f.Priority = n
	}
	if c := q.Get("category"); slices.Contains(core.Categories, c) {
		f.Category = c
	}
	return f
}

func truthy(s string) bool { return s != "" && s != "0" && s != "false" }

func clip(s string) string {
	s = strings.TrimSpace(s)
	return units.Clip(s, maxValue)
}

// Query is the filter as a query string; ParseFilter reads it back.
func (f Filter) Query() url.Values {
	q := url.Values{}
	if f.Mine {
		q.Set("mine", "1")
	}
	if f.Agents {
		q.Set("agents", "1")
	}
	if f.Tag != "" {
		q.Set("tag", f.Tag)
	}
	if f.Priority >= 0 {
		q.Set("priority", strconv.FormatInt(f.Priority, 10))
	}
	if f.Milestone != "" {
		q.Set("milestone", f.Milestone)
	}
	if f.Assignee != "" {
		q.Set("assignee", f.Assignee)
	}
	if f.Category != "" {
		q.Set("category", f.Category)
	}
	return q
}

// With is the filter with one key set to value ("" clears it): the target
// of a chip.
func (f Filter) With(key, value string) Filter {
	q := f.Query()
	if value == "" {
		q.Del(key)
	} else {
		q.Set(key, value)
	}
	return ParseFilter(q)
}

// Active says whether anything is being filtered.
func (f Filter) Active() bool { return len(f.Query()) > 0 }
