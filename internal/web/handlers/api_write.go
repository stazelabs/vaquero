package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// apiCoreFail answers a failed core call; a conflict carries the ticket as
// it is now.
func apiCoreFail(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, err error) {
	f := api.FromCore(err, func(c *core.Conflict) any {
		cur, derr := ticketDTO(r, d, c.Current, p)
		if derr != nil {
			return nil
		}
		return cur
	})
	apiFail(w, r, d, f, err)
}

// names resolves the names a ticket request may use (status, assignee,
// milestone, parent key) into ids, collecting a field message for each
// unknown one.
type names struct {
	ctx    context.Context
	d      Deps
	p      gen.Project
	fields map[string]string
	err    error // a database error: the answer is a 500, not a field message
}

// lookup records a failed read: a database error as n.err, so it isn't
// reported as a name that matches nothing.
func (n *names) lookup(err error) bool {
	if err != nil && n.err == nil {
		n.err = err
	}
	return err == nil
}

func (n *names) status(name string) string { return n.statusIn("status", name).ID }

// statusIn resolves the status called name in field; the zero status when
// none is.
func (n *names) statusIn(field, name string) gen.Status {
	ss, err := n.d.R.ListStatuses(n.ctx, n.p.ID)
	if !n.lookup(err) {
		return gen.Status{}
	}
	for _, s := range ss {
		if strings.EqualFold(s.Name, strings.TrimSpace(name)) {
			return s
		}
	}
	n.fields[field] = "No status " + name + " in " + n.p.Key + "."
	return gen.Status{}
}

func (n *names) assignee(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	as, err := n.d.R.ListActors(n.ctx)
	if !n.lookup(err) {
		return ""
	}
	for _, a := range as {
		if strings.EqualFold(a.Name, strings.TrimSpace(name)) {
			return a.ID
		}
	}
	n.fields["assignee"] = "No one is called " + name + "."
	return ""
}

func (n *names) milestone(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return n.milestoneRow(name).ID
}

// milestoneRow resolves the milestone called name; the zero milestone when
// none is.
func (n *names) milestoneRow(name string) gen.Milestone {
	ms, err := n.d.R.ListMilestones(n.ctx, n.p.ID)
	if !n.lookup(err) {
		return gen.Milestone{}
	}
	for _, m := range ms {
		if strings.EqualFold(m.Name, strings.TrimSpace(name)) {
			return m
		}
	}
	n.fields["milestone"] = "No milestone " + name + " in " + n.p.Key + "."
	return gen.Milestone{}
}

func (n *names) parent(key string) string { return n.ticket("parent", key) }

// ticket resolves the ticket key in field ("" is none).
func (n *names) ticket(field, key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	k, ok := core.ParseTicketKey(key)
	if !ok {
		n.fields[field] = "That isn't a ticket key."
		return ""
	}
	t, _, err := n.d.Core.ResolveTicket(n.ctx, n.d.R, k)
	if errors.Is(err, core.ErrNotFound) {
		n.fields[field] = "No ticket " + k.String() + "."
		return ""
	}
	if !n.lookup(err) {
		return ""
	}
	return t.ID
}

// failure is the lookups' failure, if any: a database error as a 500
// (n.err is its cause), names that match nothing as a 422 with a message
// per field.
func (n *names) failure() *api.Failure {
	if n.err != nil {
		return api.Internal()
	}
	if len(n.fields) == 0 {
		return nil
	}
	return invalidFields(n.fields, "Some names don't match anything.")
}

// invalidFields is a 422 naming fields: the one message when there is
// one, else several.
func invalidFields(fields map[string]string, several string) *api.Failure {
	msg := several
	if len(fields) == 1 {
		for _, m := range fields {
			msg = m
		}
	}
	f := api.Fail(http.StatusUnprocessableEntity, "invalid", msg)
	f.Fields = fields
	return f
}

// fail answers the lookups' failure, if any.
func (n *names) fail(w http.ResponseWriter, r *http.Request) bool {
	f := n.failure()
	if f == nil {
		return false
	}
	apiFail(w, r, n.d, f, n.err)
	return true
}

// APIProjectCreate is POST /api/v1/projects {key, name, description}.
func APIProjectCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Key, Name, Description string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		p, err := d.Core.CreateProject(r.Context(), apiActor(r).ID, core.ProjectInput{Key: strings.ToUpper(strings.TrimSpace(in.Key)), Name: in.Name, Description: in.Description})
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, projectDTO(p, 0))
	}
}

// APIProjectUpdate is PATCH /api/v1/projects/{key} {name?, description?}.
func APIProjectUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in struct{ Name, Description *string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		name, desc := p.Name, p.Description
		if in.Name != nil {
			name = *in.Name
		}
		if in.Description != nil {
			desc = *in.Description
		}
		np, err := d.Core.UpdateProject(r.Context(), apiActor(r).ID, p.ID, name, desc)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeProject(w, r, d, np, http.StatusOK)
	}
}

// APIProjectKey is POST /api/v1/projects/{key}/key {key}.
func APIProjectKey(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in struct{ Key string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		np, err := d.Core.ChangeProjectKey(r.Context(), apiActor(r).ID, p.ID, in.Key)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeProject(w, r, d, np, http.StatusOK)
	}
}

// ticketRequest is a ticket's fields as the API names them. For a create,
// an absent field takes its default; for a patch, an absent field is kept.
type ticketRequest struct {
	Title      *string   `json:"title"`
	Body       *string   `json:"body"`
	Status     *string   `json:"status"`
	Priority   *int64    `json:"priority"`
	Assignee   *string   `json:"assignee"`
	Parent     *string   `json:"parent"`
	Milestone  *string   `json:"milestone"`
	Tags       *[]string `json:"tags"`
	AddTags    []string  `json:"add_tags"`
	RemoveTags []string  `json:"remove_tags"`
	Version    int64     `json:"version"`
}

// patch resolves the request's names into a core.TicketPatch.
func (in ticketRequest) patch(n *names) core.TicketPatch {
	p := core.TicketPatch{Title: in.Title, Body: in.Body, Priority: in.Priority, AddTags: in.AddTags, RemoveTags: in.RemoveTags}
	resolve := func(v *string, fn func(string) string) *string {
		if v == nil {
			return nil
		}
		id := fn(*v)
		return &id
	}
	p.StatusID = resolve(in.Status, n.status)
	p.AssigneeID = resolve(in.Assignee, n.assignee)
	p.ParentID = resolve(in.Parent, n.parent)
	p.MilestoneID = resolve(in.Milestone, n.milestone)
	if in.Tags != nil {
		p.Tags, p.SetTags = *in.Tags, true
	}
	return p
}

// APITicketCreate is POST /api/v1/projects/{key}/tickets.
func APITicketCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := apiProject(w, r, d)
		if !ok {
			return
		}
		var in ticketRequest
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		pt := in.patch(n)
		// A patch's fields that mean nothing for a new ticket are refused,
		// not ignored.
		if in.Version != 0 {
			n.fields["version"] = "A new ticket has no version to check."
		}
		if len(in.RemoveTags) > 0 {
			n.fields["remove_tags"] = "A new ticket has no tags to remove."
		}
		if n.fail(w, r) {
			return
		}
		input := core.TicketInput{}
		deref := func(v *string) string {
			if v == nil {
				return ""
			}
			return *v
		}
		input.Title, input.Body = deref(pt.Title), deref(pt.Body)
		input.StatusID, input.AssigneeID = deref(pt.StatusID), deref(pt.AssigneeID)
		input.ParentID, input.MilestoneID = deref(pt.ParentID), deref(pt.MilestoneID)
		if pt.Priority != nil {
			input.Priority = *pt.Priority
		}
		input.Tags = append(pt.Tags, pt.AddTags...)
		t, err := d.Core.CreateTicket(r.Context(), apiActor(r).ID, p.ID, input)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		out, err := ticketDTO(r, d, t, p)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, out)
	}
}

// APITicketPatch is PATCH /api/v1/tickets/{ticket}: only the fields sent
// change; with version, a stale save is refused with 409 and the current
// ticket. ?view=summary answers with the short answer.
func APITicketPatch(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		short, ok := viewParam(w, r)
		if !ok {
			return
		}
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in ticketRequest
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		pt := in.patch(n)
		if n.fail(w, r) {
			return
		}
		nt, err := d.Core.PatchTicket(r.Context(), apiActor(r).ID, t.ID, pt, in.Version)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeTicketView(w, r, d, nt, p, http.StatusOK, short)
	}
}

// APITicketMove is POST /api/v1/tickets/{ticket}/move {status, before?,
// after?}; ?view=summary answers with the short answer.
func APITicketMove(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		short, ok := viewParam(w, r)
		if !ok {
			return
		}
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in struct{ Status, Before, After string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		n := &names{ctx: r.Context(), d: d, p: p, fields: map[string]string{}}
		status := n.status(in.Status)
		before, after := n.ticket("before", in.Before), n.ticket("after", in.After)
		if n.fail(w, r) {
			return
		}
		nt, err := d.Core.MoveTicket(r.Context(), apiActor(r).ID, t.ID, status, before, after)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeTicketView(w, r, d, nt, p, http.StatusOK, short)
	}
}

func commentDTO(c gen.Comment, author string) api.Comment {
	return api.Comment{ID: c.ID, Author: author, Body: c.Body, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

// APICommentCreate is POST /api/v1/tickets/{ticket}/comments {body};
// ?view=summary answers with the ticket's short answer and the comment's id.
func APICommentCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		short, ok := viewParam(w, r)
		if !ok {
			return
		}
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in struct{ Body string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		a := apiActor(r)
		c, err := d.Core.AddComment(r.Context(), a.ID, t.ID, in.Body)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		if short {
			b, err := ticketBrief(r, d, t, p)
			if err != nil {
				apiFail(w, r, d, api.Internal(), err)
				return
			}
			b.ID = c.ID
			api.WriteJSON(w, http.StatusCreated, b)
			return
		}
		writeCommentAnswer(w, r, d, t, c, a.Name, http.StatusCreated)
	}
}

// apiComment loads the {id} comment.
func apiComment(w http.ResponseWriter, r *http.Request, d Deps) (gen.Comment, bool) {
	c, err := d.R.GetComment(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) || err == nil && c.DeletedAt.Valid {
		api.WriteFailure(w, api.NotFound("No such comment."))
		return c, false
	}
	if err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return c, false
	}
	return c, true
}

// APICommentUpdate is PATCH /api/v1/comments/{id} {body}.
func APICommentUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := apiComment(w, r, d)
		if !ok {
			return
		}
		var in struct{ Body string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		a := apiActor(r)
		if err := d.Core.EditComment(r.Context(), a.ID, c.ID, in.Body); err != nil {
			apiCoreFail(w, r, d, gen.Project{}, err)
			return
		}
		nc, err := d.R.GetComment(r.Context(), c.ID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		t, err := d.R.GetTicket(r.Context(), nc.TicketID)
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		writeCommentAnswer(w, r, d, t, nc, a.Name, http.StatusOK)
	}
}

// writeCommentAnswer answers a comment write with the comment, the claim
// on its ticket t (which a comment leaves alone) and, when someone else
// holds that claim, its warning.
func writeCommentAnswer(w http.ResponseWriter, r *http.Request, d Deps, t gen.Ticket, c gen.Comment, author string, status int) {
	out := commentDTO(c, author)
	var err error
	if out.ClaimedBy, out.ClaimExpiresAt, out.Warning, err = ticketClaim(r, d, t); err != nil {
		apiFail(w, r, d, api.Internal(), err)
		return
	}
	api.WriteJSON(w, status, out)
}

// APICommentDelete is DELETE /api/v1/comments/{id}.
func APICommentDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := apiComment(w, r, d)
		if !ok {
			return
		}
		if err := d.Core.DeleteComment(r.Context(), apiActor(r).ID, c.ID); err != nil {
			apiCoreFail(w, r, d, gen.Project{}, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// APILinkCreate is POST /api/v1/tickets/{ticket}/links {kind, to}.
func APILinkCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var in struct{ Kind, To string }
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		k, parsed := core.ParseTicketKey(in.To)
		if !parsed {
			f := api.Fail(http.StatusUnprocessableEntity, "invalid", "That isn't a ticket key.")
			f.Fields = map[string]string{"to": "That isn't a ticket key."}
			api.WriteFailure(w, f)
			return
		}
		l, err := d.Core.AddLink(r.Context(), apiActor(r).ID, t.ID, in.Kind, k)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		other, _, _ := d.Core.ResolveTicket(r.Context(), d.R, k)
		out := api.Link{ID: l.ID, Kind: l.Kind, Direction: "out", Key: k.String(), Title: other.Title}
		if _, _, out.Warning, err = ticketClaim(r, d, t); err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, out)
	}
}

// APILinkDelete is DELETE /api/v1/links/{id}.
func APILinkDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Core.RemoveLink(r.Context(), apiActor(r).ID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, core.ErrNotFound) {
				api.WriteFailure(w, api.NotFound("No such link."))
				return
			}
			apiCoreFail(w, r, d, gen.Project{}, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
