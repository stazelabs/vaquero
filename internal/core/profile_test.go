package core_test

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	ops = append(ops,
		coreOp{"UpdateProfile",
			func(c *core.Core, f fixture) error {
				_, err := c.UpdateProfile(bg, f.Owner.ID, new("Kim"))
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.UpdateProfile(bg, f.Owner.ID, new(strings.Repeat("k", 81)))
				return err
			}},
		coreOp{"SetAvatar",
			func(c *core.Core, f fixture) error {
				_, err := c.SetAvatar(bg, f.Owner.ID, "image/png", pngIcon)
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.SetAvatar(bg, f.Owner.ID, "image/svg+xml", []byte("<svg/>"))
				return err
			}},
		coreOp{"RemoveAvatar",
			func(c *core.Core, f fixture) error {
				// A stored avatar, arranged without an event.
				if _, err := c.DB.Exec(`INSERT INTO actor_avatars (actor_id, content_type, data, etag, updated_at) VALUES (?, 'image/png', 'eA==', 'abc', '2026-11-18T12:00:00.000Z')`, f.Owner.ID); err != nil {
					return err
				}
				_, err := c.RemoveAvatar(bg, f.Owner.ID)
				return err
			},
			func(c *core.Core, f fixture) error {
				_, err := c.RemoveAvatar(bg, "no-such-actor")
				return err
			}},
	)
	lateFail["UpdateProfile"] = func(c *core.Core, f fixture, snap func()) error {
		snap()
		_, err := c.UpdateProfile(bg, f.Owner.ID, new("Kim"))
		return err
	}
	lateFail["SetAvatar"] = func(c *core.Core, f fixture, snap func()) error {
		snap()
		_, err := c.SetAvatar(bg, f.Owner.ID, "", pngIcon)
		return err
	}
	lateFail["RemoveAvatar"] = func(c *core.Core, f fixture, snap func()) error {
		if _, err := c.SetAvatar(bg, f.Owner.ID, "", pngIcon); err != nil {
			return err
		}
		snap()
		_, err := c.RemoveAvatar(bg, f.Owner.ID)
		return err
	}
}

// newestChanges is the newest event's kind and payload changes.
func newestChanges(t *testing.T, c *core.Core) (string, map[string]any) {
	t.Helper()
	var kind, payload string
	if err := c.DB.QueryRow(`SELECT kind, payload FROM events ORDER BY seq DESC LIMIT 1`).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	var body struct{ Changes map[string]any }
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatal(err)
	}
	return kind, body.Changes
}

// VAQ-149: a display name is NFC, at most 80 characters, without control
// characters; "" restores "Owner"; one event per change, none for a no-op.
func TestUpdateProfile(t *testing.T) {
	c, d, f := newFixture(t)
	if got := core.ActorName(f.Owner); got != "Owner" {
		t.Fatalf("default name %q", got)
	}
	before := eventCount(t, d)
	a, err := c.UpdateProfile(bg, f.Owner.ID, new("  José  "))
	if err != nil {
		t.Fatal(err)
	}
	if a.DisplayName != "José" || core.ActorName(a) != "José" || a.Name != "Owner" {
		t.Fatalf("%+v", a)
	}
	if n := eventCount(t, d) - before; n != 1 {
		t.Fatalf("%d events", n)
	}
	kind, ch := newestChanges(t, c)
	if got, _ := json.Marshal(ch); kind != core.EvActorUpdated || string(got) != `{"display_name":{"from":"","to":"José"}}` {
		t.Fatalf("%s %s", kind, got)
	}
	// The same name again, or no name at all, writes nothing.
	before = eventCount(t, d)
	if _, err := c.UpdateProfile(bg, f.Owner.ID, new("José")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateProfile(bg, f.Owner.ID, nil); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("a no-op wrote an event")
	}
	// Eighty characters fit; refusals change nothing.
	if _, err := c.UpdateProfile(bg, f.Owner.ID, new(strings.Repeat("é", 80))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want string }{
		{strings.Repeat("k", 81), "Keep the name to 80 characters."},
		{"Kim\x00", "Remove the hidden control characters."},
		{"Kim\nLee", "Remove the hidden control characters."},
	} {
		snap := snapshot(t, d)
		_, err := c.UpdateProfile(bg, f.Owner.ID, &tc.in)
		inv, ok := errors.AsType[*core.Invalid](err)
		if !ok || inv.Fields["display_name"] != tc.want {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if !maps.Equal(snapshot(t, d), snap) {
			t.Fatalf("%q: a refusal changed rows", tc.in)
		}
	}
	// "" restores "Owner".
	a, err = c.UpdateProfile(bg, f.Owner.ID, new("  "))
	if err != nil || a.DisplayName != "" || core.ActorName(a) != "Owner" {
		t.Fatalf("%+v %v", a, err)
	}
}

// Review Focus 5: only the owner, acting directly, edits the owner's
// profile; an agent (through Vaquero-Agent, --agent or MCP) is refused,
// and so is the owner's own token through MCP.
func TestProfileOwnerOnly(t *testing.T) {
	c, d, f := newFixture(t)
	agent, err := c.EnsureAgent(bg, f.Owner.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	const want = "Only the owner edits the owner's profile."
	mcp := core.WithOrigin(bg, core.Origin{Surface: core.SurfaceMCP})
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"agent rename", func() error { _, err := c.UpdateProfile(bg, agent.ID, new("Kim")); return err }},
		{"agent avatar", func() error { _, err := c.SetAvatar(bg, agent.ID, "", pngIcon); return err }},
		{"agent remove", func() error { _, err := c.RemoveAvatar(bg, agent.ID); return err }},
		{"mcp rename", func() error { _, err := c.UpdateProfile(mcp, f.Owner.ID, new("Kim")); return err }},
		{"mcp avatar", func() error { _, err := c.SetAvatar(mcp, f.Owner.ID, "", pngIcon); return err }},
		{"missing actor", func() error { _, err := c.UpdateProfile(bg, "no-such-actor", new("Kim")); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := snapshot(t, d)
			inv, ok := errors.AsType[*core.Invalid](tc.run())
			if !ok || inv.Fields["profile"] != want {
				t.Fatalf("%v", inv)
			}
			if !maps.Equal(snapshot(t, d), snap) {
				t.Fatal("a refusal changed rows")
			}
		})
	}
}

// The avatar takes the project icon's checks: SVG, GIF and a file over
// 256 KB are refused by their bytes.
func TestAvatarRefusals(t *testing.T) {
	c, d, f := newFixture(t)
	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	for _, tc := range []struct {
		name string
		typ  string
		data []byte
		want string
	}{
		{"svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "Use a PNG, JPEG or WebP image."},
		{"gif", "image/gif", gif, "Use a PNG, JPEG or WebP image."},
		{"text named png", "image/png", []byte("not a picture"), "Use a PNG, JPEG or WebP image."},
		{"300 KB", "image/png", append(append([]byte{}, pngIcon...), make([]byte, 300<<10)...), "Use an image of 256 KB or less."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := snapshot(t, d)
			_, err := c.SetAvatar(bg, f.Owner.ID, tc.typ, tc.data)
			inv, ok := errors.AsType[*core.Invalid](err)
			if !ok || inv.Fields["avatar"] != tc.want {
				t.Fatalf("%v", err)
			}
			if !maps.Equal(snapshot(t, d), snap) {
				t.Fatal("a refusal changed rows")
			}
		})
	}
}

// One event per avatar change, naming the etags; the same image again and
// removing a missing one write nothing.
func TestSetAndRemoveAvatar(t *testing.T) {
	c, d, f := newFixture(t)
	before := eventCount(t, d)
	if _, err := c.SetAvatar(bg, f.Owner.ID, "image/gif", pngIcon); err != nil {
		t.Fatal(err)
	}
	av, err := d.RQ.GetActorAvatar(bg, f.Owner.ID)
	if err != nil || av.ContentType != "image/png" || av.Etag == "" {
		t.Fatalf("%+v %v", av, err)
	}
	kind, ch := newestChanges(t, c)
	if got, _ := json.Marshal(ch); kind != core.EvActorUpdated || string(got) != `{"avatar":{"from":null,"to":"`+av.Etag+`"}}` {
		t.Fatalf("%s %s", kind, got)
	}
	if _, err := c.SetAvatar(bg, f.Owner.ID, "", pngIcon); err != nil {
		t.Fatal(err)
	}
	if n := eventCount(t, d) - before; n != 1 {
		t.Fatalf("%d events", n)
	}
	if _, err := c.SetAvatar(bg, f.Owner.ID, "", webpIcon); err != nil {
		t.Fatal(err)
	}
	av2, _ := d.RQ.GetActorAvatar(bg, f.Owner.ID)
	if _, ch := newestChanges(t, c); ch["avatar"].(map[string]any)["from"] != av.Etag || av2.ContentType != "image/webp" {
		t.Fatalf("%v %+v", ch, av2)
	}
	if _, err := c.RemoveAvatar(bg, f.Owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, ch := newestChanges(t, c); ch["avatar"].(map[string]any)["to"] != nil {
		t.Fatalf("%v", ch)
	}
	before = eventCount(t, d)
	if _, err := c.RemoveAvatar(bg, f.Owner.ID); err != nil {
		t.Fatal(err)
	}
	if eventCount(t, d) != before {
		t.Fatal("removing nothing wrote an event")
	}
}

// An agent's tile colour comes from its id alone.
func TestAgentMonogramStable(t *testing.T) {
	c, _, f := newFixture(t)
	a, err := c.EnsureAgent(bg, f.Owner.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.EnsureAgent(bg, f.Owner.ID, "claude")
	if err != nil || b.ID != a.ID {
		t.Fatal(err)
	}
	if core.TileColor(a.ID) != core.TileColor(b.ID) {
		t.Fatal("colour changed")
	}
	if got := core.ActorMonogram(a); got != "CL" {
		t.Fatalf("monogram %q", got)
	}
	if got := core.ActorMonogram(f.Owner); got != "OW" {
		t.Fatalf("monogram %q", got)
	}
}
