package templates

import (
	"context"
	"sync"
)

// NavProject is one project as the sidebar lists it.
type NavProject struct {
	Key, Name string
	Icon      ProjectIconView
}

// Nav is what the sidebar reads: the projects not archived, by name, and
// the inbox's count (shown when above zero).
type Nav struct {
	Projects   []NavProject
	InboxCount int
}

// Place is where a page is, for the sidebar's current item (ruling T-R2):
// the project's key ("" outside a project) and the view, one of focus,
// board, list, settings, ticket, inbox, my, activity, search, help,
// profile, or "".
type Place struct {
	Project, View string
}

// projectViews are the views a project expands to in the sidebar, in
// order, with their glyphs and labels.
var projectViews = [][3]string{
	{"focus", "focus", "Focus"},
	{"board", "board", "Board"},
	{"list", "list", "List"},
}

type navKey struct{}

type navState struct {
	place *Place
	load  func() (Nav, error)
}

// WithNav gives a request its place and the sidebar's reader. load runs at
// most once, on first use, so a request that draws no page (a fragment,
// an API call) never reads it. The place is held by pointer, so a handler
// can name its project later (SetPlaceProject).
func WithNav(ctx context.Context, place Place, load func() (Nav, error)) context.Context {
	return context.WithValue(ctx, navKey{}, navState{place: &place, load: sync.OnceValues(load)})
}

// NavOf is the sidebar's content. A failed read (the loader logs it) is an
// empty sidebar list, never an error page.
func NavOf(ctx context.Context) Nav {
	s, ok := ctx.Value(navKey{}).(navState)
	if !ok {
		return Nav{}
	}
	n, err := s.load()
	if err != nil {
		return Nav{}
	}
	return n
}

// PlaceOf is the request's place.
func PlaceOf(ctx context.Context) Place {
	s, ok := ctx.Value(navKey{}).(navState)
	if !ok {
		return Place{}
	}
	return *s.place
}

// SetPlaceProject names the place's project, for a page whose path holds
// no project key (a ticket page).
func SetPlaceProject(ctx context.Context, key string) {
	if s, ok := ctx.Value(navKey{}).(navState); ok {
		s.place.Project = key
	}
}

// setPlaceProject is SetPlaceProject as a templ statement needs it.
func setPlaceProject(ctx context.Context, key string) string {
	SetPlaceProject(ctx, key)
	return ""
}

// isProjectView reports whether view is one the sidebar lists under a
// project.
func isProjectView(view string) bool {
	for _, v := range projectViews {
		if v[0] == view {
			return true
		}
	}
	return false
}

// markedView is the project view the sidebar marks for place: the view
// itself, else Focus (app.js moves the mark to the project's last view).
func markedView(p Place) string {
	if isProjectView(p.View) {
		return p.View
	}
	return "focus"
}
