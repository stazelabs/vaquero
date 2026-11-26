package handlers

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/readiness"
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

func projectDTO(p gen.Project, open int64, links []board.ProjectLink) api.Project {
	out := api.Project{Key: p.Key, Name: p.Name, Description: p.Description, Archived: p.ArchivedAt.Valid, OpenCount: open, NextNumber: p.NextNumber,
		Workflow: p.Workflow == 1, TierMap: core.EffectiveTierMap(p.TierMap), Icon: projectIconDTO(p), Links: []api.ProjectLink{}}
	for _, l := range links {
		out.Links = append(out.Links, projectLinkDTO(l))
	}
	return out
}

// repoDTO is p's repository as the API shows it: nil without a repo path.
// A repo that can't be opened now is still shown, not available.
func repoDTO(ctx context.Context, d Deps, p gen.Project) *api.ProjectRepo {
	if p.RepoPath == "" {
		return nil
	}
	out := &api.ProjectRepo{Path: p.RepoPath, MainBranch: p.MainBranch, Remote: p.Remote, FetchMinutes: p.FetchMinutes}
	if d.Git == nil {
		return out
	}
	repo, err := d.Git.Open(ctx, p.RepoPath, p.MainBranch, p.Remote)
	if err != nil {
		return out
	}
	out.Available = true
	if u, err := repo.RemoteURL(ctx); err == nil {
		out.RemoteURL = u
	}
	if at, ok := repo.LastFetch(); ok {
		out.LastFetchAt = new(timefmt.Format(at))
	}
	return out
}

func projectLinkDTO(l board.ProjectLink) api.ProjectLink {
	return api.ProjectLink{ID: l.ID, Kind: l.Kind, Direction: l.Direction, Key: l.Key, Name: l.Name}
}

// projectIconDTO is p's icon as the API shows it.
func projectIconDTO(p gen.Project) *api.ProjectIcon {
	ic := &api.ProjectIcon{Kind: p.IconKind}
	switch p.IconKind {
	case core.IconKindGlyph:
		ic.Glyph = p.IconGlyph
	case core.IconKindImage:
		ic.URL = projectIconURL(p.Key)
	}
	return ic
}

// APIProjects is GET /api/v1/projects.
func APIProjects(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.R.ListProjects(r.Context())
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		links, err := board.LoadProjectLinks(r.Context(), d.R)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Project, len(rows))
		for i, p := range rows {
			out[i] = projectDTO(gen.Project{Key: p.Key, Name: p.Name, Description: p.Description, ArchivedAt: p.ArchivedAt, Workflow: p.Workflow, TierMap: p.TierMap,
				IconKind: p.IconKind, IconGlyph: p.IconGlyph}, p.OpenCount, links[p.ID])
			out[i].Repo = repoDTO(r.Context(), d, gen.Project{RepoPath: p.RepoPath, MainBranch: p.MainBranch, Remote: p.Remote, FetchMinutes: p.FetchMinutes})
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

// writeProject answers p with its open ticket count and its links, as a
// read or after a write.
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
	links, err := board.LoadProjectLinks(r.Context(), d.R)
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	dto := projectDTO(p, open, links[p.ID])
	dto.Repo = repoDTO(r.Context(), d, p)
	api.WriteJSON(w, status, dto)
}

func summary(k board.Card, s gen.Status) api.TicketSummary {
	tags := k.Tags
	if tags == nil {
		tags = []string{}
	}
	return api.TicketSummary{Key: k.Key, Title: k.Title, Status: s.Name, Category: s.Category,
		Priority: k.Priority, Assignee: k.Assignee, Tags: tags, Ready: k.Ready, Version: k.Version,
		ClaimedBy: k.ClaimedBy, ClaimExpiresAt: k.ClaimExpiresAt, Tier: k.Tier, Readiness: apiReadiness(k.Readiness)}
}

// apiReadiness is a readiness answer, nil for none (the workflow off).
func apiReadiness(r *readiness.Readiness) *api.Readiness {
	if r == nil {
		return nil
	}
	return &api.Readiness{Ready: r.Ready, Reasons: r.Reasons}
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
		cols, _, err := board.LoadList(r.Context(), d.R, p, apiActor(r), f)
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
		Body: t.Body, Tier: t.Tier, Model: t.Model, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		SubTickets: []api.TicketSummary{}, Links: []api.Link{}, Comments: []api.Comment{}, Attachments: []api.Attachment{},
	}
	if t.AssigneeID.Valid {
		a, err := d.R.GetActor(ctx, t.AssigneeID.String)
		if err != nil {
			return out, err
		}
		out.Assignee = a.Name
	}
	former, err := d.R.ListTicketOldKeys(ctx, t.ID)
	if err != nil {
		return out, err
	}
	for _, k := range former {
		out.FormerKeys = append(out.FormerKeys, core.TicketKey{Project: k.ProjectKey, Number: k.Number}.String())
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
	// With the workflow on, the ticket (always) and its todo sub-tickets
	// carry the readiness gate, in one batch, and ready needs it.
	var gate map[string]readiness.Readiness
	if p.Workflow == 1 {
		ids := []string{t.ID}
		for _, s := range subs {
			if s.StatusCategory == "todo" {
				ids = append(ids, s.ID)
			}
		}
		if gate, err = readiness.CheckAll(ctx, d.R, ids); err != nil {
			return out, err
		}
		rd := gate[t.ID]
		out.Readiness = apiReadiness(&rd)
		out.Ready = out.Ready && rd.Ready
	}
	subTags := map[string][]string{}
	if len(subs) > 0 {
		ids := make([]string, len(subs))
		for i, s := range subs {
			ids[i] = s.ID
		}
		pairs, err := d.R.ListTagsForTickets(ctx, ids)
		if err != nil {
			return out, err
		}
		for _, g := range pairs {
			subTags[g.TicketID] = append(subTags[g.TicketID], g.Name)
		}
	}
	for _, s := range subs {
		names := subTags[s.ID]
		if names == nil {
			names = []string{}
		}
		sum := api.TicketSummary{Key: ticketKey(p, s.Number), Title: s.Title, Status: s.StatusName,
			Category: s.StatusCategory, Priority: s.Priority, Assignee: s.AssigneeName, Tags: names, Ready: s.Ready, Version: s.Version,
			ClaimedBy: s.ClaimantName, ClaimExpiresAt: s.ClaimExpiresAt}
		if p.Workflow == 1 {
			sum.Tier = s.Tier
			if rd, ok := gate[s.ID]; ok {
				sum.Readiness = apiReadiness(&rd)
				sum.Ready = sum.Ready && rd.Ready
			}
		}
		out.SubTickets = append(out.SubTickets, sum)
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
		res, err := search.Run(r.Context(), d.RDB, q, limitParam(r, 50, maxAPIList))
		hits := res.Hits
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
		// Vaquero-Total counts every match, past the limit (VAQ-133); the
		// body stays the bare array.
		w.Header().Set("Vaquero-Total", strconv.Itoa(res.Total))
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// APIEvents is GET /api/v1/events?after={seq}&limit=: events after a
// sequence number, oldest first; poll with the last seq seen. newest=N
// instead gives the newest N, still oldest first.
func APIEvents(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Vaquero-Seq is the newest sequence, read before the list: an
		// empty answer to after=SEQ means nothing listable lies between
		// SEQ and it (reorders advance seq but aren't listed), so a poller
		// can continue from it without missing an event.
		newestSeq, err := d.R.MaxEventSeq(r.Context())
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		var rows []gen.ListEventsAfterRow
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
		views := make([]templates.EventView, len(rows))
		for i, e := range rows {
			views[i] = templates.EventView{Actor: e.ActorName, Kind: e.Kind, ProjectKey: e.ProjectKey, TicketTitle: e.TicketTitle,
				Payload: templates.ParsePayload(e.Payload)}
			if e.TicketNumber > 0 {
				views[i].TicketKey = core.TicketKey{Project: e.TicketProjectKey, Number: e.TicketNumber}.String()
			}
		}
		if err := resolveLinkTargets(r.Context(), d, views); err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		out := make([]api.Event, len(rows))
		for i, e := range rows {
			ev := views[i]
			at := e.At
			if parsed, err := timefmt.Parse(e.At); err == nil {
				at = timefmt.Format(parsed)
			}
			out[i] = api.Event{Seq: e.Seq, At: at, Actor: e.ActorName, Kind: e.Kind, Project: e.ProjectKey,
				Ticket: ev.TicketKey, Text: templates.EventSentence(ev), Payload: apiPayload(ev.Payload), Meta: eventMeta(e.Meta)}
		}
		w.Header().Set("Vaquero-Seq", strconv.FormatInt(newestSeq, 10))
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// eventMeta is an event's stored meta, nil when it has none (or, never
// expected, isn't an object of strings).
func eventMeta(raw string) map[string]string {
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
}

// apiPayload is an event's payload as the API shows it: a description
// edit's stored text (body_before, body_after) stays on the server, for the
// ticket history's diff, and "body_diff": true says it was kept.
func apiPayload(p map[string]any) map[string]any {
	_, before := p["body_before"]
	_, after := p["body_after"]
	if !before && !after {
		return p
	}
	out := maps.Clone(p)
	delete(out, "body_before")
	delete(out, "body_after")
	out["body_diff"] = true
	return out
}

// orList joins words as "a, b or c".
func orList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}
