package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// MyWork is /my (ruling T-R3): the owner's open assigned tickets across
// the projects not archived, as rows grouped by project.
func MyWork(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListMyWork(r.Context(), sql.NullString{String: d.Actor.ID, Valid: true})
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		var groups []templates.MyWorkGroup
		for _, t := range rows {
			if len(groups) == 0 || groups[len(groups)-1].Project.Key != t.ProjectKey {
				groups = append(groups, templates.MyWorkGroup{Project: templates.ProjectIconView{
					ID: t.ProjectID, Key: t.ProjectKey, Name: t.ProjectName, Kind: t.IconKind, Glyph: t.IconGlyph}})
			}
			g := &groups[len(groups)-1]
			g.Rows = append(g.Rows, templates.MyWorkRow{Status: t.StatusName, Card: board.Card{
				ID: t.ID, Key: t.ProjectKey + "-" + strconv.FormatInt(t.Number, 10), Title: t.Title,
				Priority: t.Priority, Version: t.Version,
				ClaimantID: t.ClaimantID, ClaimedBy: t.ClaimantName, ClaimantIsAgent: t.ClaimantKind == "agent",
				ClaimExpiresAt: t.ClaimExpiresAt,
			}})
		}
		render(w, r, d, http.StatusOK, templates.MyWorkPage(groups))
	}
}
