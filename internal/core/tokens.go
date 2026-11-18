package core

import (
	"context"
	"fmt"

	"github.com/stazelabs/vaquero/internal/apitoken"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// CreateToken mints an API token that acts as actorID. raw is returned
// once and never stored: only its hash and prefix are.
func (c *Core) CreateToken(ctx context.Context, by, actorID, name string) (raw string, t gen.ApiToken, err error) {
	f := fields{}
	name = text(f, "name", name, 80, "Name the token.", "Keep the name under 80 characters.")
	if err := f.err(); err != nil {
		return "", t, err
	}
	raw, err = apitoken.Mint()
	if err != nil {
		return "", t, fmt.Errorf("mint token: %w", err)
	}
	err = c.tx(ctx, func(q *gen.Queries) error {
		if _, err := q.GetActor(ctx, actorID); err != nil {
			return notFound(err, "get actor")
		}
		var err error
		t, err = q.CreateToken(ctx, gen.CreateTokenParams{
			ID: idgen.New(), ActorID: actorID, Name: name, Prefix: apitoken.Prefix(raw),
			TokenHash: apitoken.Hash(raw), CreatedAt: timefmt.Format(c.Now()),
		})
		if err != nil {
			return fmt.Errorf("create token: %w", err)
		}
		return c.emit(ctx, q, by, EvTokenCreated, "", "", map[string]string{"name": t.Name, "prefix": t.Prefix})
	})
	if err != nil {
		return "", gen.ApiToken{}, err
	}
	return raw, t, nil
}

// RevokeToken stops a token working.
func (c *Core) RevokeToken(ctx context.Context, by, tokenID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		t, err := q.GetToken(ctx, tokenID)
		if err != nil {
			return notFound(err, "get token")
		}
		if t.RevokedAt.Valid {
			return invalid("token", "That token is already revoked.")
		}
		if err := q.RevokeToken(ctx, gen.RevokeTokenParams{RevokedAt: timefmt.Null(c.Now()), ID: t.ID}); err != nil {
			return fmt.Errorf("revoke token: %w", err)
		}
		return c.emit(ctx, q, by, EvTokenRevoked, "", "", map[string]string{"name": t.Name, "prefix": t.Prefix})
	})
}
