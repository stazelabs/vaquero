package templates

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestEveryEventKindHasWording(t *testing.T) {
	kinds := core.EventKinds()
	if len(kinds) < 20 {
		t.Fatalf("only %d kinds", len(kinds))
	}
	for _, k := range kinds {
		e := EventView{Kind: k, ProjectKey: "VAQ", TicketKey: "VAQ-1", Payload: map[string]any{"name": "x", "kind": "blocks", "to": "VAQ-2"}}
		if s := EventSentence(e); s == "made a change" || s == "" {
			t.Errorf("%s has no wording", k)
		}
	}
	if s := EventSentence(EventView{Kind: "nope.nope"}); s != "made a change" {
		t.Fatalf("unknown kind: %q", s)
	}
}

func TestJoinAnd(t *testing.T) {
	for want, in := range map[string][]string{"": nil, "a": {"a"}, "a and b": {"a", "b"}, "a, b and c": {"a", "b", "c"}} {
		if got := joinAnd(in); got != want {
			t.Errorf("%v → %q", in, got)
		}
	}
}

func TestAttachmentWording(t *testing.T) {
	p := map[string]any{"name": "shot.png"}
	if got := EventSentence(EventView{Kind: core.EvAttachmentAdded, TicketKey: "VAQ-3", Payload: p}); got != "attached shot.png to VAQ-3" {
		t.Fatal(got)
	}
	if got := EventSentence(EventView{Kind: core.EvAttachmentDeleted, TicketKey: "VAQ-3", Payload: p}); got != "removed the attachment shot.png from VAQ-3" {
		t.Fatal(got)
	}
	if EventIcon(core.EvAttachmentAdded) != "attachment" || EventIcon(core.EvAttachmentDeleted) != "attachment" {
		t.Fatal("icon")
	}
}

func TestLinkWordingTakesTheReadersSide(t *testing.T) {
	e := func(kind, reader string) EventView {
		return EventView{Kind: core.EvLinkCreated, TicketKey: "VAQ-2", Reader: reader, Payload: map[string]any{"kind": kind, "to": "VAQ-1"}}
	}
	for _, c := range []struct {
		kind, reader, want string
	}{
		{"blocks", "", "marked VAQ-2 as blocking VAQ-1"},
		{"blocks", "VAQ-2", "marked this ticket as blocking VAQ-1"},
		{"blocks", "VAQ-1", "marked VAQ-2 as blocking this ticket"},
		{"duplicates", "VAQ-1", "marked VAQ-2 as a duplicate of this ticket"},
		{"duplicates", "VAQ-2", "marked this ticket as a duplicate of VAQ-1"},
		{"relates", "VAQ-1", "related VAQ-2 to this ticket"},
	} {
		if got := EventSentence(e(c.kind, c.reader)); got != c.want {
			t.Errorf("%s as %q: %q, want %q", c.kind, c.reader, got, c.want)
		}
	}
	d := EventView{Kind: core.EvLinkDeleted, TicketKey: "VAQ-2", Reader: "VAQ-1", Payload: map[string]any{"to": "VAQ-1"}}
	if got := EventSentence(d); got != "removed a link from VAQ-2 to this ticket" {
		t.Fatal(got)
	}
}

func TestTagWording(t *testing.T) {
	for _, c := range []struct {
		kind string
		p    map[string]any
		want string
	}{
		{core.EvTagRenamed, map[string]any{"from": "bug", "to": "defect", "tickets": 1.0}, "renamed tag bug to defect in VAQ (1 ticket)"},
		{core.EvTagMerged, map[string]any{"from": "bug", "to": "ui", "tickets": 3.0}, "merged tag bug into ui in VAQ (3 tickets)"},
		{core.EvTagDeleted, map[string]any{"name": "bug", "tickets": 2.0}, "deleted tag bug from VAQ (2 tickets)"},
	} {
		if got := EventSentence(EventView{Kind: c.kind, ProjectKey: "VAQ", Payload: c.p}); got != c.want {
			t.Errorf("%s: %q, want %q", c.kind, got, c.want)
		}
	}
	if EventIcon(core.EvTagRenamed) != "tag" || EventIcon(core.EvTagMerged) != "merge" || EventIcon(core.EvTagDeleted) != "delete" {
		t.Error("tag icons")
	}
}

func TestDecisionWording(t *testing.T) {
	ev := func(kind string, p map[string]any, ticket string) EventView {
		return EventView{Kind: kind, ProjectKey: "VAQ", TicketKey: ticket, Payload: p}
	}
	group := []any{"a", "b", "c"}
	for _, c := range []struct {
		e    EventView
		want string
	}{
		{ev(core.EvDecisionAsked, map[string]any{"kind": "question", "ticket_ids": []any{"a"}}, "VAQ-3"), "asked a question about VAQ-3"},
		{ev(core.EvDecisionAsked, map[string]any{"kind": "question", "ticket_ids": []any{}}, ""), "asked a question in VAQ"},
		{ev(core.EvDecisionAsked, map[string]any{"kind": "approval", "subject": "plan", "ticket_ids": group}, ""), "asked to approve the plan for 3 tickets in VAQ"},
		{ev(core.EvDecisionAsked, map[string]any{"kind": "approval", "subject": "release", "ticket_ids": []any{}}, ""), "asked to approve a release in VAQ"},
		{ev(core.EvDecisionRuled, map[string]any{"kind": "ruling", "answer": "light", "ticket_ids": []any{"a"}}, "VAQ-3"), "made a ruling on VAQ-3: light"},
		{ev(core.EvDecisionAnswered, map[string]any{"kind": "question", "answer": "By age", "ticket_ids": []any{"a"}}, "VAQ-3"), "answered a question about VAQ-3: By age"},
		{ev(core.EvDecisionAnswered, map[string]any{"kind": "question", "answer": "By age", "relayed": true, "relayed_by": "builder", "ticket_ids": []any{"a"}}, "VAQ-3"), "answered a question about VAQ-3: By age (relayed by builder)"},
		{ev(core.EvDecisionAnswered, map[string]any{"kind": "approval", "subject": "plan", "answer": "Approve", "approved": true, "ticket_ids": group}, ""), "approved the plan for 3 tickets in VAQ"},
		{ev(core.EvDecisionAnswered, map[string]any{"kind": "approval", "subject": "merge", "answer": "Not yet", "ticket_ids": []any{"a"}}, "VAQ-3"), "answered the request to approve a merge for VAQ-3: Not yet"},
		{ev(core.EvDecisionAnswered, map[string]any{"kind": "ruling", "answer": "heavy", "overruled": map[string]any{"answer": "light"}, "ticket_ids": []any{"a"}}, "VAQ-3"), "overruled a ruling on VAQ-3: heavy"},
		{ev(core.EvDecisionWithdrawn, map[string]any{"kind": "question", "ticket_ids": []any{"a"}}, "VAQ-3"), "withdrew a question about VAQ-3"},
		{ev(core.EvDecisionWithdrawn, map[string]any{"kind": "approval", "subject": "plan", "ticket_ids": []any{"a"}}, "VAQ-3"), "withdrew the request to approve the plan for VAQ-3"},
	} {
		if got := EventSentence(c.e); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if EventIcon(core.EvDecisionAnswered) != "decision" {
		t.Error("icon")
	}
}

func TestWorkflowWording(t *testing.T) {
	ch := func(c map[string]any) EventView {
		return EventView{Kind: core.EvProjectUpdated, ProjectKey: "VAQ", Payload: map[string]any{"changes": c}}
	}
	for want, e := range map[string]EventView{
		"turned the agent workflow on in VAQ":                          ch(map[string]any{"workflow": map[string]any{"from": false, "to": true}}),
		"turned the agent workflow off in VAQ":                         ch(map[string]any{"workflow": map[string]any{"from": true, "to": false}}),
		"turned the agent workflow on in VAQ and changed its tier map": ch(map[string]any{"workflow": map[string]any{"from": false, "to": true}, "tier_map": map[string]any{}}),
		"changed the tier map of VAQ":                                  ch(map[string]any{"tier_map": map[string]any{}}),
		"updated project VAQ":                                          ch(map[string]any{"name": map[string]any{"from": "A", "to": "B"}}),
	} {
		if got := EventSentence(e); got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}

// VAQ-134: on a ticket's history a tag event reads as what happened to the
// ticket's tag; on the Activity page it keeps the project and the count.
func TestTagWordingOnTicketHistory(t *testing.T) {
	ids := []any{"a"}
	for _, tc := range []struct {
		kind    string
		payload map[string]any
		history string
		feed    string
	}{
		{core.EvTagRenamed, map[string]any{"from": "bug", "to": "defect", "tickets": 1.0, "ticket_ids": ids}, "renamed tag bug → defect", "renamed tag bug to defect in VAQ (1 ticket)"},
		{core.EvTagMerged, map[string]any{"from": "bugs", "to": "bug", "tickets": 1.0, "ticket_ids": ids}, "merged tag bugs into bug", "merged tag bugs into bug in VAQ (1 ticket)"},
		{core.EvTagDeleted, map[string]any{"name": "wontfix", "tickets": 1.0, "ticket_ids": ids}, "deleted tag wontfix", "deleted tag wontfix from VAQ (1 ticket)"},
	} {
		e := EventView{Kind: tc.kind, ProjectKey: "VAQ", Payload: tc.payload}
		if got := EventSentence(e); got != tc.feed {
			t.Errorf("feed %q, want %q", got, tc.feed)
		}
		e.Reader = "VAQ-1"
		if got := EventSentence(e); got != tc.history {
			t.Errorf("history %q, want %q", got, tc.history)
		}
	}
}

func TestMovedWording(t *testing.T) {
	p := map[string]any{"from": "VAQ-4", "to": "OTH-2", "from_project": "VAQ", "to_project": "OTH",
		"sub_tickets": []any{map[string]any{}, map[string]any{}}, "open_decisions": float64(1)}
	if got := EventSentence(EventView{Kind: core.EvTicketMoved, TicketKey: "OTH-2", Payload: p}); got != "moved VAQ-4 to OTH as OTH-2 with 2 sub-tickets; 1 open decision stays in VAQ" {
		t.Fatal(got)
	}
	p = map[string]any{"from": "VAQ-4", "to": "OTH-2", "from_project": "VAQ", "to_project": "OTH", "sub_tickets": []any{}}
	if got := EventSentence(EventView{Kind: core.EvTicketMoved, Payload: p}); got != "moved VAQ-4 to OTH as OTH-2" {
		t.Fatal(got)
	}
}

// VAQ-119: a project link reads as what it says about the two projects.
func TestProjectLinkWording(t *testing.T) {
	for _, tc := range []struct{ kind, link, want string }{
		{core.EvProjectLinked, "depends on", "noted that VAQ depends on OTH"},
		{core.EvProjectLinked, "relates", "noted that VAQ relates to OTH"},
		{core.EvProjectUnlinked, "depends on", "removed the note that VAQ depends on OTH"},
		{core.EvProjectUnlinked, "relates", "removed the note that VAQ relates to OTH"},
	} {
		e := EventView{Kind: tc.kind, ProjectKey: "VAQ", Payload: map[string]any{"kind": tc.link, "to": "OTH"}}
		if got := EventSentence(e); got != tc.want {
			t.Errorf("%s %s: %q", tc.kind, tc.link, got)
		}
	}
	if EventIcon(core.EvProjectLinked) != "link" || EventIcon(core.EvProjectUnlinked) != "delete" {
		t.Fatal("icon")
	}
}
