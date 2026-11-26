package handlers

import (
	"context"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// resolveLinkTargets looks up every link event's target ticket by its
// payload.to_id, all in one read, so each line names the ticket's current
// key (or a deleted ticket). Events with no to_id keep their recorded key.
func resolveLinkTargets(ctx context.Context, d Deps, evs []templates.EventView) error {
	seen := map[string]bool{}
	var ids []string
	for _, e := range evs {
		if id := e.TargetID(); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := d.R.ListTicketKeys(ctx, ids)
	if err != nil {
		return err
	}
	keys := make(map[string]string, len(rows))
	for _, r := range rows {
		keys[r.ID] = core.TicketKey{Project: r.ProjectKey, Number: r.Number}.String()
	}
	templates.ResolveTargets(evs, keys)
	return nil
}
