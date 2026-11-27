package templates

import (
	"html/template"
	"net/url"
	"strconv"
)

// DecisionCardView is one decision as a card the owner answers in place:
// the inbox's Decide and Rulings, and the ticket page (VAQ-172).
type DecisionCardView struct {
	ID         string
	Kind       string // question, approval or ruling
	Subject    string // an approval's: plan, merge or release
	ProjectKey string
	AskedBy    string
	Age        string
	Question   template.HTML // rendered Markdown
	// Options in the order shown: the recommended one first.
	Options []CardOption
	// Covers is what approving a plan approval covers, as asked; PlanKey
	// the ticket whose plan Show plan opens ("" for none).
	Covers, PlanKey string
	Tickets         []TicketRef
	// Error is core's refusal of the last answer, shown under the buttons.
	Error string
	// Answered says how an answered decision was answered, or a ruling's
	// call ("claude ruled Left").
	Answered string
	// Back is where the card's forms return without JavaScript.
	Back string
	// Note is the note sent with a refused answer, kept in its box.
	Note string
}

// CardOption is one option's button and the trade-off under it.
type CardOption struct {
	Label, Tradeoff string
	Recommended     bool
}

// InboxChip is one filter chip along the inbox's top.
type InboxChip struct {
	Label, Href string
	On          bool
}

// InboxReviewRow is a ticket waiting in review.
type InboxReviewRow struct {
	Key, Title, MovedBy, Age string
	Priority                 int64
}

// InboxLapse is a claim that lapsed lately; Seq is its event, which
// app.js's Dismiss records.
type InboxLapse struct {
	Seq                    int64
	Agent, Key, Title, Ago string
}

// InboxRuling is a ruling an agent made, folded to its question and call.
type InboxRuling struct {
	Card     DecisionCardView
	Question string
	Ruled    string
}

// InboxAnswered is a decision answered today, with how.
type InboxAnswered struct {
	ID, ProjectKey, Question, Answer, How, Ago string
}

// InboxView is /inbox.
type InboxView struct {
	Self     string // the page's address with its filters, for live refreshes
	Chips    []InboxChip
	Decide   []DecisionCardView
	Review   []InboxReviewRow
	Check    []InboxLapse
	Rulings  []InboxRuling
	Answered []InboxAnswered
}

// empty reports whether nothing waits on the owner.
func (v InboxView) empty() bool {
	return len(v.Decide)+len(v.Review)+len(v.Check)+len(v.Rulings) == 0
}

// kindLabel is a card's kind as its head line names it.
func kindLabel(c DecisionCardView) string {
	switch c.Kind {
	case "approval":
		switch c.Subject {
		case "plan":
			return "Plan approval"
		case "merge":
			return "Merge approval"
		case "release":
			return "Release approval"
		}
		return "Approval"
	case "ruling":
		return "Ruling"
	}
	return "Question"
}

// withdrawQuestion is the confirm dialog's question for a card's Withdraw.
func withdrawQuestion(c DecisionCardView) string {
	if c.Kind == "approval" {
		return "Withdraw this approval? The agent can ask again."
	}
	return "Withdraw this question? The agent can ask again."
}

func cardAnswerURL(id string) string   { return "/inbox/" + url.PathEscape(id) + "/answer" }
func cardWithdrawURL(id string) string { return "/inbox/" + url.PathEscape(id) + "/withdraw" }

// optionClass is an option's button: the recommended one filled, the rest
// outlined, so no option looks preferred unless the agent said so.
func optionClass(recommended bool) string {
	base := "inline-flex h-chip items-center gap-1.5 rounded-sm px-2 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 max-sm:h-10.5 max-sm:text-sm"
	if recommended {
		return base + " bg-accent text-paper hover:bg-accent/90"
	}
	return base + " border border-line bg-paper text-ink hover:border-accent"
}

// optionKey is an option's number for the 1 to 9 keys, "" past nine.
func optionKey(i int) string {
	if i >= 9 {
		return ""
	}
	return strconv.Itoa(i + 1)
}

func countText(n int) string { return strconv.Itoa(n) }
