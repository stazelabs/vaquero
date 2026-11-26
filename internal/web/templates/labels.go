package templates

import (
	"strings"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

var categoryLabels = map[string]string{
	"backlog": "Backlog", "todo": "Todo", "in_progress": "In progress",
	"in_review": "In review", "done": "Done", "cancelled": "Cancelled",
}

// CategoryLabel is a status category in words.
func CategoryLabel(c string) string {
	if l, ok := categoryLabels[c]; ok {
		return l
	}
	return c
}

// CategoryOptions are the categories for a Select, in board order.
func CategoryOptions() []Option {
	out := make([]Option, len(core.Categories))
	for i, c := range core.Categories {
		out[i] = Option{Value: c, Label: CategoryLabel(c)}
	}
	return out
}

var priorityLabels = []string{"No priority", "Urgent", "High", "Medium", "Low"}

// PriorityLabel is a ticket priority in words.
func PriorityLabel(p int64) string {
	if p < 0 || int(p) >= len(priorityLabels) {
		return priorityLabels[0]
	}
	return priorityLabels[p]
}

// TierLabel is an execution tier in words: "Standard", or "No tier".
func TierLabel(t string) string {
	if t == "" {
		return "No tier"
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// TierOptions are the tiers for a Select: none, then design to heavy.
func TierOptions() []Option {
	out := []Option{{Value: "", Label: "No tier"}}
	for _, t := range core.Tiers {
		out = append(out, Option{Value: t, Label: TierLabel(t)})
	}
	return out
}

// PriorityOptions are the priorities for a Select: none, then urgent to low.
func PriorityOptions() []Option {
	out := make([]Option, len(priorityLabels))
	for i, l := range priorityLabels {
		out[i] = Option{Value: itoa(int64(i)), Label: l}
	}
	return out
}

// dueLabel is a milestone's due date in words, or "".
func dueLabel(m gen.Milestone) string {
	if !m.DueOn.Valid {
		return ""
	}
	return "Due " + timefmt.Day(m.DueOn.String)
}

// countLabel is "1 open ticket" or "3 open tickets".
func countLabel(n int64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// mergeQuestion is the confirm dialog's question before t merges into the
// tag named into: "Merge bug into ui? Its 2 tickets in VAQ will be tagged
// ui instead."
func mergeQuestion(key string, t TagRow, into string) string {
	return "Merge " + t.Name + " into " + into + "? Its " + countLabel(t.Tickets, "ticket", "tickets") + " in " + key + " will be tagged " + into + " instead."
}

// closeOrReopen is the path suffix of a milestone's close or reopen action.
func closeOrReopen(m gen.Milestone) string {
	if m.ClosedAt.Valid {
		return "/reopen"
	}
	return "/close"
}

func formTitle(f TicketForm) string {
	if f.TicketKey == "" {
		return "New ticket"
	}
	return "Edit " + f.TicketKey
}

func formAction(f TicketForm) string {
	if f.TicketKey == "" {
		return "/p/" + f.Project.Key + "/tickets"
	}
	return "/t/" + f.TicketKey
}

func formCancel(f TicketForm) string {
	if f.TicketKey == "" {
		return "/p/" + f.Project.Key
	}
	return "/t/" + f.TicketKey
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// linkKindOptions words each link kind from this ticket's side.
func linkKindOptions() []Option {
	return []Option{{"blocks", "Blocks"}, {"relates", "Relates to"}, {"duplicates", "Duplicates"}}
}

// CategoryIcon is a status category's glyph name.
func CategoryIcon(c string) string { return "cat-" + strings.ReplaceAll(c, "_", "-") }

// categoryTint colours a category's glyph.
func categoryTint(c string) string {
	switch c {
	case "in_progress":
		return "text-accent"
	case "in_review":
		return "text-warn"
	case "done":
		return "text-ink/70"
	}
	return "text-muted"
}

var priorityIcons = []string{"", "priority-urgent", "priority-high", "priority-medium", "priority-low"}

// PriorityIcon is a priority's glyph name, or "" for no priority.
func PriorityIcon(p int64) string {
	if p < 0 || int(p) >= len(priorityIcons) {
		return ""
	}
	return priorityIcons[p]
}

// ProjectLinkKindOptions are the add-link form's kinds, as the sentence
// "This project depends on / relates to …" reads.
func ProjectLinkKindOptions() []Option {
	return []Option{{Value: "depends on", Label: "Depends on"}, {Value: "relates", Label: "Relates to"}}
}

// projectLinkLabel is how a project's link reads from its side: "Depends
// on", "Used by" or "Relates to".
func projectLinkLabel(l board.ProjectLink) string {
	switch {
	case l.Kind == "depends on" && l.Direction == "out":
		return "Depends on"
	case l.Kind == "depends on":
		return "Used by"
	}
	return "Relates to"
}

// linkGroup is the projects a project's links name under one label.
type linkGroup struct {
	Label string
	Links []board.ProjectLink
}

// linkGroups gathers links (in LoadProjectLinks' order) under their labels.
func linkGroups(links []board.ProjectLink) []linkGroup {
	var out []linkGroup
	for _, l := range links {
		label := projectLinkLabel(l)
		if n := len(out); n > 0 && out[n-1].Label == label {
			out[n-1].Links = append(out[n-1].Links, l)
			continue
		}
		out = append(out, linkGroup{Label: label, Links: []board.ProjectLink{l}})
	}
	return out
}
