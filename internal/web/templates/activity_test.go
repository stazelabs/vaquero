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
	if got := EventSentence(EventView{Kind: core.EvAttachmentAdded, Payload: p}); got != "attached shot.png" {
		t.Fatal(got)
	}
	if got := EventSentence(EventView{Kind: core.EvAttachmentDeleted, Payload: p}); got != "removed the attachment shot.png" {
		t.Fatal(got)
	}
	if EventIcon(core.EvAttachmentAdded) != "attachment" || EventIcon(core.EvAttachmentDeleted) != "attachment" {
		t.Fatal("icon")
	}
}
