package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// OwnerName is the local owner actor's name.
const OwnerName = "Owner"

// EnsureOwner returns the local owner: the human actor named "Owner",
// created (with its own actor.created event) on first call.
func (c *Core) EnsureOwner(ctx context.Context) (gen.Actor, error) {
	var a gen.Actor
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		a, err = q.GetActorByKindName(ctx, gen.GetActorByKindNameParams{Kind: "human", Name: OwnerName})
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find owner: %w", err)
		}
		a, err = q.CreateActor(ctx, gen.CreateActorParams{ID: idgen.New(), Kind: "human", Name: OwnerName, CreatedAt: timefmt.Format(c.Now())})
		if err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
		return c.emit(ctx, q, a.ID, EvActorCreated, "", "", map[string]string{"kind": a.Kind, "name": a.Name})
	})
	return a, err
}
