package handlers

import (
	"net/http"
	"strings"

	"github.com/stazelabs/vaquero/internal/web/templates"
)

// Nav gives each request its place, from the path (ruling T-R2), and the
// sidebar's reader, run once on first use (templates.WithNav): a fragment
// or an API call never reads it. A failed read is logged here and the
// page draws an empty project list.
func Nav(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			r = r.WithContext(templates.WithNav(ctx, placeOf(r.URL.Path), func() (templates.Nav, error) {
				rows, err := d.R.ListNavProjects(ctx)
				if err != nil {
					if ctx.Err() == nil {
						d.Logger.WarnContext(ctx, "read sidebar", "err", err)
					}
					return templates.Nav{}, err
				}
				n := templates.Nav{Projects: make([]templates.NavProject, 0, len(rows))}
				for _, p := range rows {
					n.Projects = append(n.Projects, templates.NavProject{Key: p.Key, Name: p.Name,
						Icon: templates.ProjectIconView{ID: p.ID, Key: p.Key, Name: p.Name, Kind: p.IconKind, Glyph: p.IconGlyph}})
				}
				// A failed count keeps the projects and shows no count.
				if c, err := d.R.CountInbox(ctx); err != nil {
					if ctx.Err() == nil {
						d.Logger.WarnContext(ctx, "read inbox count", "err", err)
					}
				} else {
					n.InboxCount = int(c)
				}
				return n, nil
			}))
			next.ServeHTTP(w, r)
		})
	}
}

// placeOf is the place path names: /p/{key} is the project's Focus,
// /p/{key}/board, /list and /settings those views, any other page of a
// project the project alone; a ticket page names its project once read.
func placeOf(path string) templates.Place {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch parts[0] {
	case "p":
		if len(parts) < 2 || parts[1] == "" {
			return templates.Place{}
		}
		pl := templates.Place{Project: strings.ToUpper(parts[1])}
		switch {
		case len(parts) == 2:
			pl.View = "focus"
		case len(parts) == 3 && (parts[2] == "board" || parts[2] == "list" || parts[2] == "settings"):
			pl.View = parts[2]
		}
		return pl
	case "t":
		return templates.Place{View: "ticket"}
	case "inbox", "my", "activity", "search", "help", "profile":
		return templates.Place{View: parts[0]}
	}
	return templates.Place{}
}
