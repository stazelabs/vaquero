// Package board builds the board and list views of a project: its tickets
// grouped into status columns, narrowed by a Filter, with each card's
// readiness and the milestone panel's progress.
package board

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// DoneWindow is how far back the board's done columns reach.
const DoneWindow = 7 * 24 * time.Hour

// Card is one ticket on the board or in the list.
type Card struct {
	ID                     string // the ticket's id, for the Focus view's reasons
	Key, Title, Assignee   string
	AssigneeIsAgent, Ready bool
	Priority, Version      int64
	Tags                   []string
	// ClaimedBy is the claimant's name while a claim is held (it may have
	// lapsed and not yet been swept); ClaimExpiresAt its lease's end, as stored.
	ClaimedBy, ClaimExpiresAt string
	ClaimantIsAgent           bool
	// With the project's agent workflow on: Tier is the ticket's tier,
	// Readiness the workflow's gate for a todo ticket (nil otherwise), and
	// Ready needs it too. With it off, both are empty and Ready is the
	// board's rule alone.
	Tier      string
	Readiness *readiness.Readiness
	// StatusChangedAt is when the ticket last changed status, as stored;
	// When the Focus view's day for a finished card ("today", "Mon").
	StatusChangedAt, When string
}

// Column is one status and its cards, top to bottom.
type Column struct {
	Status gen.Status
	Cards  []Card
	Older  int // done columns on the board: cards hidden as older than DoneWindow
}

// MilestoneProgress is one open milestone in the panel.
type MilestoneProgress struct {
	ID, Name, Due string
	Done, Total   int64
}

// Board is a project's board.
type Board struct {
	Project     gen.Project
	Filter      Filter
	Active      []Column // todo, in progress, in review and done statuses, in order
	Rails       []Column // backlog and cancelled statuses, in order
	Milestones  []MilestoneProgress
	NoMilestone int64 // open tickets without a milestone
}

func isRail(category string) bool { return category == "backlog" || category == "cancelled" }

// Load builds the board as of now. The board has no category filter (its
// columns are the categories), so f.Category is ignored and dropped.
func Load(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter, now time.Time) (Board, error) {
	f.Category = ""
	cols, err := columns(ctx, q, p, owner, f, now)
	if err != nil {
		return Board{}, err
	}
	b := Board{Project: p, Filter: f}
	for _, c := range cols {
		if isRail(c.Status.Category) {
			b.Rails = append(b.Rails, c)
		} else {
			b.Active = append(b.Active, c)
		}
	}
	progress, err := q.ListMilestoneProgress(ctx, p.ID)
	if err != nil {
		return b, err
	}
	for _, m := range progress {
		if m.ClosedAt.Valid {
			continue
		}
		mp := MilestoneProgress{ID: m.ID, Name: m.Name, Done: m.Done, Total: m.Total}
		if m.DueOn.Valid {
			mp.Due = timefmt.Day(m.DueOn.String)
		}
		b.Milestones = append(b.Milestones, mp)
	}
	if b.NoMilestone, err = q.CountOpenWithoutMilestone(ctx, p.ID); err != nil {
		return b, err
	}
	return b, nil
}

// LoadList is every status column with every matching card: nothing folded
// or hidden by age.
func LoadList(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter) ([]Column, error) {
	return columns(ctx, q, p, owner, f, time.Time{})
}

// columns groups the filtered tickets by status, keeping only the filter's
// category's statuses when it names one. A non-zero now hides done
// cards older than DoneWindow, counting them in Older.
func columns(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter, now time.Time) ([]Column, error) {
	statuses, err := q.ListStatuses(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	pr := params(p, owner, f)
	if f.Tag != "" {
		// The filter may be spelled in another case or normalisation than
		// the stored tag (SQLite folds only ASCII), so use the stored name.
		known, err := q.ListTags(ctx)
		if err != nil {
			return nil, err
		}
		if i := slices.IndexFunc(known, func(k gen.Tag) bool { return core.SameTag(k.Name, f.Tag) }); i >= 0 {
			pr.Tag = sql.NullString{String: known[i].Name, Valid: true}
		}
	}
	rows, err := q.ListBoardTickets(ctx, pr)
	if err != nil {
		return nil, err
	}
	tagRows, err := q.ListProjectTicketTags(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	tags := map[string][]string{}
	for _, r := range tagRows {
		tags[r.TicketID] = append(tags[r.TicketID], r.Name)
	}
	if f.Category != "" {
		statuses = slices.DeleteFunc(statuses, func(s gen.Status) bool { return s.Category != f.Category })
	}
	cols := make([]Column, len(statuses))
	index := map[string]int{}
	for i, s := range statuses {
		cols[i].Status = s
		index[s.ID] = i
	}
	cutoff := now.Add(-DoneWindow)
	workflow := p.Workflow == 1
	todo := map[string]cardAt{} // with the workflow on: the todo cards, by ticket id
	for _, r := range rows {
		i, ok := index[r.StatusID]
		if !ok {
			continue // another category
		}
		c := &cols[i]
		if !now.IsZero() && r.StatusCategory == "done" {
			if at, err := timefmt.Parse(r.StatusChangedAt); err == nil && at.Before(cutoff) {
				c.Older++
				continue
			}
		}
		c.Cards = append(c.Cards, Card{
			ID:              r.ID,
			StatusChangedAt: r.StatusChangedAt,
			Key:             core.TicketKey{Project: p.Key, Number: r.Number}.String(),
			Title:           r.Title,
			Assignee:        r.AssigneeName,
			AssigneeIsAgent: r.AssigneeKind == "agent",
			Ready:           r.Ready,
			Priority:        r.Priority,
			Version:         r.Version,
			Tags:            tags[r.ID],
			ClaimedBy:       r.ClaimantName,
			ClaimExpiresAt:  r.ClaimExpiresAt,
			ClaimantIsAgent: r.ClaimantKind == "agent",
		})
		if workflow {
			c.Cards[len(c.Cards)-1].Tier = r.Tier
			if r.StatusCategory == "todo" {
				todo[r.ID] = cardAt{i, len(c.Cards) - 1}
			}
		}
	}
	if len(todo) > 0 {
		if err := gate(ctx, q, cols, todo); err != nil {
			return nil, err
		}
	}
	return cols, nil
}

// cardAt is a card's place in the columns.
type cardAt struct{ col, card int }

// gate applies the agent workflow's readiness to the todo cards at, in
// one batch.
func gate(ctx context.Context, q *gen.Queries, cols []Column, at map[string]cardAt) error {
	ids := make([]string, 0, len(at))
	for id := range at {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	got, err := readiness.CheckAll(ctx, q, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		rd, ok := got[id]
		if !ok {
			continue
		}
		k := &cols[at[id].col].Cards[at[id].card]
		k.Readiness = &rd
		k.Ready = k.Ready && rd.Ready
	}
	return nil
}

func params(p gen.Project, owner gen.Actor, f Filter) gen.ListBoardTicketsParams {
	str := func(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
	pr := gen.ListBoardTicketsParams{ProjectID: p.ID, Tag: str(f.Tag), AssigneeID: str(f.Assignee)}
	if f.Mine {
		pr.AssigneeID = str(owner.ID)
	}
	if f.Agents {
		pr.AssigneeKind = str("agent")
	}
	if f.Priority >= 0 {
		pr.Priority = sql.NullInt64{Int64: f.Priority, Valid: true}
	}
	switch f.Milestone {
	case "":
	case "none":
		pr.NoMilestone = true
	default:
		pr.MilestoneID = str(f.Milestone)
	}
	return pr
}
