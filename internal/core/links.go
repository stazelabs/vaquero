package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// LinkKinds are the ways one ticket can point at another: "blocks",
// "relates" (to) and "duplicates".
var LinkKinds = []string{"blocks", "relates", "duplicates"}

// AddLink links a ticket to the one named by to, in any project.
func (c *Core) AddLink(ctx context.Context, by, fromTicketID, kind string, to TicketKey) (gen.TicketLink, error) {
	var l gen.TicketLink
	err := c.tx(ctx, func(q *gen.Queries) error {
		from, err := q.GetTicket(ctx, fromTicketID)
		if err != nil {
			return notFound(err, "get ticket")
		}
		f := fields{}
		if !slices.Contains(LinkKinds, kind) {
			f.add("kind", "Choose blocks, relates to or duplicates.")
		}
		target, _, err := c.ResolveTicket(ctx, q, to)
		switch {
		case errors.Is(err, ErrNotFound):
			f.add("to", "No ticket "+to.String()+".")
		case err != nil:
			return err
		case target.ID == from.ID:
			f.add("to", "A ticket can't link to itself.")
		}
		if err := f.err(); err != nil {
			return err
		}
		existing, err := q.ListLinksFrom(ctx, from.ID)
		if err != nil {
			return fmt.Errorf("list links: %w", err)
		}
		// "relates" has no direction, so either way round is a repeat.
		back, err := q.ListLinksTo(ctx, from.ID)
		if err != nil {
			return fmt.Errorf("list links: %w", err)
		}
		dup := slices.ContainsFunc(existing, func(x gen.ListLinksFromRow) bool { return x.OtherID == target.ID && x.Kind == kind }) ||
			kind == "relates" && slices.ContainsFunc(back, func(x gen.ListLinksToRow) bool { return x.OtherID == target.ID && x.Kind == kind })
		if dup {
			return invalid("to", "Those tickets are already linked that way.")
		}
		l, err = q.CreateLink(ctx, gen.CreateLinkParams{ID: idgen.New(), FromID: from.ID, ToID: target.ID, Kind: kind, CreatedAt: timefmt.Format(c.Now())})
		if err != nil {
			return fmt.Errorf("create link: %w", err)
		}
		return c.emit(ctx, q, by, EvLinkCreated, from.ProjectID, from.ID, map[string]string{"link_id": l.ID, "kind": kind, "to": to.String()})
	})
	return l, err
}

// RemoveLink deletes a link.
func (c *Core) RemoveLink(ctx context.Context, by, linkID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		l, err := q.GetLink(ctx, linkID)
		if err != nil {
			return notFound(err, "get link")
		}
		from, err := q.GetTicket(ctx, l.FromID)
		if err != nil {
			return fmt.Errorf("get ticket: %w", err)
		}
		to, err := q.GetTicket(ctx, l.ToID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("link target missing: %w", err)
		} else if err != nil {
			return fmt.Errorf("get ticket: %w", err)
		}
		toProject, err := q.GetProject(ctx, to.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		if err := q.DeleteLink(ctx, l.ID); err != nil {
			return fmt.Errorf("delete link: %w", err)
		}
		key := TicketKey{Project: toProject.Key, Number: to.Number}.String()
		return c.emit(ctx, q, by, EvLinkDeleted, from.ProjectID, from.ID, map[string]string{"link_id": l.ID, "kind": l.Kind, "to": key})
	})
}
