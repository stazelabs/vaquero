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
}

// EventSentence is what happened, after the actor's name: "moved VAQ-4
// from Todo to In progress". An unknown kind reads "made a change".
func EventSentence(e EventView) string {
	s := func(k string) string { v, _ := e.Payload[k].(string); return v }
	switch e.Kind {
	case core.EvProjectCreated:
		return "created project " + e.ProjectKey
	case core.EvProjectUpdated:
		return "updated project " + e.ProjectKey
	case core.EvProjectArchived:
		return "archived project " + e.ProjectKey
	case core.EvProjectRestored:
		return "restored project " + e.ProjectKey
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
		return "created " + e.TicketKey + " " + e.TicketTitle
	case core.EvTicketUpdated:
		return ticketUpdated(e)
	case core.EvCommentCreated:
		return "commented on " + e.TicketKey
	case core.EvCommentUpdated:
		return "edited a comment on " + e.TicketKey
	case core.EvCommentDeleted:
		return "deleted a comment on " + e.TicketKey
	case core.EvLinkCreated:
		switch s("kind") {
		case "blocks":
			return fmt.Sprintf("marked %s as blocking %s", e.TicketKey, s("to"))
		case "duplicates":
			return fmt.Sprintf("marked %s as a duplicate of %s", e.TicketKey, s("to"))
		default:
			return fmt.Sprintf("related %s to %s", e.TicketKey, s("to"))
		}
	case core.EvLinkDeleted:
		return fmt.Sprintf("removed a link from %s to %s", e.TicketKey, s("to"))
	case core.EvActorCreated:
		return "joined"
	}
	return "made a change"
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
