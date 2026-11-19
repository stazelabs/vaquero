package middleware

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/version"
)

// MinClient is the oldest CLI this server answers properly; clients older
// than it tell the person to upgrade. A variable only so tests can raise it.
var MinClient = "0.0.0"

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
	// Local is local mode: there, a request made with the owner's token
	// (OwnerID) and a Vaquero-Agent header acts as that agent, made by
	// Agent (core.EnsureAgent) on first use. Elsewhere the header is ignored.
	Local   bool
	OwnerID string
	Agent   func(ctx context.Context, by, name string) (gen.Actor, error)
}

// AgentHeader names the agent a local request acts as.
const AgentHeader = "Vaquero-Agent"

type apiActorKey struct{}

// WithAPIActor is ctx acting as a: what APIToken sets, for the MCP
// server's calls into the API.
func WithAPIActor(ctx context.Context, a gen.Actor) context.Context {
	return context.WithValue(ctx, apiActorKey{}, a)
}

type apiTokenPrefixKey struct{}

// APITokenPrefix is the shown prefix of the request's token ("vq_" and 6
// characters), never the token.
func APITokenPrefix(ctx context.Context) string {
	p, _ := ctx.Value(apiTokenPrefixKey{}).(string)
	return p
}

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
			if name := r.Header.Get(AgentHeader); name != "" && a.Local && a.Agent != nil && actor.ID == a.OwnerID {
				agent, err := a.Agent(ctx, actor.ID, name)
				if inv, ok := errors.AsType[*core.Invalid](err); ok {
					api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", inv.Fields["agent"]))
					return
				}
				if err != nil {
					a.Logger.ErrorContext(ctx, "api agent", "err", err)
					api.WriteFailure(w, api.Internal())
					return
				}
				actor = agent
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
			ctx = context.WithValue(ctx, apiTokenPrefixKey{}, t.Prefix)
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, apiActorKey{}, actor)))
		})
	}
}
