package templates

import (
	"context"
	"sync"
)

// Face is one actor as a page draws it beside a name (VAQ-149): its kind,
// its name and display name, and its avatar's etag ("" for none).
type Face struct {
	Kind, Name, DisplayName, Etag string
}

// Shown is the name a page shows: the display name when set.
func (f Face) Shown() string {
	if f.DisplayName != "" {
		return f.DisplayName
	}
	return f.Name
}

// ActorView is what Avatar draws: the actor's id (its tile colour and its
// image's address), its shown name (the monogram), and whether it has an
// uploaded image.
type ActorView struct {
	ID, Name string
	Image    bool
}

type facesKey struct{}

type faces struct {
	owner string
	load  func() (map[string]Face, error)
}

// WithFaces gives a request the actors' faces, read on first use (at most
// once, and not at all by a request that draws no avatar), and the owner's
// id for the header's profile link. Names are looked up by actor id when a
// page is drawn, so a rename shows everywhere at once.
func WithFaces(ctx context.Context, ownerID string, load func() (map[string]Face, error)) context.Context {
	return context.WithValue(ctx, facesKey{}, faces{owner: ownerID, load: sync.OnceValues(load)})
}

// face is actor id's face, if the request has faces and it is among them.
func face(ctx context.Context, id string) (Face, bool) {
	fs, ok := ctx.Value(facesKey{}).(faces)
	if !ok || id == "" {
		return Face{}, false
	}
	m, err := fs.load()
	if err != nil {
		return Face{}, false
	}
	f, ok := m[id]
	return f, ok
}

// ActorOf is actor id as Avatar draws it: from the request's faces, else
// with name as given and no image.
func ActorOf(ctx context.Context, id, name string) ActorView {
	if f, ok := face(ctx, id); ok {
		return ActorView{ID: id, Name: f.Shown(), Image: f.Etag != ""}
	}
	return ActorView{ID: id, Name: name}
}

// OwnerView is the owner as the header's profile link draws them.
func OwnerView(ctx context.Context) ActorView {
	fs, _ := ctx.Value(facesKey{}).(faces)
	return ActorOf(ctx, fs.owner, "Owner")
}

// AvatarURL is the address of actor id's uploaded avatar.
func AvatarURL(id string) string { return "/avatars/" + id }
