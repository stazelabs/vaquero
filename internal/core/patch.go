package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// TicketPatch names the fields to change; a nil field keeps its value.
// AssigneeID, ParentID and MilestoneID may be "" to clear them. Tags (when
// SetTags) replaces the whole tag set; AddTags and RemoveTags then adjust
// it, ignoring case.
type TicketPatch struct {
	Title, Body, StatusID, AssigneeID, ParentID, MilestoneID *string
	Priority                                                 *int64
	Tags, AddTags, RemoveTags                                []string
	SetTags                                                  bool
}

// Patch is the TicketPatch that sets every field to in's value.
func (in TicketInput) Patch() TicketPatch {
	return TicketPatch{
		Title: &in.Title, Body: &in.Body, StatusID: &in.StatusID, AssigneeID: &in.AssigneeID,
		ParentID: &in.ParentID, MilestoneID: &in.MilestoneID, Priority: &in.Priority,
		Tags: in.Tags, SetTags: true,
	}
}

// Conflict is a save refused because the ticket changed since the caller
// read it. Current is the ticket as it is now.
type Conflict struct {
	Key     string
	Current gen.Ticket
}

func (e *Conflict) Error() string { return e.Key + " changed since you read it." }

// PatchTicket changes the named fields of a ticket. With version > 0 the
// ticket must still be at that version, else *Conflict and nothing is
// written. A ticket whose status changes moves to the end of the new
// status's column. A patch that changes nothing writes nothing.
func (c *Core) PatchTicket(ctx context.Context, by, ticketID string, patch TicketPatch, version int64) (gen.Ticket, error) {
	var t gen.Ticket
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if t, err = q.GetTicket(ctx, ticketID); err != nil {
			return notFound(err, "get ticket")
		}
		p, err := q.GetProject(ctx, t.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		if version > 0 && version != t.Version {
			return &Conflict{Key: TicketKey{Project: p.Key, Number: t.Number}.String(), Current: t}
		}
		in, err := c.applyPatch(ctx, q, t, patch)
		if err != nil {
			return err
		}
		tf, err := checkTicket(ctx, q, p, t.ID, in)
		if err != nil {
			return err
		}
		old, err := c.ticketView(ctx, q, p, t)
		if err != nil {
			return err
		}
		newTags, err := c.upsertTags(ctx, q, tf.Tags)
		if err != nil {
			return err
		}
		tf.tags = newTags

		changes := diffTicket(old, tf)
		if len(changes) == 0 {
			return nil
		}
		now := timefmt.Format(c.Now())
		pos, statusAt := t.Position, t.StatusChangedAt
		if tf.StatusID != t.StatusID {
			if pos, err = c.nextPosition(ctx, q, tf.StatusID); err != nil {
				return err
			}
			statusAt = now
		}
		if err := q.UpdateTicket(ctx, gen.UpdateTicketParams{
			Title: tf.Title, Body: tf.Body, StatusID: tf.StatusID, Priority: tf.Priority,
			AssigneeID: nullString(tf.AssigneeID), ParentID: nullString(tf.ParentID),
			MilestoneID: nullString(tf.MilestoneID), Position: pos, UpdatedAt: now, StatusChangedAt: statusAt, ID: t.ID,
		}); err != nil {
			return fmt.Errorf("update ticket: %w", err)
		}
		if _, ok := changes["tags"]; ok {
			if err := c.replaceTags(ctx, q, t.ID, old.tags, newTags); err != nil {
				return err
			}
		}
		if t, err = q.GetTicket(ctx, t.ID); err != nil {
			return fmt.Errorf("reload ticket: %w", err)
		}
		return c.emit(ctx, q, by, EvTicketUpdated, p.ID, t.ID, map[string]any{"changes": changes})
	})
	return t, err
}

// applyPatch is the ticket's current fields with patch laid over them.
func (c *Core) applyPatch(ctx context.Context, q *gen.Queries, t gen.Ticket, p TicketPatch) (TicketInput, error) {
	in := TicketInput{
		Title: t.Title, Body: t.Body, StatusID: t.StatusID, Priority: t.Priority,
		AssigneeID: t.AssigneeID.String, ParentID: t.ParentID.String, MilestoneID: t.MilestoneID.String,
	}
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&in.Title, p.Title)
	set(&in.Body, p.Body)
	set(&in.StatusID, p.StatusID)
	set(&in.AssigneeID, p.AssigneeID)
	set(&in.ParentID, p.ParentID)
	set(&in.MilestoneID, p.MilestoneID)
	if p.Priority != nil {
		in.Priority = *p.Priority
	}
	if p.SetTags {
		in.Tags = slices.Clone(p.Tags)
	} else {
		cur, err := q.ListTicketTags(ctx, t.ID)
		if err != nil {
			return in, fmt.Errorf("ticket tags: %w", err)
		}
		in.Tags = tagNames(cur)
	}
	in.Tags = append(in.Tags, p.AddTags...)
	in.Tags = slices.DeleteFunc(in.Tags, func(n string) bool {
		return slices.ContainsFunc(p.RemoveTags, func(r string) bool { return strings.EqualFold(strings.TrimSpace(r), strings.TrimSpace(n)) })
	})
	return in, nil
}
