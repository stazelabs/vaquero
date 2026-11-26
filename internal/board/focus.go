package board

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// How many cards each Focus lane shows before "N more".
const (
	FocusNext     = 5
	FocusFlight   = 8
	FocusFinished = 8
)

// Focus is a project's landing view: what's next, what's in flight and
// what just finished. Each lane shows its first cards; the rest are kept
// (NextRest, Rest, FinishedRest) for the page to open in place, and
// counted (NextMore, More, FinishedMore).
type Focus struct {
	Project gen.Project
	Filter  Filter
	// Next is the ready tickets, by priority (Urgent first, no priority
	// last), then board position; with Mine, the owner's todo tickets come
	// first, in the same order.
	Next, NextRest []Card
	NextMore       int
	// Waiting is the todo tickets that aren't ready, each with why.
	Waiting []Waiting
	// InFlight is a group per in progress status, then per in review
	// status, in board order. Once runs exist, a group per run.
	InFlight []FlightGroup
	// Finished is the tickets done within DoneWindow, newest first, each
	// with its day in When.
	Finished, FinishedRest []Card
	FinishedMore           int
	// NeedsDecision says the project has a "Needs decision" status, which
	// the empty Next lane points to.
	NeedsDecision bool
	// Tags is every tag on the project's tickets, as Board.Tags.
	Tags []string
}

// Waiting is a todo ticket that isn't ready, and why: "Blocked by KEY",
// "Has open sub-tickets", "Claimed by NAME", "Assigned to NAME", then with
// the workflow on its readiness reasons.
type Waiting struct {
	Card
	Reason string
}

// FlightGroup is in-flight work under one header. Run is empty until runs
// exist; until then Status names the group.
type FlightGroup struct {
	Run    string
	Status gen.Status
	Cards  []Card
	Rest   []Card
	More   int
}

// NeedsDecisionStatus is the triage status the empty Next lane names.
const NeedsDecisionStatus = "Needs decision"

// LoadFocus builds the Focus view as of now, narrowed by f as the board
// is (f.Category is ignored and dropped). With f.Mine, Next is the
// owner's todo tickets, then the ready ones (ruling M-R2): the ready ones
// are unassigned, so they are read without Mine, and nothing waits.
func LoadFocus(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter, now time.Time) (Focus, error) {
	f.Category = ""
	cols, tags, err := columns(ctx, q, p, owner, f, time.Time{})
	if err != nil {
		return Focus{}, err
	}
	fo := Focus{Project: p, Filter: f, Tags: tags}
	var next, mine, waiting, finished []Card
	if f.Mine {
		all := f
		all.Mine = false
		allCols, _, err := columns(ctx, q, p, owner, all, time.Time{})
		if err != nil {
			return Focus{}, err
		}
		for _, c := range allCols {
			if c.Status.Category == "todo" {
				for _, k := range c.Cards {
					if k.Ready {
						next = append(next, k)
					}
				}
			}
		}
	}
	cutoff := now.Add(-DoneWindow)
	for _, c := range cols {
		if strings.EqualFold(c.Status.Name, NeedsDecisionStatus) {
			fo.NeedsDecision = true
		}
		switch c.Status.Category {
		case "todo":
			for _, k := range c.Cards {
				if f.Mine {
					mine = append(mine, k) // the Mine filter read only the owner's
					continue
				}
				if k.Ready {
					next = append(next, k)
				} else {
					waiting = append(waiting, k)
				}
			}
		case "in_progress", "in_review":
			g := FlightGroup{Status: c.Status}
			g.Cards, g.Rest, g.More = split(c.Cards, FocusFlight)
			fo.InFlight = append(fo.InFlight, g)
		case "done":
			for _, k := range c.Cards {
				at, err := timefmt.Parse(k.StatusChangedAt)
				if err != nil || at.Before(cutoff) {
					continue
				}
				k.When = timefmt.DayLabel(at, now)
				finished = append(finished, k)
			}
		}
	}
	// In progress groups before in review ones, each in board order.
	slices.SortStableFunc(fo.InFlight, func(a, b FlightGroup) int {
		return flightRank(a.Status.Category) - flightRank(b.Status.Category)
	})
	// The rows come in board order, so a stable sort by priority keeps it
	// within each priority.
	byPriority := func(a, b Card) int { return priorityRank(a.Priority) - priorityRank(b.Priority) }
	slices.SortStableFunc(next, byPriority)
	slices.SortStableFunc(mine, byPriority)
	fo.Next, fo.NextRest, fo.NextMore = split(append(mine, next...), FocusNext)
	slices.SortStableFunc(finished, func(a, b Card) int { return strings.Compare(b.StatusChangedAt, a.StatusChangedAt) })
	fo.Finished, fo.FinishedRest, fo.FinishedMore = split(finished, FocusFinished)
	if len(waiting) > 0 {
		if fo.Waiting, err = reasons(ctx, q, p, waiting); err != nil {
			return Focus{}, err
		}
	}
	return fo, nil
}

func split(cs []Card, n int) (shown, rest []Card, more int) {
	if len(cs) <= n {
		return cs, nil, 0
	}
	return cs[:n], cs[n:], len(cs) - n
}

// priorityRank orders priorities Urgent (1) to Low (4), then none (0).
func priorityRank(p int64) int {
	if p <= 0 {
		return 5
	}
	return int(p)
}

func flightRank(category string) int {
	if category == "in_progress" {
		return 0
	}
	return 1
}

// reasons says why each waiting card isn't ready, in two reads for the
// whole project.
func reasons(ctx context.Context, q *gen.Queries, p gen.Project, cards []Card) ([]Waiting, error) {
	blockRows, err := q.ListOpenBlockers(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	blockers := map[string][]string{}
	for _, r := range blockRows {
		k := core.TicketKey{Project: r.BlockerProject, Number: r.BlockerNumber}.String()
		blockers[r.TicketID] = append(blockers[r.TicketID], k)
	}
	parentRows, err := q.ListParentsWithOpenSubTickets(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]bool, len(parentRows))
	for _, id := range parentRows {
		parents[id] = true
	}
	out := make([]Waiting, 0, len(cards))
	for _, k := range cards {
		var why []string
		if b := blockers[k.ID]; len(b) > 0 {
			why = append(why, "Blocked by "+strings.Join(b, ", "))
		}
		if parents[k.ID] {
			why = append(why, "Has open sub-tickets")
		}
		switch {
		case k.ClaimedBy != "":
			why = append(why, "Claimed by "+k.ClaimedBy)
		case k.Assignee != "":
			why = append(why, "Assigned to "+k.Assignee)
		}
		if k.Readiness != nil && len(k.Readiness.Reasons) > 0 {
			why = append(why, strings.Join(k.Readiness.Reasons, " "))
		}
		out = append(out, Waiting{Card: k, Reason: strings.Join(why, " · ")})
	}
	return out, nil
}
