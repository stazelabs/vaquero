package board

import (
	"context"
	"time"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// ProjectStats is one project's line on the projects page: its ready, in
// review and open ticket counts, its open milestone (the earliest due,
// else the first open one by name; nil when none is open) and when it
// last changed (its newest event; zero when it has none).
type ProjectStats struct {
	Ready, InReview, Open int
	Milestone             *MilestoneProgress
	LastChange            time.Time
}

// LoadProjectStats reads every project's stats, by project id, in a fixed
// number of queries however many projects there are: one per stat across
// all projects, and one readiness batch for the board-ready tickets of
// projects with the workflow on. Ready is the board's rule, and with the
// workflow on readiness's too, so it counts the cards the board and Focus
// mark ready.
func LoadProjectStats(ctx context.Context, q *gen.Queries) (map[string]ProjectStats, error) {
	rows, err := q.ListProjectStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ProjectStats, len(rows))
	for _, r := range rows {
		s := ProjectStats{InReview: int(r.InReview), Open: int(r.OpenCount)}
		if r.LastChange != "" {
			if at, err := timefmt.Parse(r.LastChange); err == nil {
				s.LastChange = at
			}
		}
		out[r.ProjectID] = s
	}
	ready, err := q.ListBoardReadyTickets(ctx)
	if err != nil {
		return nil, err
	}
	var gated []string // board-ready tickets of workflow projects
	project := map[string]string{}
	for _, r := range ready {
		if r.Workflow == 1 {
			gated = append(gated, r.ID)
			project[r.ID] = r.ProjectID
			continue
		}
		bump(out, r.ProjectID)
	}
	if len(gated) > 0 {
		got, err := readiness.CheckAll(ctx, q, gated)
		if err != nil {
			return nil, err
		}
		for _, id := range gated {
			if rd, ok := got[id]; ok && rd.Ready {
				bump(out, project[id])
			}
		}
	}
	ms, err := q.ListOpenMilestoneProgress(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		s, ok := out[m.ProjectID]
		if !ok || s.Milestone != nil {
			continue // the first in each project's order wins
		}
		mp := MilestoneProgress{ID: m.ID, Name: m.Name, Done: m.Done, Total: m.Total}
		if m.DueOn.Valid {
			mp.Due = timefmt.Day(m.DueOn.String)
		}
		s.Milestone = &mp
		out[m.ProjectID] = s
	}
	return out, nil
}

func bump(out map[string]ProjectStats, projectID string) {
	s := out[projectID]
	s.Ready++
	out[projectID] = s
}
