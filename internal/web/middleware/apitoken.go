package middleware

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/version"
)

// MinClient is the oldest CLI this server answers properly; clients older
// than it tell the person to upgrade.
const MinClient = "0.0.0"

// APIHeaders sets the headers every API answer carries.
func APIHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Vaquero-Version", version.Version)
		h.Set("Vaquero-Min-Client", MinClient)
		next.ServeHTTP(w, r)
	})
}

// Toucher records that a token was used. It is the one write the API's
// middleware makes, outside core.
type Toucher interface {
	TouchToken(ctx context.Context, arg gen.TouchTokenParams) error
}

// APIAuth is what APIToken needs.
type APIAuth struct {
	R      *gen.Queries // reader
	Touch  Toucher      // writer, last-used only
	Now    func() time.Time
	Logger *slog.Logger
}

type apiActorKey struct{}

// APIActor is the actor the request's token acts as.
func APIActor(ctx context.Context) (gen.Actor, bool) {
	a, ok := ctx.Value(apiActorKey{}).(gen.Actor)
	return a, ok
}

// APIToken requires a live bearer token. Every refusal is the same 401, so
// nothing tells a caller why; the reason is logged with the token's id (or
// nothing, for a token we don't know), never the token itself.
func APIToken(a APIAuth) func(http.Handler) http.Handler {
	refuse := func(w http.ResponseWriter, r *http.Request, reason, tokenID string) {
		a.Logger.InfoContext(r.Context(), "api token refused", "reason", reason, "token_id", tokenID, "request_id", GetRequestID(r.Context()))
		w.Header().Set("WWW-Authenticate", `Bearer realm="api", error="invalid_token"`)
		api.WriteFailure(w, api.Fail(http.StatusUnauthorized, "unauthorized", "A valid API token is required."))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := apitoken.Parse(r.Header.Get("Authorization"))
			if !ok {
				refuse(w, r, "missing or malformed", "")
				return
			}
			ctx := r.Context()
			t, err := a.R.GetTokenByHash(ctx, apitoken.Hash(raw))
			if err == sql.ErrNoRows {
				refuse(w, r, "unknown", "")
				return
			}
			if err != nil {
				a.Logger.ErrorContext(ctx, "api token lookup", "err", err)
				api.WriteFailure(w, api.Internal())
				return
			}
			if t.RevokedAt.Valid {
				refuse(w, r, "revoked", t.ID)
				return
			}
			actor, err := a.R.GetActor(ctx, t.ActorID)
			if err != nil {
				a.Logger.ErrorContext(ctx, "api token actor", "err", err, "token_id", t.ID)
				api.WriteFailure(w, api.Internal())
				return
			}
			now := a.Now()
			if !t.LastUsedAt.Valid || t.LastUsedAt.String < timefmt.Format(now.Add(-apitoken.TouchEvery)) {
				tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				if err := a.Touch.TouchToken(tctx, gen.TouchTokenParams{
					Now: sql.NullString{String: timefmt.Format(now), Valid: true}, ID: t.ID,
					Before: sql.NullString{String: timefmt.Format(now.Add(-apitoken.TouchEvery)), Valid: true},
				}); err != nil {
					a.Logger.WarnContext(ctx, "api token touch", "err", err, "token_id", t.ID)
				}
				cancel()
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, apiActorKey{}, actor)))
		})
	}
}
