package templates

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/core"
)

// EventView is one activity line: who did what, and how long ago.
type EventView struct {
	Actor, Kind, ProjectKey, TicketKey, TicketTitle, Ago string
	Payload                                              map[string]any
	// Seq is the event's sequence number (the history's "Show changes"
	// reads its diff by it).
	Seq int64

	// Reader is the key of the ticket whose page shows the line, so a link
	// event reads from that ticket's side; "" on the activity feed.
	Reader string

	// ActorID tells actors apart when FoldReorders compares lines.
	ActorID string
	// Times is how many reorders a folded line stands for (0 or 1: one).
	Times int
	// Target is a link event's other ticket as it is now, looked up by
	// payload.to_id (ResolveTargets); unset, the recorded key is shown.
	Target LinkTarget
}

// LinkTarget is a link event's other ticket, looked up by id when the
// history is shown. Known is false for an event with no to_id (or one not
// looked up); Key is "" when the ticket is gone.
type LinkTarget struct {
	Known bool
	Key   string
}

// TargetID is a link event's payload.to_id, "" for any other event.
func (e EventView) TargetID() string {
	if e.Kind != core.EvLinkCreated && e.Kind != core.EvLinkDeleted {
		return ""
	}
	id, _ := e.Payload["to_id"].(string)
	return id
}

// ResolveTargets sets Target on each link event from keys (ticket id to
// its current key; a missing id is a deleted ticket).
func ResolveTargets(evs []EventView, keys map[string]string) {
	for i := range evs {
		if id := evs[i].TargetID(); id != "" {
			evs[i].Target = LinkTarget{Known: true, Key: keys[id]}
		}
	}
}

// SentencePart is a piece of an event's sentence; Href links it to a ticket.
type SentencePart struct {
	Text, Href string
}

// EventParts is EventSentence in pieces, with the tickets a link event
// names linked (not "this ticket", a deleted ticket or a recorded key that
// may no longer lead anywhere). Other events are one unlinked piece.
func EventParts(e EventView) []SentencePart {
	if e.Kind != core.EvLinkCreated && e.Kind != core.EvLinkDeleted {
		return []SentencePart{{Text: EventSentence(e)}}
	}
	from := e.ticketPart(e.TicketKey, true)
	to := e.targetPart()
	if e.Kind == core.EvLinkDeleted {
		return []SentencePart{{Text: "removed a link from "}, from, {Text: " to "}, to}
	}
	kind, _ := e.Payload["kind"].(string)
	switch kind {
	case "blocks":
		return []SentencePart{{Text: "marked "}, from, {Text: " as blocking "}, to}
	case "duplicates":
		return []SentencePart{{Text: "marked "}, from, {Text: " as a duplicate of "}, to}
	}
	return []SentencePart{{Text: "related "}, from, {Text: " to "}, to}
}

// ticketPart names a ticket for the reader, linked when link is set and it
// isn't the reader's own ticket.
func (e EventView) ticketPart(key string, link bool) SentencePart {
	name := e.side(key)
	if !link || name != key || key == "" {
		return SentencePart{Text: name}
	}
	return SentencePart{Text: key, Href: "/t/" + key}
}

// targetPart names a link event's other ticket: its current key when it was
// looked up by id ("a deleted ticket" when gone), else the recorded key,
// unlinked.
func (e EventView) targetPart() SentencePart {
	if !e.Target.Known {
		to, _ := e.Payload["to"].(string)
		return e.ticketPart(to, false)
	}
	if e.Target.Key == "" {
		return SentencePart{Text: "a deleted ticket"}
	}
	return e.ticketPart(e.Target.Key, true)
}

// hasLinks reports whether any part of the sentence is linked.
func hasLinks(parts []SentencePart) bool {
	return slices.ContainsFunc(parts, func(p SentencePart) bool { return p.Href != "" })
}

// FoldReorders folds each run of consecutive ticket.reordered lines by the
// same actor, with no other line between them, into its newest line,
// counted in Times. Any other event, a status change included, ends a run.
func FoldReorders(evs []EventView) []EventView {
	var out []EventView
	for _, e := range evs {
		if n := len(out); n > 0 && e.Kind == core.EvTicketReordered {
			last := &out[n-1]
			if last.Kind == core.EvTicketReordered && last.ActorID == e.ActorID {
				last.Times = max(last.Times, 1) + 1
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// side is how a ticket is named for the reader: "this ticket" on its own page.
func (e EventView) side(key string) string {
	if e.Reader != "" && key == e.Reader {
		return "this ticket"
	}
	return key
}

// ParsePayload decodes an event's JSON payload; a bad one reads as empty.
func ParsePayload(raw string) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

// fieldWords names ticket fields in activity lines, in the order listed.
var fieldWords = [][2]string{
	{"title", "title"}, {"body", "description"}, {"status", "status"}, {"priority", "priority"},
	{"assignee", "assignee"}, {"parent", "parent"}, {"milestone", "milestone"}, {"tags", "tags"},
	{"tier", "tier"}, {"model", "model"},
}

// EventSentence is what happened, after the actor's name: "moved VAQ-4
// from Todo to In progress". An unknown kind reads "made a change".
func EventSentence(e EventView) string {
	s := func(k string) string { v, _ := e.Payload[k].(string); return v }
	switch e.Kind {
	case core.EvProjectCreated:
		return "created project " + e.ProjectKey
	case core.EvProjectUpdated:
		if from, to, ok := change(e.Payload, "key"); ok {
			return fmt.Sprintf("changed the key of %s to %s", from, to)
		}
		return projectUpdated(e)
	case core.EvProjectArchived:
		return "archived project " + e.ProjectKey
	case core.EvProjectRestored:
		return "restored project " + e.ProjectKey
	case core.EvProjectDeleted:
		return "deleted project " + s("key")
	case core.EvProjectLinked, core.EvProjectUnlinked:
		verb := "depends on"
		if s("kind") == "relates" {
			verb = "relates to"
		}
		said := e.ProjectKey + " " + verb + " " + s("to")
		if e.Kind == core.EvProjectUnlinked {
			return "removed the note that " + said
		}
		return "noted that " + said
	case core.EvStatusCreated:
		return fmt.Sprintf("added status %s to %s", s("name"), e.ProjectKey)
	case core.EvStatusUpdated:
		if from, to, ok := change(e.Payload, "name"); ok {
			return fmt.Sprintf("renamed status %s to %s", from, to)
		}
		return "renamed a status in " + e.ProjectKey
	case core.EvStatusMoved:
		return fmt.Sprintf("moved status %s in %s", s("name"), e.ProjectKey)
	case core.EvStatusDeleted:
		return fmt.Sprintf("deleted status %s from %s", s("name"), e.ProjectKey)
	case core.EvMilestoneCreated:
		return fmt.Sprintf("added milestone %s to %s", s("name"), e.ProjectKey)
	case core.EvMilestoneUpdated:
		return "updated milestone " + s("name")
	case core.EvMilestoneClosed:
		return "closed milestone " + s("name")
	case core.EvMilestoneReopened:
		return "reopened milestone " + s("name")
	case core.EvTicketCreated:
		// The title as created, not as it is now.
		title := s("title")
		if title == "" {
			title = e.TicketTitle
		}
		return "created " + e.TicketKey + " " + title
	case core.EvTicketUpdated:
		return ticketUpdated(e)
	case core.EvTicketMoved:
		return movedSentence(e)
	case core.EvTicketReordered:
		if e.Times > 1 {
			return fmt.Sprintf("reordered %s in %s %d times", e.TicketKey, s("status"), e.Times)
		}
		return fmt.Sprintf("reordered %s in %s", e.TicketKey, s("status"))
	case core.EvCommentCreated:
		return "commented on " + e.TicketKey
	case core.EvCommentUpdated:
		return "edited a comment on " + e.TicketKey
	case core.EvCommentDeleted:
		return "deleted a comment on " + e.TicketKey
	case core.EvAttachmentAdded:
		return "attached " + s("name") + " to " + e.TicketKey
	case core.EvAttachmentDeleted:
		return "removed the attachment " + s("name") + " from " + e.TicketKey
	case core.EvLinkCreated, core.EvLinkDeleted:
		var b strings.Builder
		for _, p := range EventParts(e) {
			b.WriteString(p.Text)
		}
		return b.String()
	case core.EvActorCreated:
		return "joined"
	case core.EvTokenCreated:
		return fmt.Sprintf("created API token %s (%s…)", s("name"), s("prefix"))
	case core.EvTokenRevoked:
		return fmt.Sprintf("revoked API token %s (%s…)", s("name"), s("prefix"))
	case core.EvTicketClaimed:
		if renewed, _ := e.Payload["renewed"].(bool); renewed {
			return "renewed the claim on " + e.TicketKey
		}
		return "claimed " + e.TicketKey
	case core.EvTicketReleased:
		return "released " + e.TicketKey
	case core.EvClaimLapsed:
		if st := s("status"); st != "" {
			return fmt.Sprintf("let the claim on %s lapse; it is back in %s", e.TicketKey, st)
		}
		return "let the claim on " + e.TicketKey + " lapse"
	case core.EvPlanCreated:
		return "wrote the plan for " + e.TicketKey
	case core.EvPlanRevised:
		if n, ok := e.Payload["revision"].(float64); ok {
			return fmt.Sprintf("revised the plan for %s (revision %d)", e.TicketKey, int64(n))
		}
		return "revised the plan for " + e.TicketKey
	case core.EvDecisionAsked, core.EvDecisionRuled, core.EvDecisionAnswered, core.EvDecisionWithdrawn:
		return decisionSentence(e)
	// A tag event is a project's; on a ticket's history (Reader set) it
	// reads as what happened to that ticket's tag.
	case core.EvTagRenamed:
		if e.Reader != "" {
			return fmt.Sprintf("renamed tag %s → %s", s("from"), s("to"))
		}
		return fmt.Sprintf("renamed tag %s to %s in %s (%s)", s("from"), s("to"), e.ProjectKey, tagTickets(e.Payload))
	case core.EvTagMerged:
		if e.Reader != "" {
			return fmt.Sprintf("merged tag %s into %s", s("from"), s("to"))
		}
		return fmt.Sprintf("merged tag %s into %s in %s (%s)", s("from"), s("to"), e.ProjectKey, tagTickets(e.Payload))
	case core.EvTagDeleted:
		if e.Reader != "" {
			return "deleted tag " + s("name")
		}
		return fmt.Sprintf("deleted tag %s from %s (%s)", s("name"), e.ProjectKey, tagTickets(e.Payload))
	}
	return "made a change"
}

// decisionSentence words a decision.* event: what was asked, ruled,
// answered (approved, overruled, relayed) or withdrawn, and about what.
func decisionSentence(e EventView) string {
	s := func(k string) string { v, _ := e.Payload[k].(string); return v }
	scope := func(prep string) string {
		ids, _ := e.Payload["ticket_ids"].([]any)
		switch {
		case e.TicketKey != "":
			return prep + " " + e.TicketKey
		case len(ids) > 1:
			return prep + " " + countLabel(int64(len(ids)), "ticket", "tickets") + " in " + e.ProjectKey
		}
		return "in " + e.ProjectKey
	}
	noun := map[string]string{"plan": "the plan", "merge": "a merge", "release": "a release"}[s("subject")]
	if noun == "" {
		noun = "a change"
	}
	answer := ""
	if a := s("answer"); a != "" {
		answer = ": " + a
	}
	if e.Payload["relayed"] == true {
		answer += " (relayed by " + s("relayed_by") + ")"
	}
	kind := s("kind")
	switch e.Kind {
	case core.EvDecisionAsked:
		if kind == core.DecisionApproval {
			return "asked to approve " + noun + " " + scope("for")
		}
		return "asked a question " + scope("about")
	case core.EvDecisionRuled:
		return "made a ruling " + scope("on") + answer
	case core.EvDecisionAnswered:
		switch {
		case kind == core.DecisionApproval && e.Payload["approved"] == true:
			return "approved " + noun + " " + scope("for") + strings.TrimPrefix(answer, ": "+s("answer"))
		case kind == core.DecisionApproval:
			return "answered the request to approve " + noun + " " + scope("for") + answer
		case kind == core.DecisionRuling:
			return "overruled a ruling " + scope("on") + answer
		}
		return "answered a question " + scope("about") + answer
	}
	if kind == core.DecisionApproval {
		return "withdrew the request to approve " + noun + " " + scope("for")
	}
	return "withdrew a question " + scope("about")
}

// movedSentence words a ticket.moved event, naming both projects: "moved
// VAQ-4 to OTH as OTH-2 with 2 sub-tickets; 1 open decision stays in VAQ".
func movedSentence(e EventView) string {
	s := func(k string) string { v, _ := e.Payload[k].(string); return v }
	out := fmt.Sprintf("moved %s to %s as %s", s("from"), s("to_project"), s("to"))
	if subs, _ := e.Payload["sub_tickets"].([]any); len(subs) > 0 {
		out += " with " + countLabel(int64(len(subs)), "sub-ticket", "sub-tickets")
	}
	if n, _ := e.Payload["open_decisions"].(float64); n > 0 {
		verb := " stays in "
		if n > 1 {
			verb = " stay in "
		}
		out += "; " + countLabel(int64(n), "open decision", "open decisions") + verb + s("from_project")
	}
	return out
}

// tagTickets is a tag event's ticket count: "1 ticket", "3 tickets".
func tagTickets(p map[string]any) string {
	n, _ := p["tickets"].(float64)
	return countLabel(int64(n), "ticket", "tickets")
}

func ticketUpdated(e EventView) string {
	changes, _ := e.Payload["changes"].(map[string]any)
	if len(changes) == 1 {
		if from, to, ok := change(e.Payload, "status"); ok {
			return fmt.Sprintf("moved %s from %s to %s", e.TicketKey, from, to)
		}
	}
	var names []string
	for _, f := range fieldWords {
		if _, ok := changes[f[0]]; ok {
			names = append(names, f[1])
		}
	}
	if len(names) == 0 {
		return "updated " + e.TicketKey
	}
	return fmt.Sprintf("changed the %s of %s", joinAnd(names), e.TicketKey)
}

// change reads one field's string from and to out of a payload's changes.
func change(p map[string]any, field string) (from, to string, ok bool) {
	changes, _ := p["changes"].(map[string]any)
	c, _ := changes[field].(map[string]any)
	if c == nil {
		return "", "", false
	}
	from, _ = c["from"].(string)
	to, _ = c["to"].(string)
	return from, to, true
}

// projectUpdated is a project.updated sentence: the agent workflow turned
// on or off and the tier map changed, when they did, or an icon change on
// its own, else "updated project VAQ".
func projectUpdated(e EventView) string {
	changes, _ := e.Payload["changes"].(map[string]any)
	_, tierMap := changes["tier_map"]
	var out string
	if c, ok := changes["workflow"].(map[string]any); ok {
		state := "off"
		if on, _ := c["to"].(bool); on {
			state = "on"
		}
		out = "turned the agent workflow " + state + " in " + e.ProjectKey
		if tierMap {
			out += " and changed its tier map"
		}
		return out
	}
	if tierMap {
		return "changed the tier map of " + e.ProjectKey
	}
	if _, icon := changes["icon"]; icon && len(changes) == 1 {
		return "changed the icon of " + e.ProjectKey
	}
	return "updated project " + e.ProjectKey
}

// joinAnd is "a", "a and b", "a, b and c".
func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(slices.Clone(xs[:len(xs)-1]), ", ") + " and " + xs[len(xs)-1]
}

// eventHref is where an activity line links: its ticket, else its project.
func eventHref(e EventView) string {
	switch {
	case e.TicketKey != "":
		return "/t/" + e.TicketKey
	case e.ProjectKey != "":
		return "/p/" + e.ProjectKey
	}
	return ""
}

// eventIcons are the glyphs for event kinds that don't follow their prefix.
var eventIcons = map[string]string{
	core.EvTicketCreated: "plus", core.EvTicketUpdated: "edit", core.EvTicketReordered: "moved", core.EvTicketMoved: "moved",
	core.EvProjectDeleted: "delete", core.EvStatusDeleted: "delete", core.EvCommentDeleted: "delete",
	core.EvLinkDeleted: "delete", core.EvActorCreated: "person",
	core.EvProjectLinked: "link", core.EvProjectUnlinked: "delete",
	core.EvTagMerged: "merge", core.EvTagDeleted: "delete",
	core.EvTicketClaimed: "claim", core.EvTicketReleased: "release", core.EvClaimLapsed: "lapsed",
}

// prefixIcons are the glyphs for each kind's first word.
var prefixIcons = map[string]string{
	"project": "project", "status": "settings", "milestone": "milestone",
	"comment": "comment", "attachment": "attachment", "link": "link", "token": "token", "tag": "tag",
	"plan": "plan", "decision": "decision",
}

// EventIcon is the glyph an activity line starts with.
func EventIcon(kind string) string {
	if i, ok := eventIcons[kind]; ok {
		return i
	}
	prefix, _, _ := strings.Cut(kind, ".")
	if i, ok := prefixIcons[prefix]; ok {
		return i
	}
	return "activity"
}
