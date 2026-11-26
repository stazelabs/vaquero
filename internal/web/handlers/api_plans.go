package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// noPlan is the answer for a ticket without a plan.
func noPlan(key string) *api.Failure { return api.NotFound(key + " has no plan.") }

// currentPlan is the ticket's plan as the API answers it, read through the
// reader pool; ok is false (and the answer written) when there is none.
func currentPlan(w http.ResponseWriter, r *http.Request, d Deps, t gen.Ticket, p gen.Project) (api.Plan, gen.GetTicketPlanRow, bool) {
	key := ticketKey(p, t.Number)
	row, err := d.R.GetTicketPlan(r.Context(), t.ID)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteFailure(w, noPlan(key))
		return api.Plan{}, row, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return api.Plan{}, row, false
	}
	return api.Plan{Ticket: key, Revision: row.Revision, Body: row.Body, UpdatedBy: row.UpdatedByName, UpdatedAt: row.UpdatedAt}, row, true
}

// APIPlan is GET /api/v1/tickets/{ticket}/plan: the current revision.
func APIPlan(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		if out, _, ok := currentPlan(w, r, d, t, p); ok {
			api.WriteJSON(w, http.StatusOK, out)
		}
	}
}

// APIPlanRevision is GET /api/v1/tickets/{ticket}/plan/revisions/{n}: any
// revision, the current one included.
func APIPlanRevision(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		_, cur, ok := currentPlan(w, r, d, t, p)
		if !ok {
			return
		}
		key := ticketKey(p, t.Number)
		n, err := strconv.ParseInt(chi.URLParam(r, "n"), 10, 64)
		if err != nil || n < 1 {
			api.WriteFailure(w, api.NotFound("Plan revisions are numbered from 1."))
			return
		}
		rev, err := d.R.GetPlanRevision(r.Context(), gen.GetPlanRevisionParams{PlanID: cur.ID, Revision: n})
		if errors.Is(err, sql.ErrNoRows) {
			api.WriteFailure(w, api.NotFound("The plan for "+key+" has no revision "+strconv.FormatInt(n, 10)+"."))
			return
		}
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, api.Plan{Ticket: key, Revision: rev.Revision, Body: rev.Body, UpdatedBy: rev.ActorName, UpdatedAt: rev.At})
	}
}

// APIPlanSave is PUT /api/v1/tickets/{ticket}/plan: {"body", "revision"}.
// Without a revision it writes a new plan; with one, that revision must be
// the current one. A stale save is 409 with the plan as it is now.
func APIPlanSave(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in struct {
			Body     string `json:"body"`
			Revision int64  `json:"revision"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		saved, err := d.Core.SavePlan(r.Context(), apiActor(r).ID, t.ID, in.Body, in.Revision)
		if err != nil {
			f := api.FromCore(err, nil)
			if pc, ok := errors.AsType[*core.PlanConflict](err); ok {
				f.Current = planDTO(r, d, p, t, pc.Current)
			}
			apiFail(w, r, d, f, err)
			return
		}
		api.WriteJSON(w, http.StatusOK, planDTO(r, d, p, t, saved))
	}
}

// planDTO is pl as the API answers it; its author is named, or left empty
// when the name can't be read.
func planDTO(r *http.Request, d Deps, p gen.Project, t gen.Ticket, pl gen.Plan) api.Plan {
	out := api.Plan{Ticket: ticketKey(p, t.Number), Revision: pl.Revision, Body: pl.Body, UpdatedAt: pl.UpdatedAt}
	if a, err := d.R.GetActor(r.Context(), pl.UpdatedBy); err == nil {
		out.UpdatedBy = a.Name
	}
	return out
}
