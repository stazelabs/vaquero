package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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

// AgentNameRule is the sentence a bad agent name is refused with.
const AgentNameRule = "Agent names are 1 to 80 letters, digits, spaces, dots, dashes or underscores."

// ValidAgentName reports whether name, trimmed, is an acceptable agent name.
func ValidAgentName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// EnsureAgent returns the agent actor called name (trimmed), creating it,
// with an actor.created event by by, on first use.
func (c *Core) EnsureAgent(ctx context.Context, by, name string) (gen.Actor, error) {
	name = strings.TrimSpace(name)
	if !ValidAgentName(name) {
		return gen.Actor{}, &Invalid{Fields: map[string]string{"agent": AgentNameRule}}
	}
	var a gen.Actor
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		a, err = q.GetActorByKindName(ctx, gen.GetActorByKindNameParams{Kind: "agent", Name: name})
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find agent: %w", err)
		}
		a, err = q.CreateActor(ctx, gen.CreateActorParams{ID: idgen.New(), Kind: "agent", Name: name, CreatedAt: timefmt.Format(c.Now())})
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
		return c.emit(ctx, q, by, EvActorCreated, "", "", map[string]string{"kind": a.Kind, "name": a.Name})
	})
	return a, err
}
