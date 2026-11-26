package templates

import (
	"time"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func chipClass(on bool) string {
	base := "rounded-full border px-3 py-0.5 text-xs "
	if on {
		return base + "border-accent bg-accent text-paper"
	}
	return base + "border-line text-ink/80 hover:border-accent"
}

// cardClass gives ready cards the accent edge, and a ring while the card's
// link has focus (j and k move it).
func cardClass(ready bool) string {
	base := "cursor-grab rounded-md border border-line bg-paper p-2 shadow-sm hover:border-accent/60 focus-within:ring-2 focus-within:ring-accent"
	if ready {
		return base + " border-l-4 border-l-accent"
	}
	return base
}

func priorityClass(p int64) string {
	base := "inline-flex items-center gap-0.5 text-[10px] "
	switch {
	case p == 1:
		return base + "font-semibold text-danger"
	case p == 2:
		return base + "font-semibold text-accent"
	}
	return base + "text-muted"
}

// railsIn is the board's rails of one category, in status order.
func railsIn(b board.Board, category string) []board.Column {
	var out []board.Column
	for _, c := range b.Rails {
		if c.Status.Category == category {
			out = append(out, c)
		}
	}
	return out
}

// FilterHref is a project page's address with the filter's query.
func FilterHref(base string, f board.Filter) string {
	if q := f.Query().Encode(); q != "" {
		return base + "?" + q
	}
	return base
}

// olderHref is the list view of a done column's category, with the board's filter.
func olderHref(b board.Board, c board.Column) string {
	return FilterHref("/p/"+b.Project.Key+"/list", b.Filter.With("category", c.Status.Category))
}

// milestoneHref toggles the milestone filter to id.
func milestoneHref(b board.Board, id string) string {
	if b.Filter.Milestone == id {
		id = ""
	}
	return FilterHref("/p/"+b.Project.Key, b.Filter.With("milestone", id))
}

func maxOne(n int64) int64 { return max(n, 1) }

// milestoneFilterName names the milestone the board is filtered to ("No
// milestone" for none), or "" when it isn't filtered by one.
func milestoneFilterName(b board.Board) string {
	if b.Filter.Milestone == "none" {
		return "No milestone"
	}
	for _, m := range b.Milestones {
		if m.ID == b.Filter.Milestone {
			return m.Name
		}
	}
	return ""
}

func listEmpty(cols []board.Column) bool {
	for _, c := range cols {
		if len(c.Cards) > 0 {
			return false
		}
	}
	return true
}

// claimUntil is a lease's end before the page's script counts it down:
// "until 12:15 UTC".
func claimUntil(stamp string) string {
	t, err := timefmt.Parse(stamp)
	if err != nil {
		return ""
	}
	return "until " + timefmt.DisplayClock(t, time.UTC)
}

// showAssignee reports whether a card shows its assignee apart from its
// claim: the claimant is usually the assignee, and shows once.
func showAssignee(k board.Card) bool { return k.Assignee != "" && k.Assignee != k.ClaimedBy }

// railPanelClass is an open rail's cards. From md they sit in the rail;
// below md they overlay the columns beside the rail's strip, toward the
// board's middle: right of a backlog rail, left of a cancelled one.
func railPanelClass(category string) string {
	edge := "max-md:left-full max-md:ml-1"
	if category == "cancelled" {
		edge = "max-md:right-full max-md:mr-1"
	}
	return "px-2 pb-2 max-md:absolute max-md:top-0 max-md:z-20 max-md:w-64 max-md:max-w-[calc(100vw-5rem)] " +
		"max-md:rounded-md max-md:border max-md:border-line max-md:bg-paper-alt max-md:pt-2 max-md:shadow-lg " + edge
}
