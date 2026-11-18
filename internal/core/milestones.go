package core

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// MilestoneInput is a milestone's editable fields. DueOn is "" or YYYY-MM-DD.
type MilestoneInput struct{ Name, Description, DueOn string }

// milestoneInput validates in against the project's other milestones.
func milestoneInput(ctx context.Context, q *gen.Queries, projectID, exceptID string, in MilestoneInput) (MilestoneInput, error) {
	f := fields{}
	in.Name = text(f, "name", in.Name, 80, "Enter a name.", "Keep the name under 80 characters.")
	in.Description = text(f, "description", in.Description, 20000, "", "Keep the description under 20,000 characters.")
	in.DueOn = strings.TrimSpace(in.DueOn)
	if in.DueOn != "" {
		if _, err := time.Parse(time.DateOnly, in.DueOn); err != nil {
			f.add("due_on", "Use a date like 2026-12-31.")
		}
	}
	ms, err := q.ListMilestones(ctx, projectID)
	if err != nil {
		return in, fmt.Errorf("list milestones: %w", err)
	}
	for _, m := range ms {
		if m.ID != exceptID && strings.EqualFold(m.Name, in.Name) {
			f.add("name", "A milestone with that name exists.")
		}
	}
	return in, f.err()
}

// CreateMilestone adds a milestone to a project.
func (c *Core) CreateMilestone(ctx context.Context, by, projectID string, in MilestoneInput) (gen.Milestone, error) {
	var m gen.Milestone
	err := c.tx(ctx, func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		in, err := milestoneInput(ctx, q, projectID, "", in)
		if err != nil {
			return err
		}
		m, err = q.CreateMilestone(ctx, gen.CreateMilestoneParams{
			ID: idgen.New(), ProjectID: projectID, Name: in.Name, Description: in.Description,
			DueOn: nullString(in.DueOn), CreatedAt: timefmt.Format(c.Now()),
		})
		if err != nil {
			return fmt.Errorf("create milestone: %w", err)
		}
		return c.emit(ctx, q, by, EvMilestoneCreated, projectID, "", map[string]string{"name": m.Name})
	})
	return m, err
}

// UpdateMilestone changes a milestone's name, description and due date.
func (c *Core) UpdateMilestone(ctx context.Context, by, milestoneID string, in MilestoneInput) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		m, err := q.GetMilestone(ctx, milestoneID)
		if err != nil {
			return notFound(err, "get milestone")
		}
		in, err := milestoneInput(ctx, q, m.ProjectID, m.ID, in)
		if err != nil {
			return err
		}
		changes := map[string]Change{}
		if in.Name != m.Name {
			changes["name"] = Change{m.Name, in.Name}
		}
		if in.Description != m.Description {
			changes["description"] = Change{}
		}
		if in.DueOn != m.DueOn.String {
			changes["due_on"] = Change{m.DueOn.String, in.DueOn}
		}
		if len(changes) == 0 {
			return nil
		}
		if err := q.UpdateMilestone(ctx, gen.UpdateMilestoneParams{Name: in.Name, Description: in.Description, DueOn: nullString(in.DueOn), ID: m.ID}); err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}
		return c.emit(ctx, q, by, EvMilestoneUpdated, m.ProjectID, "", map[string]any{"name": in.Name, "changes": changes})
	})
}

// SetMilestoneClosed closes or reopens a milestone.
func (c *Core) SetMilestoneClosed(ctx context.Context, by, milestoneID string, closed bool) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		m, err := q.GetMilestone(ctx, milestoneID)
		if err != nil {
			return notFound(err, "get milestone")
		}
		if m.ClosedAt.Valid == closed {
			if closed {
				return invalid("milestone", "It's already closed.")
			}
			return invalid("milestone", "It's already open.")
		}
		at, kind := sql.NullString{}, EvMilestoneReopened
		if closed {
			at, kind = timefmt.Null(c.Now()), EvMilestoneClosed
		}
		if err := q.SetMilestoneClosed(ctx, gen.SetMilestoneClosedParams{ClosedAt: at, ID: m.ID}); err != nil {
			return fmt.Errorf("close milestone: %w", err)
		}
		return c.emit(ctx, q, by, kind, m.ProjectID, "", map[string]string{"name": m.Name})
	})
}
