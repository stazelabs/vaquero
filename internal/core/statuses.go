package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
)

// Categories are the fixed status categories, in board order.
var Categories = []string{"backlog", "todo", "in_progress", "in_review", "done", "cancelled"}

// statusName validates a status name and refuses one another status in the
// project already uses (ignoring case). exceptID is the status being renamed.
func statusName(ctx context.Context, q *gen.Queries, f fields, projectID, exceptID, name string) (string, error) {
	name = text(f, "name", name, 40, "Enter a name.", "Keep the name under 40 characters.")
	ss, err := q.ListStatuses(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("list statuses: %w", err)
	}
	for _, s := range ss {
		if s.ID != exceptID && strings.EqualFold(s.Name, name) {
			f.add("name", "A status with that name exists.")
		}
	}
	return name, nil
}

// CreateStatus adds a status right after the last status of its category
// or, when its category has none, after the last status of the nearest
// earlier category (first when there is none).
func (c *Core) CreateStatus(ctx context.Context, by, projectID, name, category string) (gen.Status, error) {
	var s gen.Status
	err := c.tx(ctx, func(q *gen.Queries) error {
		if _, err := q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		f := fields{}
		name, err := statusName(ctx, q, f, projectID, "", name)
		if err != nil {
			return err
		}
		if !slices.Contains(Categories, category) {
			f.add("category", "Choose a category.")
		}
		if err := f.err(); err != nil {
			return err
		}
		ss, err := q.ListStatuses(ctx, projectID)
		if err != nil {
			return fmt.Errorf("list statuses: %w", err)
		}
		at := endOfCategory(ss, category)
		if err := renumberStatuses(ctx, q, ss, at); err != nil {
			return err
		}
		if s, err = q.CreateStatus(ctx, gen.CreateStatusParams{ID: idgen.New(), ProjectID: projectID, Name: name, Category: category, Position: int64(at + 1)}); err != nil {
			return fmt.Errorf("create status: %w", err)
		}
		return c.emit(ctx, q, by, EvStatusCreated, projectID, "", map[string]string{"name": name, "category": category})
	})
	return s, err
}

// endOfCategory is the index a new status of category takes in ss (by
// position): after the last of its category, else after the last of the
// nearest earlier category in Categories, else 0.
func endOfCategory(ss []gen.Status, category string) int {
	for k := slices.Index(Categories, category); k >= 0; k-- {
		for i, s := range slices.Backward(ss) {
			if s.Category == Categories[k] {
				return i + 1
			}
		}
	}
	return 0
}

// renumberStatuses gives ss positions 1, 2, 3... in order, leaving out
// position gap+1 for a status about to be inserted (gap -1: none), and
// writes only the positions that change.
func renumberStatuses(ctx context.Context, q *gen.Queries, ss []gen.Status, gap int) error {
	for i, s := range ss {
		pos := int64(i + 1)
		if gap >= 0 && i >= gap {
			pos++
		}
		if s.Position == pos {
			continue
		}
		if err := q.SetStatusPosition(ctx, gen.SetStatusPositionParams{Position: pos, ID: s.ID}); err != nil {
			return fmt.Errorf("renumber statuses: %w", err)
		}
	}
	return nil
}

// RenameStatus changes a status's name. Its category never changes.
func (c *Core) RenameStatus(ctx context.Context, by, statusID, name string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		s, err := q.GetStatus(ctx, statusID)
		if err != nil {
			return notFound(err, "get status")
		}
		f := fields{}
		name, err := statusName(ctx, q, f, s.ProjectID, s.ID, name)
		if err != nil {
			return err
		}
		if err := f.err(); err != nil {
			return err
		}
		if name == s.Name {
			return nil
		}
		if err := q.RenameStatus(ctx, gen.RenameStatusParams{Name: name, ID: s.ID}); err != nil {
			return fmt.Errorf("rename status: %w", err)
		}
		return c.emit(ctx, q, by, EvStatusUpdated, s.ProjectID, "", map[string]any{"changes": map[string]Change{"name": {s.Name, name}}})
	})
}

// MoveStatus swaps a status with its neighbour: delta -1 moves it up, +1 down.
func (c *Core) MoveStatus(ctx context.Context, by, statusID string, delta int) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		s, err := q.GetStatus(ctx, statusID)
		if err != nil {
			return notFound(err, "get status")
		}
		if delta != -1 && delta != 1 {
			return invalid("status", "Move one place at a time.")
		}
		ss, err := q.ListStatuses(ctx, s.ProjectID)
		if err != nil {
			return fmt.Errorf("list statuses: %w", err)
		}
		i := slices.IndexFunc(ss, func(x gen.Status) bool { return x.ID == s.ID })
		j := i + delta
		if j < 0 {
			return invalid("status", "It's already first.")
		}
		if j >= len(ss) {
			return invalid("status", "It's already last.")
		}
		if err := q.SetStatusPosition(ctx, gen.SetStatusPositionParams{Position: ss[j].Position, ID: ss[i].ID}); err != nil {
			return fmt.Errorf("move status: %w", err)
		}
		if err := q.SetStatusPosition(ctx, gen.SetStatusPositionParams{Position: ss[i].Position, ID: ss[j].ID}); err != nil {
			return fmt.Errorf("move status: %w", err)
		}
		return c.emit(ctx, q, by, EvStatusMoved, s.ProjectID, "", map[string]any{"name": s.Name, "from": i + 1, "to": j + 1})
	})
}

// DeleteStatus removes an unused status, keeping at least one per category.
func (c *Core) DeleteStatus(ctx context.Context, by, statusID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		s, err := q.GetStatus(ctx, statusID)
		if err != nil {
			return notFound(err, "get status")
		}
		n, err := q.CountTicketsInStatus(ctx, s.ID)
		if err != nil {
			return fmt.Errorf("count tickets: %w", err)
		}
		if n > 0 {
			return invalid("status", "Move this status's tickets first.")
		}
		same, err := q.CountStatusesInCategory(ctx, gen.CountStatusesInCategoryParams{ProjectID: s.ProjectID, Category: s.Category})
		if err != nil {
			return fmt.Errorf("count statuses: %w", err)
		}
		if same <= 1 {
			return invalid("status", "A project needs at least one status in each category.")
		}
		if err := q.DeleteStatus(ctx, s.ID); err != nil {
			return fmt.Errorf("delete status: %w", err)
		}
		return c.emit(ctx, q, by, EvStatusDeleted, s.ProjectID, "", map[string]string{"name": s.Name})
	})
}
