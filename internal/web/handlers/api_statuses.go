package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// statusDTOs is a project's statuses in board order, with ticket counts.
func statusDTOs(ctx context.Context, d Deps, p gen.Project) ([]api.Status, error) {
	ss, err := d.R.ListStatuses(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	counts, err := d.R.CountTicketsByStatus(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	n := map[string]int64{}
	for _, c := range counts {
		n[c.StatusID] = c.N
	}
	out := make([]api.Status, len(ss))
	for i, s := range ss {
		out[i] = api.Status{Name: s.Name, Category: s.Category, Position: s.Position, TicketCount: n[s.ID]}
	}
	return out, nil
}

// writeStatus answers the status with the id it has now (after a rename or
// a move), as it is in the project's list.
func writeStatus(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, id string, code int) {
	s, err := d.R.GetStatus(r.Context(), id)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	all, err := statusDTOs(r.Context(), d, p)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	i := slices.IndexFunc(all, func(x api.Status) bool { return x.Name == s.Name })
	if i < 0 {
		apiFail(w, r, d, api.Internal(), errors.New("status missing from its project's list"))
		return
	}
	api.WriteJSON(w, code, all[i])
}

// apiStatus loads the {key} project and its {name} status; an unknown name
// is a 422 on the field "status".
func apiStatus(w http.ResponseWriter, r *http.Request, d Deps) (gen.Project, gen.Status, bool) {
	p, ok := apiProject(w, r, d)
	if !ok {
		return p, gen.Status{}, false
	}
	n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
	s := n.statusIn("status", pathName(r))
	if n.fail(w, r) {
		return p, s, false
	}
	return p, s, true
}

// pathName is the {name} URL parameter, unescaped.
func pathName(r *http.Request) string {
	raw := chi.URLParam(r, "name")
	if v, err := url.PathUnescape(raw); err == nil {
		return v
	}
	return raw
}

// statusPlace resolves the before and after names into a core.StatusPlace.
func statusPlace(n *names, before, after string) core.StatusPlace {
	var at core.StatusPlace
	if strings.TrimSpace(before) != "" {
		at.Before = n.statusIn("before", before).ID
	}
	if strings.TrimSpace(after) != "" {
		at.After = n.statusIn("after", after).ID
	}
	return at
}

// apiCategory reads a category as the API takes it: its name, or the same
// in any case with spaces ("In progress").
func apiCategory(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
}

// APIStatuses is GET /api/v1/projects/{key}/statuses.
func APIStatuses(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		out, err := statusDTOs(r.Context(), d, p)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIStatusCreate is POST /api/v1/projects/{key}/statuses {name, category,
// before?, after?}: without a neighbour, the end of its category.
func APIStatusCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in struct{ Name, Category, Before, After string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		category := apiCategory(in.Category)
		if !slices.Contains(core.Categories, category) {
			n.fields["category"] = "Use one of backlog, todo, in_progress, in_review, done or cancelled."
		}
		at := statusPlace(n, in.Before, in.After)
		if n.fail(w, r) {
			return
		}
		s, err := d.Core.CreateStatusAt(r.Context(), apiActor(r).ID, p.ID, in.Name, category, at)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeStatus(w, r, d, p, s.ID, http.StatusCreated)
	}
}

// APIStatusUpdate is PATCH /api/v1/projects/{key}/statuses/{name} {name}.
func APIStatusUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, s, ok := apiStatus(w, r, d)
		if !ok {
			return
		}
		var in struct{ Name *string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		if in.Name != nil {
			if err := d.Core.RenameStatus(r.Context(), apiActor(r).ID, s.ID, *in.Name); err != nil {
				apiCoreFail(w, r, d, p, err)
				return
			}
		}
		writeStatus(w, r, d, p, s.ID, http.StatusOK)
	}
}

// APIStatusMove is POST /api/v1/projects/{key}/statuses/{name}/move
// {before} or {after}.
func APIStatusMove(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, s, ok := apiStatus(w, r, d)
		if !ok {
			return
		}
		var in struct{ Before, After string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		at := statusPlace(n, in.Before, in.After)
		if n.fail(w, r) {
			return
		}
		if err := d.Core.MoveStatusTo(r.Context(), apiActor(r).ID, s.ID, at); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeStatus(w, r, d, p, s.ID, http.StatusOK)
	}
}

// APIStatusDelete is DELETE /api/v1/projects/{key}/statuses/{name}.
func APIStatusDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, s, ok := apiStatus(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.DeleteStatus(r.Context(), apiActor(r).ID, s.ID); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
