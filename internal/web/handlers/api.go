package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// apiActor is the actor behind the request's token (APIToken guarantees one).
func apiActor(r *http.Request) gen.Actor {
	a, _ := middleware.APIActor(r.Context())
	return a
}

// APINotFound and APIMethodNotAllowed answer in JSON inside /api/v1.
func APINotFound(w http.ResponseWriter, r *http.Request) {
	api.WriteFailure(w, api.NotFound("There's nothing at "+middleware.RedactPath(r.URL.Path)+"."))
}

func APIMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	api.WriteFailure(w, api.Fail(http.StatusMethodNotAllowed, "method_not_allowed", r.Method+" isn't allowed here."))
}

// APIMe is GET /api/v1/me: who the token acts as, and the server's version.
// name is the actor's identity (vaquero mcp checks it); display_name and
// shown_name are its profile's (VAQ-149).
func APIMe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := apiActor(r)
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"actor":   map[string]string{"name": a.Name, "kind": a.Kind, "display_name": a.DisplayName, "shown_name": core.ActorName(a)},
			"version": version.Version,
		})
	}
}
