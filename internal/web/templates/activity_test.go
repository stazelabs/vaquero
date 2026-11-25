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
