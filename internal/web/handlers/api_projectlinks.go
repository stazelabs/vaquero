package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

// APIProjectLinkCreate is POST /api/v1/projects/{key}/links: {"kind",
// "project"}, "depends on" (or "depends-on") or "relates", and the other
// project's key. Answers 201 with the link as this project sees it.
func APIProjectLinkCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in struct{ Kind, Project string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		l, err := d.Core.AddProjectLink(r.Context(), apiActor(r).ID, p.ID, in.Kind, in.Project)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		to, err := d.R.GetProject(r.Context(), l.ToProject)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, api.ProjectLink{ID: l.ID, Kind: l.Kind, Direction: "out", Key: to.Key, Name: to.Name})
	}
}

// APIProjectLinkDelete is DELETE /api/v1/projects/{key}/links/{id}: a
// link of the project's, either way round. Answers 204.
func APIProjectLinkDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.RemoveProjectLink(r.Context(), apiActor(r).ID, p.ID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				api.WriteFailure(w, api.NotFound("No such link in "+p.Key+"."))
				return
			}
			apiCoreFail(w, r, d, p, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
