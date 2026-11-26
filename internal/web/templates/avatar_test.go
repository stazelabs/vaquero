package templates

import (
	"context"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func TestAvatarKinds(t *testing.T) {
	mono := html(t, Avatar(ActorView{ID: "01ABC", Name: "Kim Lee"}, "sm"))
	if !strings.Contains(mono, `data-avatar-monogram="KL"`) || !strings.Contains(mono, tileClasses[core.TileColor("01ABC")]) || !strings.Contains(mono, "rounded-full") {
		t.Fatalf("monogram: %s", mono)
	}
	img := html(t, Avatar(ActorView{ID: "01ABC", Name: "Kim Lee", Image: true}, "md"))
	if !strings.Contains(img, `src="/avatars/01ABC"`) || !strings.Contains(img, "object-cover") || !strings.Contains(img, `alt=""`) {
		t.Fatalf("image: %s", img)
	}
}

// An agent's tile colour comes from its id: the same on every render, and
// two agents with different ids can differ.
func TestAgentMonogramStable(t *testing.T) {
	ctx := WithFaces(context.Background(), "owner", func() (map[string]Face, error) {
		return map[string]Face{"01AGENT": {Kind: "agent", Name: "claude"}}, nil
	})
	a := ActorOf(ctx, "01AGENT", "someone")
	if a.Name != "claude" || a.Image {
		t.Fatalf("%+v", a)
	}
	first := html(t, Avatar(a, "sm"))
	for range 3 {
		if got := html(t, Avatar(ActorOf(ctx, "01AGENT", "claude"), "sm")); got != first {
			t.Fatalf("render changed: %s vs %s", got, first)
		}
	}
	if !strings.Contains(first, tileClasses[core.TileColor("01AGENT")]) || !strings.Contains(first, `data-avatar-monogram="CL"`) {
		t.Fatal(first)
	}
}

// The faces load once per request, and a request without them falls back
// to the name it was given.
func TestFacesLoadOnce(t *testing.T) {
	n := 0
	ctx := WithFaces(context.Background(), "01OWN", func() (map[string]Face, error) {
		n++
		return map[string]Face{"01OWN": {Kind: "human", Name: "Owner", DisplayName: "Kim", Etag: "e1"}}, nil
	})
	if o := OwnerView(ctx); o.Name != "Kim" || !o.Image || o.ID != "01OWN" {
		t.Fatalf("%+v", o)
	}
	ActorOf(ctx, "01OWN", "Owner")
	if n != 1 {
		t.Fatalf("loaded %d times", n)
	}
	if a := ActorOf(context.Background(), "01X", "Owner"); a.Name != "Owner" || a.Image {
		t.Fatalf("%+v", a)
	}
}

func TestActorUpdatedWording(t *testing.T) {
	for _, tc := range []struct{ payload, want string }{
		{`{"changes":{"display_name":{"from":"","to":"Kim"}}}`, "changed their display name to Kim"},
		{`{"changes":{"display_name":{"from":"Kim","to":""}}}`, "cleared their display name"},
		{`{"changes":{"avatar":{"from":null,"to":"e1"}}}`, "added an avatar"},
		{`{"changes":{"avatar":{"from":"e1","to":"e2"}}}`, "changed their avatar"},
		{`{"changes":{"avatar":{"from":"e1","to":null}}}`, "removed their avatar"},
	} {
		if got := EventSentence(EventView{Kind: core.EvActorUpdated, Payload: ParsePayload(tc.payload)}); got != tc.want {
			t.Errorf("%s: %q", tc.payload, got)
		}
	}
}
