package templates

import (
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// cardClass is a board card: the accent edge when ready, and a ring while
// the card's link has focus (j and k move it), with the grab cursor.
func cardClass(ready bool) string {
	base := "rounded-sm border border-line bg-paper px-2 py-1.5 hover:border-accent/60 focus-within:ring-2 focus-within:ring-accent"
	base = "cursor-grab " + base
	if ready {
		return base + " border-l-2 border-l-accent"
	}
	return base
}

func priorityClass(p int64) string {
	base := "inline-flex items-center gap-0.5 text-xs "
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
	return FilterHref("/p/"+b.Project.Key+"/board", b.Filter.With("milestone", id))
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

func listEmpty(groups []ListGroup) bool {
	for _, g := range groups {
		if len(g.Column.Cards) > 0 {
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

// ClaimNote is "Claimed by bot until 12:15 UTC" plus end, when someone
// other than viewerID holds a live claim (it ends after now); "" when the
// viewer holds it, nobody does, or the lease has run out. The API's write
// answers carry it as their warning and the edit form shows it: claims are
// advisory, so neither stops the edit.
func ClaimNote(claimantID, claimant, expires, viewerID string, now time.Time, end string) string {
	if claimantID == "" || claimantID == viewerID || claimant == "" {
		return ""
	}
	t, err := timefmt.Parse(expires)
	if err != nil || !t.After(now) {
		return ""
	}
	return "Claimed by " + claimant + " " + claimUntil(expires) + end
}

// listPageHref is the address of a list group's next page.
func listPageHref(p gen.Project, f board.Filter, g ListGroup) string {
	q := f.Query()
	q.Set("group", g.Column.Status.ID)
	q.Set("offset", strconv.Itoa(g.Next))
	return "/p/" + p.Key + "/list?" + q.Encode()
}

// cardAttrs are the attributes every drawing of a ticket carries, card or
// row: app.js finds tickets by data-card (drags, j and k), and tests and
// scripts read data-ready.
func cardAttrs(k board.Card) templ.OrderedAttributes {
	return templ.OrderedAttributes{
		{Key: "data-card", Value: k.Key},
		{Key: "data-ready", Value: k.Ready},
	}
}

// hasCardMeta reports whether a board card has anything for its meta line.
func hasCardMeta(k board.Card) bool {
	return k.Ready || k.Priority > 0 || k.Tier != "" || len(k.Tags) > 0 || k.When != "" || k.ClaimedBy != "" || showAssignee(k)
}

// priorityMarkClass is a row's priority slot: always as wide as a glyph,
// so the numbers line up, tinted as the card's priority is.
func priorityMarkClass(p int64) string {
	base := "flex w-3.5 shrink-0 items-center "
	switch p {
	case 1:
		return base + "text-danger"
	case 2:
		return base + "text-accent"
	}
	return base + "text-muted"
}

// keyPrefix and keyNumber split a ticket key: a row shows the number
// alone inside its project ("12") and keeps "VAQ-" for screen readers.
func keyPrefix(key string) string {
	if i := strings.LastIndexByte(key, '-'); i >= 0 {
		return key[:i+1]
	}
	return ""
}

func keyNumber(key string) string {
	return key[len(keyPrefix(key)):]
}
