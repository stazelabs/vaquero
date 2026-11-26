package templates

import "github.com/stazelabs/vaquero/internal/board"

const (
	laneClass   = "min-w-0 rounded-md bg-paper-alt p-2"
	laneHeading = "mb-2 flex items-center gap-1.5 px-1 text-xs font-semibold uppercase tracking-wider text-muted"
	moreClass   = "cursor-pointer list-none px-1 text-xs text-muted hover:text-accent [&::-webkit-details-marker]:hidden"
)

// nothingReady is the empty Next lane, naming Needs decision when the
// project has that status.
func nothingReady(fo board.Focus) string {
	if fo.NeedsDecision {
		return "Nothing ready. Triage the backlog or check Needs decision."
	}
	return "Nothing ready. Triage the backlog."
}

func flightEmpty(fo board.Focus) bool {
	for _, g := range fo.InFlight {
		if len(g.Cards) > 0 {
			return false
		}
	}
	return true
}

// flightHeader names an in-flight group: its run once runs exist, else
// its status.
func flightHeader(g board.FlightGroup) string {
	if g.Run != "" {
		return g.Run
	}
	return g.Status.Name
}
