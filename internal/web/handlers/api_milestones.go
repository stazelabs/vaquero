package handlers

import (
	"context"
	"net/http"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// milestoneDTOs is a project's milestones with their done and total counts.
func milestoneDTOs(ctx context.Context, d Deps, p gen.Project) ([]api.Milestone, []string, error) {
	ms, err := d.R.ListMilestones(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	progress, err := d.R.ListMilestoneProgress(ctx, p.ID)
	if err != nil {
		return nil, nil, err
	}
	counts := map[string][2]int64{}
	for _, m := range progress {
		counts[m.ID] = [2]int64{m.Done, m.Total}
	}
	out := make([]api.Milestone, len(ms))
	ids := make([]string, len(ms))
	for i, m := range ms {
		c := counts[m.ID]
		out[i] = api.Milestone{Name: m.Name, Description: m.Description, DueOn: m.DueOn.String, Closed: m.ClosedAt.Valid, Done: c[0], Total: c[1]}
		ids[i] = m.ID
	}
	return out, ids, nil
}

// writeMilestone answers the milestone with id as it is now.
func writeMilestone(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, id string, code int) {
	out, ids, err := milestoneDTOs(r.Context(), d, p)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	for i := range ids {
		if ids[i] == id {
			api.WriteJSON(w, code, out[i])
			return
		}
	}
	api.WriteFailure(w, api.NotFound("That no longer exists."))
}

// apiMilestone loads the {key} project and its {name} milestone; an
// unknown name is a 404.
func apiMilestone(w http.ResponseWriter, r *http.Request, d Deps) (gen.Project, gen.Milestone, bool) {
	p, ok := apiProject(w, r, d)
	if !ok {
		return p, gen.Milestone{}, false
	}
	n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
	m := n.milestoneRow(pathName(r))
	if n.pathMiss(w, r, "milestone") {
		return p, m, false
	}
	return p, m, true
}

// APIMilestones is GET /api/v1/projects/{key}/milestones.
func APIMilestones(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		out, _, err := milestoneDTOs(r.Context(), d, p)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIMilestoneCreate is POST /api/v1/projects/{key}/milestones {name,
// description?, due_on?}.
func APIMilestoneCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			DueOn       string `json:"due_on"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		m, err := d.Core.CreateMilestone(r.Context(), apiActor(r).ID, p.ID, core.MilestoneInput{Name: in.Name, Description: in.Description, DueOn: in.DueOn})
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeMilestone(w, r, d, p, m.ID, http.StatusCreated)
	}
}

// APIMilestoneUpdate is PATCH /api/v1/projects/{key}/milestones/{name}
// {name?, description?, due_on?}: only the fields sent change.
func APIMilestoneUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, m, ok := apiMilestone(w, r, d)
		if !ok {
			return
		}
		var in struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
			DueOn       *string `json:"due_on"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		mi := core.MilestoneInput{Name: m.Name, Description: m.Description, DueOn: m.DueOn.String}
		if in.Name != nil {
			mi.Name = *in.Name
		}
		if in.Description != nil {
			mi.Description = *in.Description
		}
		if in.DueOn != nil {
			mi.DueOn = *in.DueOn
		}
		if err := d.Core.UpdateMilestone(r.Context(), apiActor(r).ID, m.ID, mi); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeMilestone(w, r, d, p, m.ID, http.StatusOK)
	}
}

// APIMilestoneClose and APIMilestoneReopen are POST
// /api/v1/projects/{key}/milestones/{name}/close and /reopen.
func APIMilestoneClose(d Deps) http.HandlerFunc  { return milestoneClosed(d, true) }
func APIMilestoneReopen(d Deps) http.HandlerFunc { return milestoneClosed(d, false) }

func milestoneClosed(d Deps, closed bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, m, ok := apiMilestone(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.SetMilestoneClosed(r.Context(), apiActor(r).ID, m.ID, closed); err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeMilestone(w, r, d, p, m.ID, http.StatusOK)
	}
}
