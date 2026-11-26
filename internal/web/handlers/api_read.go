package handlers

import (
	"cmp"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/search"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/web/middleware"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// maxAPIList bounds a list answer.
const maxAPIList = 500

// apiFail logs a 500's cause and writes f.
func apiFail(w http.ResponseWriter, r *http.Request, d Deps, f *api.Failure, cause error) {
	if f.Status >= 500 && cause != nil {
		d.Logger.ErrorContext(r.Context(), "api error", "err", cause, "path", middleware.RedactPath(r.URL.Path),
			"request_id", middleware.GetRequestID(r.Context()))
	}
	api.WriteFailure(w, f)
}

// limitParam reads ?limit=, defaulting to and capped at max.
func limitParam(r *http.Request, def, max int64) int64 {
	n, err := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	return min(n, max)
}

// apiProject loads the {key} project, any case.
func apiProject(w http.ResponseWriter, r *http.Request, d Deps) (gen.Project, bool) {
	key := strings.ToUpper(chi.URLParam(r, "key"))
	p, err := d.R.GetProjectByKey(r.Context(), key)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteFailure(w, api.NotFound("No project "+key+"."))
		return p, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return p, false
	}
	return p, true
}

// apiTicket loads the {ticket} ticket.
func apiTicket(w http.ResponseWriter, r *http.Request, d Deps) (gen.Ticket, gen.Project, bool) {
	raw := chi.URLParam(r, "ticket")
	k, ok := core.ParseTicketKey(raw)
	if !ok {
		api.WriteFailure(w, api.NotFound("That isn't a ticket key."))
		return gen.Ticket{}, gen.Project{}, false
	}
	t, p, err := d.Core.ResolveTicket(r.Context(), d.R, k)
	if errors.Is(err, core.ErrNotFound) {
		api.WriteFailure(w, api.NotFound("No ticket "+k.String()+"."))
		return t, p, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return t, p, false
	}
	return t, p, true
}

func projectDTO(p gen.Project, open int64) api.Project {
	return api.Project{Key: p.Key, Name: p.Name, Description: p.Description, Archived: p.ArchivedAt.Valid, OpenCount: open}
}

// APIProjects is GET /api/v1/projects.
func APIProjects(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListProjects(r.Context())
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Project, len(rows))
		for i, p := range rows {
			out[i] = api.Project{Key: p.Key, Name: p.Name, Description: p.Description, Archived: p.ArchivedAt.Valid, OpenCount: p.OpenCount}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIProject is GET /api/v1/projects/{key}.
func APIProject(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		writeProject(w, r, d, p, http.StatusOK)
	}
}

// writeProject answers p with its open ticket count, as a read or after a
// write.
func writeProject(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, status int) {
	rows, err := d.R.ListProjects(r.Context())
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	var open int64
	for _, row := range rows {
		if row.ID == p.ID {
			open = row.OpenCount
		}
	}
	api.WriteJSON(w, status, projectDTO(p, open))
}

// APIMilestones is GET /api/v1/projects/{key}/milestones.
func APIMilestones(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		ms, err := d.R.ListMilestones(r.Context(), p.ID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		progress, err := d.R.ListMilestoneProgress(r.Context(), p.ID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		counts := map[string][2]int64{}
		for _, m := range progress {
			counts[m.ID] = [2]int64{m.Done, m.Total}
		}
		out := make([]api.Milestone, len(ms))
		for i, m := range ms {
			c := counts[m.ID]
			out[i] = api.Milestone{Name: m.Name, Description: m.Description, DueOn: m.DueOn.String, Closed: m.ClosedAt.Valid, Done: c[0], Total: c[1]}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

func summary(k board.Card, s gen.Status) api.TicketSummary {
	tags := k.Tags
	if tags == nil {
		tags = []string{}
	}
	return api.TicketSummary{Key: k.Key, Title: k.Title, Status: s.Name, Category: s.Category,
		Priority: k.Priority, Assignee: k.Assignee, Tags: tags, Ready: k.Ready, Version: k.Version,
		ClaimedBy: k.ClaimedBy, ClaimExpiresAt: k.ClaimExpiresAt}
}

// APITickets is GET /api/v1/projects/{key}/tickets: the board's filters,
// plus status (a status name, any case), open=1 (not done or cancelled)
// and limit (≤ 500), in board order. An unknown milestone, assignee or
// category is a 422.
func APITickets(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		q := r.URL.Query()
		// The API takes milestone and assignee by name; the board filter
		// wants ids.
		f := board.ParseFilter(q)
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		if f.Milestone != "" && f.Milestone != "none" {
			f.Milestone = n.milestone(f.Milestone)
		}
		if f.Assignee != "" {
			f.Assignee = n.assignee(f.Assignee)
		}
		// The web filter reads an unknown category as any; the API refuses it.
		if c := q.Get("category"); c != "" && !slices.Contains(core.Categories, c) {
			n.fields["category"] = "No category " + c + ". Use " + orList(core.Categories) + "."
		}
		if n.fail(w, r) {
			return
		}
		cols, err := board.LoadList(r.Context(), d.R, p, apiActor(r), f)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		status, open := strings.TrimSpace(q.Get("status")), q.Get("open") == "1" || q.Get("open") == "true"
		limit := limitParam(r, maxAPIList, maxAPIList)
		out := []api.TicketSummary{}
		for _, c := range cols {
			if status != "" && !strings.EqualFold(c.Status.Name, status) {
				continue
			}
			if open && isDone(c.Status.Category) {
				continue
			}
			for _, k := range c.Cards {
				if int64(len(out)) >= limit {
					break
				}
				out = append(out, summary(k, c.Status))
			}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APITicket is GET /api/v1/tickets/{ticket}: the ticket with its tags,
// sub-tickets, links and comments.
func APITicket(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		out, err := ticketDTO(r, d, t, p)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// ticketDTO is the full API view of a ticket.
func ticketDTO(r *http.Request, d Deps, t gen.Ticket, p gen.Project) (api.Ticket, error) {
	ctx := r.Context()
	st, err := d.R.GetStatus(ctx, t.StatusID)
	if err != nil {
		return api.Ticket{}, err
	}
	out := api.Ticket{
		Key: ticketKey(p, t.Number), Title: t.Title, Status: st.Name, Category: st.Category,
		Priority: t.Priority, Tags: []string{}, Version: t.Version,
		Body: t.Body, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		SubTickets: []api.TicketSummary{}, Links: []api.Link{}, Comments: []api.Comment{}, Attachments: []api.Attachment{},
	}
	if t.AssigneeID.Valid {
		a, err := d.R.GetActor(ctx, t.AssigneeID.String)
		if err != nil {
			return out, err
		}
		out.Assignee = a.Name
	}
	if t.MilestoneID.Valid {
		m, err := d.R.GetMilestone(ctx, t.MilestoneID.String)
		if err != nil {
			return out, err
		}
		out.Milestone = m.Name
	}
	if t.ParentID.Valid {
		parent, err := d.R.GetTicket(ctx, t.ParentID.String)
		if err != nil {
			return out, err
		}
		out.Parent = ticketKey(p, parent.Number)
	}
	tags, err := d.R.ListTicketTags(ctx, t.ID)
	if err != nil {
		return out, err
	}
	for _, g := range tags {
		out.Tags = append(out.Tags, g.Name)
	}
	// Ready and the claim match the board: read them from the board
	// query's row, narrowed to this ticket; the sub-tickets are its rows
	// too, so they are the same summaries as the list's.
	rows, err := d.R.ListBoardTickets(ctx, gen.ListBoardTicketsParams{ProjectID: p.ID, TicketID: sqlNull(t.ID)})
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		out.Ready = row.Ready
		out.ClaimedBy, out.ClaimExpiresAt = row.ClaimantName, row.ClaimExpiresAt
	}
	subs, err := d.R.ListBoardTickets(ctx, gen.ListBoardTicketsParams{ProjectID: p.ID, ParentID: sqlNull(t.ID)})
	if err != nil {
		return out, err
	}
	slices.SortFunc(subs, func(a, b gen.ListBoardTicketsRow) int { return cmp.Compare(a.Number, b.Number) })
	for _, s := range subs {
		subTags, err := d.R.ListTicketTags(ctx, s.ID)
		if err != nil {
			return out, err
		}
		names := make([]string, 0, len(subTags))
		for _, g := range subTags {
			names = append(names, g.Name)
		}
		out.SubTickets = append(out.SubTickets, api.TicketSummary{Key: ticketKey(p, s.Number), Title: s.Title, Status: s.StatusName,
			Category: s.StatusCategory, Priority: s.Priority, Assignee: s.AssigneeName, Tags: names, Ready: s.Ready, Version: s.Version,
			ClaimedBy: s.ClaimantName, ClaimExpiresAt: s.ClaimExpiresAt})
	}
	from, err := d.R.ListLinksFrom(ctx, t.ID)
	if err != nil {
		return out, err
	}
	for _, l := range from {
		out.Links = append(out.Links, api.Link{ID: l.ID, Kind: l.Kind, Direction: "out", Key: core.TicketKey{Project: l.OtherKey, Number: l.OtherNumber}.String(), Title: l.OtherTitle})
	}
	to, err := d.R.ListLinksTo(ctx, t.ID)
	if err != nil {
		return out, err
	}
	for _, l := range to {
		out.Links = append(out.Links, api.Link{ID: l.ID, Kind: l.Kind, Direction: "in", Key: core.TicketKey{Project: l.OtherKey, Number: l.OtherNumber}.String(), Title: l.OtherTitle})
	}
	comments, err := d.R.ListComments(ctx, t.ID)
	if err != nil {
		return out, err
	}
	for _, c := range comments {
		out.Comments = append(out.Comments, api.Comment{ID: c.ID, Author: c.AuthorName, Body: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt})
	}
	if out.Attachments, err = ticketAttachments(r, d, t.ID); err != nil {
		return out, err
	}
	return out, nil
}

// APISearch is GET /api/v1/search?q=.
func APISearch(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := units.Clip(r.URL.Query().Get("q"), maxQuery)
		hits, err := search.Run(r.Context(), d.RDB, q, limitParam(r, 50, maxAPIList))
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.SearchHit, len(hits))
		for i, h := range hits {
			var b strings.Builder
			for _, part := range h.Snippet {
				if part.Mark {
					b.WriteString("[" + part.Text + "]")
				} else {
					b.WriteString(part.Text)
				}
			}
			out[i] = api.SearchHit{Key: h.Key, Title: h.Title, Status: h.Status, Snippet: b.String()}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIEvents is GET /api/v1/events?after={seq}&limit=: events after a
// sequence number, oldest first; poll with the last seq seen. newest=N
// instead gives the newest N, still oldest first.
func APIEvents(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var rows []gen.ListEventsAfterRow
		var err error
		if n, perr := strconv.ParseInt(r.URL.Query().Get("newest"), 10, 64); perr == nil && n > 0 {
			var newest []gen.ListEventsNewestRow
			newest, err = d.R.ListEventsNewest(r.Context(), min(n, maxAPIList))
			for _, n := range slices.Backward(newest) {
				rows = append(rows, gen.ListEventsAfterRow(n))
			}
		} else {
			after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			rows, err = d.R.ListEventsAfter(r.Context(), gen.ListEventsAfterParams{Seq: after, Limit: limitParam(r, 100, maxAPIList)})
		}
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Event, len(rows))
		for i, e := range rows {
			ev := templates.EventView{Actor: e.ActorName, Kind: e.Kind, ProjectKey: e.ProjectKey, TicketTitle: e.TicketTitle,
				Payload: templates.ParsePayload(e.Payload)}
			if e.TicketNumber > 0 {
				ev.TicketKey = core.TicketKey{Project: e.ProjectKey, Number: e.TicketNumber}.String()
			}
			at := e.At
			if parsed, err := timefmt.Parse(e.At); err == nil {
				at = timefmt.Format(parsed)
			}
			out[i] = api.Event{Seq: e.Seq, At: at, Actor: e.ActorName, Kind: e.Kind, Project: e.ProjectKey,
				Ticket: ev.TicketKey, Text: templates.EventSentence(ev), Payload: ev.Payload}
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// orList joins words as "a, b or c".
func orList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}
