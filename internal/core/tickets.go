package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// TicketInput is every editable field of a ticket.
type TicketInput struct {
	Title, Body string
	StatusID    string   // "" means the project's first status in category todo
	Priority    int64    // 0 none, 1 urgent, 2 high, 3 medium, 4 low
	AssigneeID  string   // "" for none
	ParentID    string   // "" for none
	MilestoneID string   // "" for none
	Tags        []string // names; created as needed
}

// positionStep spaces tickets within a status column so later drags can
// insert between neighbours.
const positionStep = 1024

// ticketFields is a validated TicketInput plus what the event payload names.
type ticketFields struct {
	TicketInput
	status    gen.Status
	assignee  string // name, "" for none
	parentKey string // "VAQ-3", "" for none
	milestone string // name, "" for none
	tags      []gen.Tag
}

// normTags trims tag names and drops blanks and case-insensitive repeats,
// keeping the first spelling.
func normTags(f fields, names []string) []string {
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			continue
		}
		if len([]rune(n)) > 40 {
			f.add("tags", "Keep each tag under 40 characters.")
		}
		out = append(out, n)
	}
	return out
}

// checkTicket validates in for a ticket in project p. ticketID is the
// ticket being updated ("" when creating). It reads only; tags are created
// later, after every check has passed.
func checkTicket(ctx context.Context, q *gen.Queries, p gen.Project, ticketID string, in TicketInput) (ticketFields, error) {
	f := fields{}
	tf := ticketFields{TicketInput: in}
	tf.Title = text(f, "title", in.Title, 200, "Enter a title.", "Keep the title under 200 characters.")
	tf.Body = strings.TrimRight(in.Body, " \t\r\n")
	if len([]rune(tf.Body)) > 100000 {
		f.add("body", "Keep the description under 100,000 characters.")
	}
	tf.Tags = normTags(f, in.Tags)
	if in.Priority < 0 || in.Priority > 4 {
		f.add("priority", "Choose a priority.")
	}

	statuses, err := q.ListStatuses(ctx, p.ID)
	if err != nil {
		return tf, fmt.Errorf("list statuses: %w", err)
	}
	if in.StatusID == "" {
		i := slices.IndexFunc(statuses, func(s gen.Status) bool { return s.Category == "todo" })
		if i < 0 {
			return tf, errors.New("project has no todo status")
		}
		tf.status = statuses[i]
	} else if i := slices.IndexFunc(statuses, func(s gen.Status) bool { return s.ID == in.StatusID }); i >= 0 {
		tf.status = statuses[i]
	} else {
		f.add("status", "Choose a status from this project.")
	}
	tf.StatusID = tf.status.ID

	if in.AssigneeID != "" {
		a, err := q.GetActor(ctx, in.AssigneeID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			f.add("assignee", "Choose someone from the list.")
		case err != nil:
			return tf, fmt.Errorf("get assignee: %w", err)
		default:
			tf.assignee = a.Name
		}
	}

	if in.MilestoneID != "" {
		m, err := q.GetMilestone(ctx, in.MilestoneID)
		switch {
		case errors.Is(err, sql.ErrNoRows) || err == nil && m.ProjectID != p.ID:
			f.add("milestone", "Choose a milestone from this project.")
		case err != nil:
			return tf, fmt.Errorf("get milestone: %w", err)
		default:
			tf.milestone = m.Name
		}
	}

	if in.ParentID != "" {
		if err := checkParent(ctx, q, f, p, ticketID, in.ParentID, &tf); err != nil {
			return tf, err
		}
	}
	return tf, f.err()
}

// checkParent refuses a parent that is the ticket itself, one of its
// sub-tickets (at any depth), missing, or in another project.
func checkParent(ctx context.Context, q *gen.Queries, f fields, p gen.Project, ticketID, parentID string, tf *ticketFields) error {
	if parentID == ticketID {
		f.add("parent", "A ticket can't be its own parent.")
		return nil
	}
	parent, err := q.GetTicket(ctx, parentID)
	if errors.Is(err, sql.ErrNoRows) {
		f.add("parent", "Choose an existing ticket as the parent.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("get parent: %w", err)
	}
	if parent.ProjectID != p.ID {
		f.add("parent", "The parent must be in the same project.")
		return nil
	}
	if ticketID != "" {
		up, err := q.ListAncestorIDs(ctx, parentID)
		if err != nil {
			return fmt.Errorf("parent ancestors: %w", err)
		}
		if slices.Contains(up, ticketID) {
			f.add("parent", "That ticket is a sub-ticket of this one.")
			return nil
		}
	}
	tf.parentKey = TicketKey{Project: p.Key, Number: parent.Number}.String()
	return nil
}

// upsertTags creates any missing tags and returns them all, sorted by name.
func (c *Core) upsertTags(ctx context.Context, q *gen.Queries, names []string) ([]gen.Tag, error) {
	tags := make([]gen.Tag, 0, len(names))
	for _, n := range names {
		t, err := q.UpsertTag(ctx, gen.UpsertTagParams{ID: idgen.New(), Name: n, CreatedAt: timefmt.Format(c.Now())})
		if err != nil {
			return nil, fmt.Errorf("upsert tag: %w", err)
		}
		tags = append(tags, t)
	}
	slices.SortFunc(tags, func(a, b gen.Tag) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return tags, nil
}

func tagNames(tags []gen.Tag) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = t.Name
	}
	return out
}

func (c *Core) nextPosition(ctx context.Context, q *gen.Queries, statusID string) (float64, error) {
	max, err := q.MaxPositionInStatus(ctx, statusID)
	if err != nil {
		return 0, fmt.Errorf("max position: %w", err)
	}
	return max + positionStep, nil
}

// CreateTicket adds a ticket to a project, numbering it next.
func (c *Core) CreateTicket(ctx context.Context, by, projectID string, in TicketInput) (gen.Ticket, error) {
	var t gen.Ticket
	err := c.tx(ctx, func(q *gen.Queries) error {
		p, err := q.GetProject(ctx, projectID)
		if err != nil {
			return notFound(err, "get project")
		}
		if p.ArchivedAt.Valid {
			return invalid("project", "Restore this project to add tickets.")
		}
		tf, err := checkTicket(ctx, q, p, "", in)
		if err != nil {
			return err
		}
		tags, err := c.upsertTags(ctx, q, tf.Tags)
		if err != nil {
			return err
		}
		num, err := q.NextTicketNumber(ctx, p.ID)
		if err != nil {
			return fmt.Errorf("next number: %w", err)
		}
		pos, err := c.nextPosition(ctx, q, tf.StatusID)
		if err != nil {
			return err
		}
		now := timefmt.Format(c.Now())
		t, err = q.CreateTicket(ctx, gen.CreateTicketParams{
			ID: idgen.New(), ProjectID: p.ID, Number: num, Title: tf.Title, Body: tf.Body,
			StatusID: tf.StatusID, Priority: tf.Priority, AssigneeID: nullString(tf.AssigneeID),
			ParentID: nullString(tf.ParentID), MilestoneID: nullString(tf.MilestoneID),
			Position: pos, CreatedBy: by, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("create ticket: %w", err)
		}
		for _, tag := range tags {
			if err := q.AddTicketTag(ctx, gen.AddTicketTagParams{TicketID: t.ID, TagID: tag.ID}); err != nil {
				return fmt.Errorf("tag ticket: %w", err)
			}
		}
		return c.emit(ctx, q, by, EvTicketCreated, p.ID, t.ID, map[string]string{"title": t.Title})
	})
	return t, err
}

// UpdateTicket replaces every editable field of a ticket, its tag set
// included. A ticket whose status changes moves to the end of the new
// status's column. An update that changes nothing writes nothing.
func (c *Core) UpdateTicket(ctx context.Context, by, ticketID string, in TicketInput) (gen.Ticket, error) {
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
		pos := t.Position
		if tf.StatusID != t.StatusID {
			if pos, err = c.nextPosition(ctx, q, tf.StatusID); err != nil {
				return err
			}
		}
		now := timefmt.Format(c.Now())
		if err := q.UpdateTicket(ctx, gen.UpdateTicketParams{
			Title: tf.Title, Body: tf.Body, StatusID: tf.StatusID, Priority: tf.Priority,
			AssigneeID: nullString(tf.AssigneeID), ParentID: nullString(tf.ParentID),
			MilestoneID: nullString(tf.MilestoneID), Position: pos, UpdatedAt: now, ID: t.ID,
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

// ticketView is a stored ticket in the shape checkTicket returns, so the
// two can be compared field by field.
func (c *Core) ticketView(ctx context.Context, q *gen.Queries, p gen.Project, t gen.Ticket) (ticketFields, error) {
	in := TicketInput{
		Title: t.Title, Body: t.Body, StatusID: t.StatusID, Priority: t.Priority,
		AssigneeID: t.AssigneeID.String, ParentID: t.ParentID.String, MilestoneID: t.MilestoneID.String,
	}
	tf, err := checkTicket(ctx, q, p, t.ID, in)
	if _, ok := errors.AsType[*Invalid](err); ok {
		err = nil // a stored ticket that no longer validates still diffs
	}
	if err != nil {
		return tf, err
	}
	if tf.tags, err = q.ListTicketTags(ctx, t.ID); err != nil {
		return tf, fmt.Errorf("ticket tags: %w", err)
	}
	return tf, nil
}

// diffTicket lists the fields that differ, as event payload changes.
func diffTicket(old, cur ticketFields) map[string]Change {
	ch := map[string]Change{}
	if old.Title != cur.Title {
		ch["title"] = Change{old.Title, cur.Title}
	}
	if old.Body != cur.Body {
		ch["body"] = Change{}
	}
	if old.StatusID != cur.StatusID {
		ch["status"] = Change{old.status.Name, cur.status.Name}
	}
	if old.Priority != cur.Priority {
		ch["priority"] = Change{old.Priority, cur.Priority}
	}
	if old.AssigneeID != cur.AssigneeID {
		ch["assignee"] = Change{old.assignee, cur.assignee}
	}
	if old.ParentID != cur.ParentID {
		ch["parent"] = Change{old.parentKey, cur.parentKey}
	}
	if old.MilestoneID != cur.MilestoneID {
		ch["milestone"] = Change{old.milestone, cur.milestone}
	}
	if from, to := tagNames(old.tags), tagNames(cur.tags); !slices.Equal(from, to) {
		ch["tags"] = Change{from, to}
	}
	return ch
}

// replaceTags makes the ticket's tag set exactly cur.
func (c *Core) replaceTags(ctx context.Context, q *gen.Queries, ticketID string, old, cur []gen.Tag) error {
	has := func(set []gen.Tag, id string) bool {
		return slices.ContainsFunc(set, func(t gen.Tag) bool { return t.ID == id })
	}
	for _, t := range old {
		if !has(cur, t.ID) {
			if err := q.RemoveTicketTag(ctx, gen.RemoveTicketTagParams{TicketID: ticketID, TagID: t.ID}); err != nil {
				return fmt.Errorf("untag ticket: %w", err)
			}
		}
	}
	for _, t := range cur {
		if !has(old, t.ID) {
			if err := q.AddTicketTag(ctx, gen.AddTicketTagParams{TicketID: ticketID, TagID: t.ID}); err != nil {
				return fmt.Errorf("tag ticket: %w", err)
			}
		}
	}
	return nil
}

// ResolveTicket finds a ticket by key through q (normally the reader).
func (c *Core) ResolveTicket(ctx context.Context, q *gen.Queries, key TicketKey) (gen.Ticket, gen.Project, error) {
	p, err := q.GetProjectByKey(ctx, key.Project)
	if err != nil {
		return gen.Ticket{}, gen.Project{}, notFound(err, "get project")
	}
	t, err := q.GetTicketByNumber(ctx, gen.GetTicketByNumberParams{ProjectID: p.ID, Number: key.Number})
	if err != nil {
		return gen.Ticket{}, p, notFound(err, "get ticket")
	}
	return t, p, nil
}
