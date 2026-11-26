package handlers

import (
	"net/http"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// activityLimit is how many events the activity page shows.
const activityLimit = 100

// Activity lists the newest changes across every project.
func Activity(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListEvents(r.Context(), activityLimit)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		now := d.Now()
		events := make([]templates.EventView, len(rows))
		for i, e := range rows {
			at, _ := timefmt.Parse(e.At)
			v := templates.EventView{
				Actor: e.ActorName, Kind: e.Kind, ProjectKey: e.ProjectKey, TicketTitle: e.TicketTitle,
				Payload: templates.ParsePayload(e.Payload), Ago: timefmt.Ago(at, now, nil), ActorID: e.ActorID,
			}
			if e.TicketNumber > 0 {
				v.TicketKey = core.TicketKey{Project: e.TicketProjectKey, Number: e.TicketNumber}.String()
			}
			events[i] = v
		}
		if err := resolveLinkTargets(r.Context(), d, events); err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.ActivityPage(events))
	}
}
