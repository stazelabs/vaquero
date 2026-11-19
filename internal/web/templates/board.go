package templates

import (
	"time"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func boolAttr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func chipClass(on bool) string {
	base := "rounded-full border px-3 py-0.5 text-xs "
	if on {
		return base + "border-accent bg-accent text-paper"
	}
	return base + "border-line text-ink/80 hover:border-accent"
}

// cardClass gives ready cards the accent edge.
func cardClass(ready bool) string {
	base := "cursor-grab rounded-md border border-line bg-paper p-2 shadow-sm hover:border-accent/60"
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

func listHref(b board.Board) string { return FilterHref("/p/"+b.Project.Key+"/list", b.Filter) }

// milestoneHref toggles the milestone filter to id.
func milestoneHref(b board.Board, id string) string {
	if b.Filter.Milestone == id {
		id = ""
	}
	return FilterHref("/p/"+b.Project.Key, b.Filter.With("milestone", id))
}

func maxOne(n int64) int64 { return max(n, 1) }

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
