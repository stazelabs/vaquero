package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

func tokenDTO(t gen.ListTokensRow) api.Token {
	return api.Token{Name: t.Name, Prefix: t.Prefix, Actor: t.ActorName, CreatedAt: t.CreatedAt,
		LastUsedAt: t.LastUsedAt.String, Revoked: t.RevokedAt.Valid}
}

// APITokens is GET /api/v1/tokens.
func APITokens(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListTokens(r.Context())
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Token, len(rows))
		for i, t := range rows {
			out[i] = tokenDTO(t)
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APITokenCreate is POST /api/v1/tokens {name}: a token acting as the
// caller's actor. The answer is the only time its value is shown.
func APITokenCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		a := apiActor(r)
		raw, t, err := d.Core.CreateToken(r.Context(), a.ID, a.ID, in.Name)
		if err != nil {
			apiCoreFail(w, r, d, gen.Project{}, err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, api.Token{Name: t.Name, Prefix: t.Prefix, Actor: a.Name, CreatedAt: t.CreatedAt, Token: raw})
	}
}

// APITokenRevoke is POST /api/v1/tokens/{prefix}/revoke.
func APITokenRevoke(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := chi.URLParam(r, "prefix")
		rows, err := d.R.ListTokens(r.Context())
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		for _, t := range rows {
			if strings.EqualFold(t.Prefix, prefix) && !t.RevokedAt.Valid {
				if err := d.Core.RevokeToken(r.Context(), apiActor(r).ID, t.ID); err != nil {
					apiCoreFail(w, r, d, gen.Project{}, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		api.WriteFailure(w, api.NotFound("No live token starts "+prefix+"."))
	}
}
