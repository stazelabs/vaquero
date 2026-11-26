package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// MovedTicket is one sub-ticket a move took along, in a ticket.moved
// event's payload: its key before and after, and its id.
type MovedTicket struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

// inOpenRun reports whether any of the tickets is in an open run, which
// moving them would pull out from under (ruling N-R2). Runs arrive with
// the agent workflow's part 3; until then no ticket is in one. Part 3
// replaces this with its check, inside the move's transaction.
var inOpenRun = func(context.Context, *gen.Queries, []string) (bool, error) { return false, nil }

// MoveTicketToProject moves a ticket, with its sub-tickets all the way
// down, into the project targetID (VAQ-18). Each takes the target's next
// number, parent first; its old key keeps leading to it (ticket_old_keys,
// read by ResolveTicket). Statuses and milestones are mapped by name (any
// letter case): a status with no namesake takes the target's first status
// of its category, a milestone with none is dropped. The assignee, a claim
// and its lease, tags, comments, links, attachments, the plan and the
// history stay with the ticket. A claim's earlier status is mapped like the
// status, so a lapse returns the ticket to a status of its new project.
//
// The ticket's approvals stay in history but count only in the project
// whose decision made them (readiness, N-R1): its project, key and parent
// changed, so it needs approving again. Open decisions naming it stay in
// the old project; the event counts them (open_decisions), and a plan
// approval among them can no longer approve it (askedUnchanged).
//
// Refused: a sub-ticket on its own ("Move its parent, or detach it
// first."), the ticket's own project, an archived target, a ticket in an
// open run, and with version > 0 a stale version (*Conflict). One
// ticket.moved event on the ticket, in the target project.
func (c *Core) MoveTicketToProject(ctx context.Context, by, ticketID, targetID string, version int64) (gen.Ticket, error) {
	var t gen.Ticket
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if t, err = q.GetTicket(ctx, ticketID); err != nil {
			return notFound(err, "get ticket")
		}
		from, err := q.GetProject(ctx, t.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		key := TicketKey{Project: from.Key, Number: t.Number}.String()
		to, err := q.GetProject(ctx, targetID)
		if err != nil {
			return notFound(err, "get target project")
		}
		if version > 0 && version != t.Version {
			return &Conflict{Key: key, Current: t}
		}
		switch {
		case t.ParentID.Valid:
			return invalid("project", "Move its parent, or detach it first.")
		case to.ID == from.ID:
			return invalid("project", key+" is already in "+to.Key+".")
		case to.ArchivedAt.Valid:
			return invalid("project", "Restore "+to.Key+" to move tickets into it.")
		}
		// Ruling N-R6: out of the agent workflow's gate only by the owner.
		if from.Workflow == 1 && to.Workflow == 0 {
			owner, err := byOwner(ctx, q, by)
			if err != nil {
				return err
			}
			if !owner {
				return invalid("project", "Only the owner moves a ticket out of a project with the agent workflow on.")
			}
		}
		subs, err := q.ListDescendants(ctx, nullString(t.ID))
		if err != nil {
			return fmt.Errorf("list sub-tickets: %w", err)
		}
		all := append([]gen.Ticket{t}, subs...)
		ids := make([]string, len(all))
		for i, s := range all {
			ids[i] = s.ID
		}
		busy, err := inOpenRun(ctx, q, ids)
		if err != nil {
			return err
		}
		if busy {
			return invalid("project", key+" is in an open run; move it once the run ends.")
		}
		m, err := newProjectMap(ctx, q, from.ID, to.ID)
		if err != nil {
			return err
		}
		open, err := q.CountOpenDecisionsNaming(ctx, ids)
		if err != nil {
			return fmt.Errorf("count open decisions: %w", err)
		}
		now := timefmt.Format(c.Now())
		payload := map[string]any{
			"from": key, "from_project": from.Key, "from_project_id": from.ID, "to_project": to.Key,
			"sub_tickets": []MovedTicket{}, "ticket_ids": []string{},
		}
		if open > 0 {
			payload["open_decisions"] = open
		}
		for i, s := range all {
			num, err := q.NextTicketNumber(ctx, to.ID)
			if err != nil {
				return fmt.Errorf("next number: %w", err)
			}
			if err := q.AddTicketOldKey(ctx, gen.AddTicketOldKeyParams{ProjectID: from.ID, Number: s.Number, TicketID: s.ID, MovedAt: now}); err != nil {
				return fmt.Errorf("add old key: %w", err)
			}
			st := m.status(s.StatusID)
			pos, err := c.nextPosition(ctx, q, st.ID)
			if err != nil {
				return err
			}
			statusAt := s.StatusChangedAt
			if st.Name != m.statuses[s.StatusID].Name {
				statusAt = now
			}
			prev := s.ClaimPrevStatusID
			if prev.Valid {
				prev = nullString(m.status(prev.String).ID)
			}
			ms := m.milestone(s.MilestoneID.String)
			if err := q.SetTicketProject(ctx, gen.SetTicketProjectParams{
				ProjectID: to.ID, Number: num, StatusID: st.ID, MilestoneID: nullString(ms.ID), Position: pos,
				ClaimPrevStatusID: prev, StatusChangedAt: statusAt, UpdatedAt: now, ID: s.ID,
			}); err != nil {
				return fmt.Errorf("move ticket: %w", err)
			}
			newKey := TicketKey{Project: to.Key, Number: num}.String()
			if i == 0 {
				payload["to"] = newKey
				changes := map[string]Change{}
				if old := m.statuses[s.StatusID].Name; old != st.Name {
					changes["status"] = Change{old, st.Name}
				}
				if old := m.milestones[s.MilestoneID.String].Name; old != ms.Name {
					changes["milestone"] = Change{old, ms.Name}
				}
				if len(changes) > 0 {
					payload["changes"] = changes
				}
				continue
			}
			payload["sub_tickets"] = append(payload["sub_tickets"].([]MovedTicket), MovedTicket{
				ID: s.ID, From: TicketKey{Project: from.Key, Number: s.Number}.String(), To: newKey,
			})
			payload["ticket_ids"] = append(payload["ticket_ids"].([]string), s.ID)
		}
		if t, err = q.GetTicket(ctx, t.ID); err != nil {
			return fmt.Errorf("reload ticket: %w", err)
		}
		return c.emit(ctx, q, by, EvTicketMoved, to.ID, t.ID, payload)
	})
	return t, err
}

// projectMap maps one project's statuses and milestones onto another's by
// name.
type projectMap struct {
	statuses   map[string]gen.Status    // the old project's, by id
	milestones map[string]gen.Milestone // the old project's, by id
	toStatuses []gen.Status             // the target's, in board order
	toMiles    []gen.Milestone
}

func newProjectMap(ctx context.Context, q *gen.Queries, fromID, toID string) (projectMap, error) {
	m := projectMap{statuses: map[string]gen.Status{}, milestones: map[string]gen.Milestone{}}
	ss, err := q.ListStatuses(ctx, fromID)
	if err != nil {
		return m, fmt.Errorf("list statuses: %w", err)
	}
	for _, s := range ss {
		m.statuses[s.ID] = s
	}
	ms, err := q.ListMilestones(ctx, fromID)
	if err != nil {
		return m, fmt.Errorf("list milestones: %w", err)
	}
	for _, x := range ms {
		m.milestones[x.ID] = x
	}
	if m.toStatuses, err = q.ListStatuses(ctx, toID); err != nil {
		return m, fmt.Errorf("list statuses: %w", err)
	}
	if m.toMiles, err = q.ListMilestones(ctx, toID); err != nil {
		return m, fmt.Errorf("list milestones: %w", err)
	}
	return m, nil
}

// status is the target's status for the old status id: the one of the
// same name, else the first of the same category. Every project keeps a
// status of each category, so there always is one.
func (m projectMap) status(id string) gen.Status {
	old := m.statuses[id]
	for _, s := range m.toStatuses {
		if strings.EqualFold(s.Name, old.Name) {
			return s
		}
	}
	for _, s := range m.toStatuses {
		if s.Category == old.Category {
			return s
		}
	}
	return m.toStatuses[0]
}

// milestone is the target's milestone of the same name as the old one, or
// none.
func (m projectMap) milestone(id string) gen.Milestone {
	if id == "" {
		return gen.Milestone{}
	}
	old := m.milestones[id]
	for _, x := range m.toMiles {
		if strings.EqualFold(x.Name, old.Name) {
			return x
		}
	}
	return gen.Milestone{}
}

// resolveOldKey is the ticket that had key's number in project p before it
// moved away, and the project it is in now.
func resolveOldKey(ctx context.Context, q *gen.Queries, p gen.Project, number int64) (gen.Ticket, gen.Project, error) {
	t, err := q.GetTicketByOldKey(ctx, gen.GetTicketByOldKeyParams{ProjectID: p.ID, Number: number})
	if err != nil {
		return gen.Ticket{}, p, err
	}
	now, err := q.GetProject(ctx, t.ProjectID)
	if err != nil {
		return gen.Ticket{}, p, err
	}
	return t, now, nil
}
