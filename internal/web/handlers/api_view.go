package handlers

import (
	"fmt"
	"net/http"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// viewParam reads a write's ?view=: "summary" asks for the short answer
// (api.TicketBrief), "" or "full" for the whole ticket. Any other view is
// answered here with a 422, before anything is written.
func viewParam(w http.ResponseWriter, r *http.Request) (summary, ok bool) {
	switch v := r.URL.Query().Get("view"); v {
	case "", "full":
		return false, true
	case "summary":
		return true, true
	default:
		msg := "No view " + v + ". Use full or summary."
		f := api.Fail(http.StatusUnprocessableEntity, "invalid", msg)
		f.Fields = map[string]string{"view": msg}
		api.WriteFailure(w, f)
		return false, false
	}
}

// ticketBrief is t's short answer, read from the board query's row for it
// so the claim matches every other answer.
func ticketBrief(r *http.Request, d Deps, t gen.Ticket, p gen.Project) (api.TicketBrief, error) {
	rows, err := d.R.ListBoardTickets(r.Context(), gen.ListBoardTicketsParams{ProjectID: p.ID, TicketID: sqlNull(t.ID)})
	if err != nil {
		return api.TicketBrief{}, err
	}
	if len(rows) != 1 {
		return api.TicketBrief{}, fmt.Errorf("brief of ticket %s: %d rows", t.ID, len(rows))
	}
	row := rows[0]
	return api.TicketBrief{Key: ticketKey(p, row.Number), Status: row.StatusName, Category: row.StatusCategory, Version: row.Version,
		Assignee: row.AssigneeName, ClaimedBy: row.ClaimantName, ClaimExpiresAt: row.ClaimExpiresAt}, nil
}

// writeTicketView answers a write with t: the short answer when summary,
// else the whole ticket.
func writeTicketView(w http.ResponseWriter, r *http.Request, d Deps, t gen.Ticket, p gen.Project, status int, summary bool) {
	if !summary {
		writeTicket(w, r, d, t, p, status)
		return
	}
	b, err := ticketBrief(r, d, t, p)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	api.WriteJSON(w, status, b)
}
