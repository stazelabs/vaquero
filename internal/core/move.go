package core

import (
	"context"
	"fmt"
	"slices"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// minGap is the smallest gap between neighbours a midpoint may split;
// below it the column is renumbered first.
const minGap = 1e-6

// MoveTicket puts a ticket in statusID between beforeID (the card above it)
// and afterID (the card below it); either may be "" for the top or bottom
// of the column. A neighbour that isn't in that column, or is the ticket
// itself, is ignored; with no usable neighbour the ticket goes to the end.
// A status change records ticket.updated (the status change); a move within
// one column records ticket.reordered. A drop where the ticket already is
// writes nothing.
func (c *Core) MoveTicket(ctx context.Context, by, ticketID, statusID, beforeID, afterID string) (gen.Ticket, error) {
	var t gen.Ticket
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if t, err = q.GetTicket(ctx, ticketID); err != nil {
			return notFound(err, "get ticket")
		}
		to, err := q.GetStatus(ctx, statusID)
		if err != nil || to.ProjectID != t.ProjectID {
			return invalid("status", "Choose a status from this project.")
		}
		col, err := q.ListColumn(ctx, statusID)
		if err != nil {
			return fmt.Errorf("list column: %w", err)
		}
		was := slices.IndexFunc(col, func(r gen.ListColumnRow) bool { return r.ID == t.ID })
		if was >= 0 {
			col = slices.Delete(col, was, was+1)
		}
		find := func(id string) int {
			if id == "" || id == t.ID {
				return -1
			}
			return slices.IndexFunc(col, func(r gen.ListColumnRow) bool { return r.ID == id })
		}
		at := len(col) // the index the ticket takes in col
		if i := find(beforeID); i >= 0 {
			at = i + 1
		} else if i := find(afterID); i >= 0 {
			at = i
		}
		if was == at {
			return nil // already there
		}

		pos, err := c.placeIn(ctx, q, col, at)
		if err != nil {
			return err
		}
		now := timefmt.Format(c.Now())
		statusAt := t.StatusChangedAt
		if statusID != t.StatusID {
			statusAt = now
		}
		if err := q.PlaceTicket(ctx, gen.PlaceTicketParams{StatusID: statusID, Position: pos, StatusChangedAt: statusAt, UpdatedAt: now, ID: t.ID}); err != nil {
			return fmt.Errorf("place ticket: %w", err)
		}
		from := t.StatusID
		if t, err = q.GetTicket(ctx, t.ID); err != nil {
			return fmt.Errorf("reload ticket: %w", err)
		}
		if from == statusID {
			return c.emit(ctx, q, by, EvTicketReordered, t.ProjectID, t.ID, map[string]string{"status": to.Name})
		}
		old, err := q.GetStatus(ctx, from)
		if err != nil {
			return fmt.Errorf("get status: %w", err)
		}
		return c.emit(ctx, q, by, EvTicketUpdated, t.ProjectID, t.ID, map[string]any{"changes": map[string]Change{"status": {old.Name, to.Name}}})
	})
	return t, err
}

// placeIn is the position for a ticket inserted at index at of col (the
// column without it). When the gap there is too small to split, the column
// is renumbered in steps of positionStep first.
func (c *Core) placeIn(ctx context.Context, q *gen.Queries, col []gen.ListColumnRow, at int) (float64, error) {
	switch {
	case len(col) == 0:
		return positionStep, nil
	case at == 0:
		return col[0].Position - positionStep, nil
	case at == len(col):
		return col[len(col)-1].Position + positionStep, nil
	}
	prev, next := col[at-1].Position, col[at].Position
	if next-prev >= minGap {
		return prev + (next-prev)/2, nil
	}
	for i, r := range col {
		n := i + 1
		if i >= at {
			n++ // leave a step free for the inserted ticket
		}
		if err := q.SetTicketPosition(ctx, gen.SetTicketPositionParams{Position: float64(n) * positionStep, ID: r.ID}); err != nil {
			return 0, fmt.Errorf("renumber column: %w", err)
		}
	}
	return float64(at+1) * positionStep, nil
}
