package core

import (
	"context"
	"fmt"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func commentBody(body string) (string, error) {
	f := fields{}
	body = checked(f, "body", trimBlock(body), 20000, "Write a comment.", "Keep comments under 20,000 characters.")
	return body, f.err()
}

// AddComment adds a comment to a ticket's thread.
func (c *Core) AddComment(ctx context.Context, by, ticketID, body string) (gen.Comment, error) {
	var cm gen.Comment
	err := c.tx(ctx, func(q *gen.Queries) error {
		t, err := q.GetTicket(ctx, ticketID)
		if err != nil {
			return notFound(err, "get ticket")
		}
		body, err := commentBody(body)
		if err != nil {
			return err
		}
		now := timefmt.Format(c.Now())
		cm, err = q.CreateComment(ctx, gen.CreateCommentParams{ID: idgen.New(), TicketID: t.ID, AuthorID: by, Body: body, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		return c.emit(ctx, q, by, EvCommentCreated, t.ProjectID, t.ID, map[string]string{"comment_id": cm.ID})
	})
	return cm, err
}

// ownComment loads a live comment that by wrote, and its ticket.
func ownComment(ctx context.Context, q *gen.Queries, by, commentID, verb string) (gen.Comment, gen.Ticket, error) {
	cm, err := q.GetComment(ctx, commentID)
	if err != nil {
		return cm, gen.Ticket{}, notFound(err, "get comment")
	}
	if cm.DeletedAt.Valid {
		return cm, gen.Ticket{}, invalid("body", "That comment was deleted.")
	}
	if cm.AuthorID != by {
		return cm, gen.Ticket{}, invalid("body", "You can only "+verb+" your own comments.")
	}
	t, err := q.GetTicket(ctx, cm.TicketID)
	if err != nil {
		return cm, t, fmt.Errorf("get ticket: %w", err)
	}
	return cm, t, nil
}

// EditComment replaces the body of the caller's own comment.
func (c *Core) EditComment(ctx context.Context, by, commentID, body string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		cm, t, err := ownComment(ctx, q, by, commentID, "edit")
		if err != nil {
			return err
		}
		body, err := commentBody(body)
		if err != nil {
			return err
		}
		if body == cm.Body {
			return nil
		}
		if err := q.UpdateComment(ctx, gen.UpdateCommentParams{Body: body, UpdatedAt: timefmt.Format(c.Now()), ID: cm.ID}); err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
		return c.emit(ctx, q, by, EvCommentUpdated, t.ProjectID, t.ID, map[string]string{"comment_id": cm.ID})
	})
}

// DeleteComment hides the caller's own comment. The row stays, so the
// activity history still makes sense.
func (c *Core) DeleteComment(ctx context.Context, by, commentID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		cm, t, err := ownComment(ctx, q, by, commentID, "delete")
		if err != nil {
			return err
		}
		if err := q.SoftDeleteComment(ctx, gen.SoftDeleteCommentParams{DeletedAt: timefmt.Null(c.Now()), ID: cm.ID}); err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		return c.emit(ctx, q, by, EvCommentDeleted, t.ProjectID, t.ID, map[string]string{"comment_id": cm.ID})
	})
}
