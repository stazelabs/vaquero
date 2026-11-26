package templates

import (
	"strings"

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

func hasArchived(rows []gen.ListProjectsRow) bool {
	for _, p := range rows {
		if p.ArchivedAt.Valid {
			return true
		}
	}
	return false
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
