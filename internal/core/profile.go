package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// The owner's profile (VAQ-149, ruling P-R3): a display name and an
// avatar. actors.name stays the actor's identity ("Owner", which
// EnsureOwner finds it by); every page, answer and history line shows the
// display name when one is set (ActorName), looked up by actor id when it
// is drawn, so a rename shows in past history too. The avatar is an image
// stored like a project icon (base64 in actor_avatars, never the blob
// store), checked as one; without it the actor shows a monogram tile.
// Agents have no profile: they show their monogram on a tile coloured by
// their id.

// MaxDisplayName is the longest display name, in characters.
const MaxDisplayName = 80

// profileRule is the refusal of anyone but the owner acting directly.
const profileRule = "Only the owner edits the owner's profile."

// ActorName is the name a page shows for a: its display name when set,
// else its name.
func ActorName(a gen.Actor) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Name
}

// ActorMonogram is the two letters of an actor's tile: its shown name's
// initials, else the name's first two characters.
func ActorMonogram(a gen.Actor) string {
	n := ActorName(a)
	return Monogram(n, n)
}

// profileOwner reads actor by inside q's transaction and refuses anyone
// but the owner acting directly: an agent (an actor of its own, through
// Vaquero-Agent, --agent or MCP), a missing actor, or a call through MCP.
func profileOwner(ctx context.Context, q *gen.Queries, by string) (gen.Actor, error) {
	if OriginFrom(ctx).Surface == SurfaceMCP {
		return gen.Actor{}, invalid("profile", profileRule)
	}
	a, err := q.GetActor(ctx, by)
	if errors.Is(err, sql.ErrNoRows) {
		return a, invalid("profile", profileRule)
	}
	if err != nil {
		return a, fmt.Errorf("get actor: %w", err)
	}
	if !IsOwner(a) {
		return a, invalid("profile", profileRule)
	}
	return a, nil
}

// UpdateProfile sets the owner's display name: trimmed, in NFC, at most
// MaxDisplayName characters, without control characters; "" goes back to
// the name ("Owner"). A nil name changes nothing. It writes one
// actor.updated event with changes.display_name, or nothing when the name
// is unchanged.
func (c *Core) UpdateProfile(ctx context.Context, by string, displayName *string) (gen.Actor, error) {
	var a gen.Actor
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if a, err = profileOwner(ctx, q, by); err != nil {
			return err
		}
		if displayName == nil {
			return nil
		}
		f := fields{}
		name := checked(f, "display_name", norm.NFC.String(strings.TrimSpace(*displayName)), MaxDisplayName, "", "Keep the name to 80 characters.")
		if hasNewline(name) {
			f.add("display_name", controlMsg)
		}
		if err := f.err(); err != nil {
			return err
		}
		if name != "" {
			if err := nameTaken(ctx, q, a.ID, name); err != nil {
				return err
			}
		}
		if name == a.DisplayName {
			return nil
		}
		if err := q.SetActorDisplayName(ctx, gen.SetActorDisplayNameParams{DisplayName: name, ID: a.ID}); err != nil {
			return fmt.Errorf("set display name: %w", err)
		}
		from := a.DisplayName
		a.DisplayName = name
		return c.emit(ctx, q, by, EvActorUpdated, "", "", map[string]any{"changes": map[string]Change{"display_name": {From: from, To: name}}})
	})
	return a, err
}

// hasNewline reports a tab or line break, U+2028 and U+2029 included,
// which a name never holds (checked lets them through for multi-line
// text).
func hasNewline(s string) bool { return strings.ContainsAny(s, "\t\n\r\u2028\u2029") }

// nameTaken refuses a display name that another actor is called, or shows,
// matched as tags are (SameTag: NFC and case folding), so a name in an
// answer or the assignee field names one actor.
func nameTaken(ctx context.Context, q *gen.Queries, self, name string) error {
	as, err := q.ListActors(ctx)
	if err != nil {
		return fmt.Errorf("list actors: %w", err)
	}
	for _, o := range as {
		if o.ID == self || !SameTag(name, o.Name) && (o.DisplayName == "" || !SameTag(name, o.DisplayName)) {
			continue
		}
		who := "Someone"
		if o.Kind == "agent" {
			who = "An agent"
		}
		return invalid("display_name", who+" is called "+ActorName(o)+"; choose another name.")
	}
	return nil
}

// avatarEtag is the owner's stored avatar's etag, "" when none, read
// inside q's transaction.
func avatarEtag(ctx context.Context, q *gen.Queries, actorID string) (string, error) {
	etag, err := q.GetActorAvatarEtag(ctx, actorID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get avatar: %w", err)
	}
	return etag, nil
}

// SetAvatar makes data the owner's avatar: a PNG, JPEG or WebP of at most
// MaxIcon bytes, its type sniffed from the bytes (contentType, what the
// client claimed, is never trusted), checked as a project icon is. It
// writes one actor.updated event with changes.avatar (etag to etag, null
// for none), or nothing when the same image is stored already.
func (c *Core) SetAvatar(ctx context.Context, by, contentType string, data []byte) (gen.Actor, error) {
	var a gen.Actor
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if a, err = profileOwner(ctx, q, by); err != nil {
			return err
		}
		f := fields{}
		stored := checkIconImage(f, "avatar", &IconImage{Type: contentType, Data: data})
		if err := f.err(); err != nil {
			return err
		}
		old, err := avatarEtag(ctx, q, a.ID)
		if err != nil {
			return err
		}
		if old == stored.etag {
			return nil
		}
		if err := q.SaveActorAvatar(ctx, gen.SaveActorAvatarParams{ActorID: a.ID, ContentType: stored.typ, Data: stored.data, Etag: stored.etag, UpdatedAt: timefmt.Format(c.Now())}); err != nil {
			return fmt.Errorf("save avatar: %w", err)
		}
		var from any
		if old != "" {
			from = old
		}
		return c.emit(ctx, q, by, EvActorUpdated, "", "", map[string]any{"changes": map[string]Change{"avatar": {From: from, To: stored.etag}}})
	})
	return a, err
}

// RemoveAvatar deletes the owner's avatar, so the monogram shows again. It
// writes one actor.updated event with changes.avatar (the etag to null), or
// nothing when no avatar is stored.
func (c *Core) RemoveAvatar(ctx context.Context, by string) (gen.Actor, error) {
	var a gen.Actor
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if a, err = profileOwner(ctx, q, by); err != nil {
			return err
		}
		old, err := avatarEtag(ctx, q, a.ID)
		if err != nil || old == "" {
			return err
		}
		if err := q.DeleteActorAvatar(ctx, a.ID); err != nil {
			return fmt.Errorf("delete avatar: %w", err)
		}
		return c.emit(ctx, q, by, EvActorUpdated, "", "", map[string]any{"changes": map[string]Change{"avatar": {From: old, To: nil}}})
	})
	return a, err
}
