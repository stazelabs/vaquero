package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/web/templates"
)

// Faces gives each page request the actors' faces, read once on first use
// (templates.WithFaces): names and avatars are looked up by actor id when a
// page is drawn, so a rename or a new avatar shows everywhere at once,
// past history included (VAQ-149).
func Faces(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			r = r.WithContext(templates.WithFaces(ctx, d.Actor.ID, func() (map[string]templates.Face, error) {
				rows, err := d.R.ListActorFaces(ctx)
				if err != nil {
					d.Logger.WarnContext(ctx, "read faces", "err", err)
					return nil, err
				}
				m := make(map[string]templates.Face, len(rows))
				for _, a := range rows {
					m[a.ID] = templates.Face{Kind: a.Kind, Name: a.Name, DisplayName: a.DisplayName, Etag: a.AvatarEtag}
				}
				return m, nil
			}))
			next.ServeHTTP(w, r)
		})
	}
}
