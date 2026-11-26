package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func refDTO(x gen.TicketRef) api.Ref {
	return api.Ref{ID: x.ID, Kind: x.Kind, Target: x.Target, Anchor: x.Anchor, Label: x.Label}
}

// APIRefCreate is POST /api/v1/tickets/{ticket}/refs {kind, target, anchor?, label?}.
func APIRefCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in struct{ Kind, Target, Anchor, Label string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		x, err := d.Core.AddRef(r.Context(), apiActor(r).ID, t.ID, core.RefInput{Kind: in.Kind, Target: in.Target, Anchor: in.Anchor, Label: in.Label})
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		out := refDTO(x)
		if _, _, out.Warning, err = ticketClaim(r, d, t); err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, out)
	}
}

// APIRefDelete is DELETE /api/v1/tickets/{ticket}/refs/{id}. The reference
// must be this ticket's.
func APIRefDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		id := chi.URLParam(r, "id")
		x, err := d.R.GetTicketRef(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) || err == nil && x.TicketID != t.ID {
			api.WriteFailure(w, api.NotFound("No such reference."))
			return
		}
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		if err := d.Core.RemoveRef(r.Context(), apiActor(r).ID, id); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				api.WriteFailure(w, api.NotFound("No such reference."))
				return
			}
			apiCoreFail(w, r, d, p, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
