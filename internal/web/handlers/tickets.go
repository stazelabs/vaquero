package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/markdown"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// ticketFromURL loads the {ticket} ticket. A GET with a non-canonical key
// ("vaq-012") is redirected 301 to the canonical path ("VAQ-12"); a key
// that doesn't parse or doesn't exist goes home with a toast. ok is false
// when the response is already written.
func ticketFromURL(w http.ResponseWriter, r *http.Request, d Deps) (t gen.Ticket, p gen.Project, ok bool) {
	raw := chi.URLParam(r, "ticket")
	k, parsed := core.ParseTicketKey(raw)
	if !parsed {
		redirectWithFlash(w, r, flash.Error, "That isn't a ticket key.", "/")
		return t, p, false
	}
	if raw != k.String() && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		to := strings.Replace(r.URL.Path, "/t/"+raw, "/t/"+k.String(), 1)
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, to, http.StatusMovedPermanently)
		return t, p, false
	}
	t, p, err := d.Core.ResolveTicket(r.Context(), d.R, k)
	if errors.Is(err, core.ErrNotFound) {
		redirectWithFlash(w, r, flash.Error, "No ticket "+k.String()+".", "/")
		return t, p, false
	}
	if err != nil {
		serverError(w, r, d, err)
		return t, p, false
	}
	return t, p, true
}

func ticketKey(p gen.Project, number int64) string {
	return core.TicketKey{Project: p.Key, Number: number}.String()
}

// splitTags reads a comma-separated tag list.
func splitTags(s string) []string { return strings.Split(s, ",") }

// formChoices fills the form's status, assignee and milestone options.
// keepMilestone is listed even when closed, so editing doesn't drop it.
func formChoices(ctx context.Context, d Deps, p gen.Project, f *templates.TicketForm, keepMilestone string) error {
	statuses, err := d.R.ListStatuses(ctx, p.ID)
	if err != nil {
		return err
	}
	f.Statuses = nil
	for _, s := range statuses {
		f.Statuses = append(f.Statuses, templates.Option{Value: s.ID, Label: s.Name})
	}
	actors, err := d.R.ListActors(ctx)
	if err != nil {
		return err
	}
	f.Assignees = []templates.Option{{Value: "", Label: "Unassigned"}}
	for _, a := range actors {
		f.Assignees = append(f.Assignees, templates.Option{Value: a.ID, Label: a.Name})
	}
	milestones, err := d.R.ListMilestones(ctx, p.ID)
	if err != nil {
		return err
	}
	f.Milestones = []templates.Option{{Value: "", Label: "None"}}
	for _, m := range milestones {
		if !m.ClosedAt.Valid || m.ID == keepMilestone {
			f.Milestones = append(f.Milestones, templates.Option{Value: m.ID, Label: m.Name})
		}
	}
	return nil
}

// readTicketForm reads the posted form. The parent is typed as a ticket
// key; a key that doesn't parse or resolve is a field error here, before
// core sees the input.
func readTicketForm(r *http.Request, d Deps, p gen.Project, f *templates.TicketForm) (core.TicketInput, map[string]string) {
	f.Title, f.Body = r.PostFormValue("title"), r.PostFormValue("body")
	f.StatusID, f.Priority = r.PostFormValue("status"), r.PostFormValue("priority")
	f.AssigneeID, f.MilestoneID = r.PostFormValue("assignee"), r.PostFormValue("milestone")
	f.Parent, f.Tags = strings.TrimSpace(r.PostFormValue("parent")), r.PostFormValue("tags")
	in := core.TicketInput{
		Title: f.Title, Body: f.Body, StatusID: f.StatusID,
		AssigneeID: f.AssigneeID, MilestoneID: f.MilestoneID, Tags: splitTags(f.Tags),
	}
	errs := map[string]string{}
	if f.Priority != "" {
		n, err := strconv.ParseInt(f.Priority, 10, 64)
		if err != nil {
			errs["priority"] = "Choose a priority."
		}
		in.Priority = n
	}
	if f.Parent != "" {
		k, ok := core.ParseTicketKey(f.Parent)
		if !ok {
			errs["parent"] = "That isn't a ticket key."
		} else if parent, _, err := d.Core.ResolveTicket(r.Context(), d.R, k); err != nil {
			errs["parent"] = "No ticket " + k.String() + "."
		} else {
			in.ParentID = parent.ID
		}
	}
	return in, errs
}

// formPage renders the ticket form with status.
func formPage(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, f templates.TicketForm, keepMilestone string, status int) {
	templates.LoadEventSeq(r.Context()) // before the page data is read
	if err := formChoices(r.Context(), d, p, &f, keepMilestone); err != nil {
		serverError(w, r, d, err)
		return
	}
	render(w, r, d, status, templates.TicketFormPage(f))
}

// TicketNew is the new-ticket form; ?parent= prefills the parent.
func TicketNew(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := templates.TicketForm{Project: p, Priority: "0", Parent: r.URL.Query().Get("parent")}
		if k, ok := core.ParseTicketKey(f.Parent); ok {
			f.Parent = k.String()
		}
		formPage(w, r, d, p, f, "", http.StatusOK)
	}
}

// TicketCreate makes a ticket.
func TicketCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := templates.TicketForm{Project: p}
		in, errs := readTicketForm(r, d, p, &f)
		if len(errs) > 0 {
			f.Errors = errs
			formPage(w, r, d, p, f, f.MilestoneID, http.StatusUnprocessableEntity)
			return
		}
		t, err := d.Core.CreateTicket(r.Context(), d.Actor.ID, p.ID, in)
		if err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				f.Errors = fields
				formPage(w, r, d, p, f, f.MilestoneID, http.StatusUnprocessableEntity)
			})
			return
		}
		key := ticketKey(p, t.Number)
		redirectWithFlash(w, r, flash.Success, "Ticket "+key+" created.", "/t/"+key)
	}
}

// TicketEdit is the edit form, filled from the ticket.
func TicketEdit(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		ctx := r.Context()
		f := templates.TicketForm{
			Project: p, TicketKey: ticketKey(p, t.Number), Title: t.Title, Body: t.Body,
			StatusID: t.StatusID, Priority: strconv.FormatInt(t.Priority, 10),
			AssigneeID: t.AssigneeID.String, MilestoneID: t.MilestoneID.String,
			Version: strconv.FormatInt(t.Version, 10),
		}
		if t.ParentID.Valid {
			parent, err := d.R.GetTicket(ctx, t.ParentID.String)
			if err != nil {
				serverError(w, r, d, err)
				return
			}
			f.Parent = ticketKey(p, parent.Number)
		}
		tags, err := d.R.ListTicketTags(ctx, t.ID)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		names := make([]string, len(tags))
		for i, tag := range tags {
			names[i] = tag.Name
		}
		f.Tags = strings.Join(names, ", ")
		formPage(w, r, d, p, f, f.MilestoneID, http.StatusOK)
	}
}

// TicketUpdate saves the edit form.
func TicketUpdate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		key := ticketKey(p, t.Number)
		// The version rides through every re-render, field errors included,
		// so the next save is still checked.
		f := templates.TicketForm{Project: p, TicketKey: key, Version: r.PostFormValue("version")}
		in, errs := readTicketForm(r, d, p, &f)
		if len(errs) > 0 {
			f.Errors = errs
			formPage(w, r, d, p, f, t.MilestoneID.String, http.StatusUnprocessableEntity)
			return
		}
		version, _ := strconv.ParseInt(f.Version, 10, 64)
		if _, err := d.Core.PatchTicket(r.Context(), d.Actor.ID, t.ID, in.Patch(), version); err != nil {
			if conflict, ok := errors.AsType[*core.Conflict](err); ok {
				// Keep what was typed; the form now carries the current
				// version, so saving again overwrites knowingly.
				f.Version = strconv.FormatInt(conflict.Current.Version, 10)
				f.Conflict = strings.Replace(conflict.Error(), "since you read it", "since you opened this form", 1)
				formPage(w, r, d, p, f, t.MilestoneID.String, http.StatusConflict)
				return
			}
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				f.Errors = fields
				formPage(w, r, d, p, f, t.MilestoneID.String, http.StatusUnprocessableEntity)
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Ticket saved.", "/t/"+key)
	}
}

// TicketShow is the ticket page; ?edit={comment id} opens that comment's
// edit form in place.
func TicketShow(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		ticketPage(w, r, d, t, p, http.StatusOK, func(*templates.TicketView) {})
	}
}

// ticketPage builds and renders the ticket page; adjust sets form state
// (a refused comment or link) before rendering.
func ticketPage(w http.ResponseWriter, r *http.Request, d Deps, t gen.Ticket, p gen.Project, status int, adjust func(*templates.TicketView)) {
	templates.LoadEventSeq(r.Context()) // before the page data is read
	v, err := ticketView(r, d, t, p)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	adjust(&v)
	render(w, r, d, status, templates.TicketPage(v))
}

func ticketView(r *http.Request, d Deps, t gen.Ticket, p gen.Project) (templates.TicketView, error) {
	ctx := r.Context()
	now := d.Now()
	v := templates.TicketView{Project: p, Ticket: t, Key: ticketKey(p, t.Number), Body: markdown.Render(t.Body), LinkKind: "blocks"}
	var err error
	if v.Status, err = d.R.GetStatus(ctx, t.StatusID); err != nil {
		return v, err
	}
	if t.AssigneeID.Valid {
		a, err := d.R.GetActor(ctx, t.AssigneeID.String)
		if err != nil {
			return v, err
		}
		v.Assignee = a.Name
		v.AssigneeIsAgent = a.Kind == "agent"
	}
	if t.MilestoneID.Valid {
		m, err := d.R.GetMilestone(ctx, t.MilestoneID.String)
		if err != nil {
			return v, err
		}
		v.Milestone = m.Name
	}
	if t.ParentID.Valid {
		parent, err := d.R.GetTicket(ctx, t.ParentID.String)
		if err != nil {
			return v, err
		}
		v.Parent = &templates.TicketRef{Key: ticketKey(p, parent.Number), Title: parent.Title}
	}
	tags, err := d.R.ListTicketTags(ctx, t.ID)
	if err != nil {
		return v, err
	}
	for _, tag := range tags {
		v.Tags = append(v.Tags, tag.Name)
	}
	subs, err := d.R.ListSubTickets(ctx, sqlNull(t.ID))
	if err != nil {
		return v, err
	}
	for _, s := range subs {
		v.Subs = append(v.Subs, templates.TicketRef{Key: ticketKey(p, s.Number), Title: s.Title, Status: s.StatusName, Done: isDone(s.StatusCategory)})
	}
	if v.Links, err = linkGroups(ctx, d, t.ID); err != nil {
		return v, err
	}
	comments, err := d.R.ListComments(ctx, t.ID)
	if err != nil {
		return v, err
	}
	editing := r.URL.Query().Get("edit")
	for _, c := range comments {
		at, _ := timefmt.Parse(c.CreatedAt)
		v.Comments = append(v.Comments, templates.CommentRow{
			ID: c.ID, Author: c.AuthorName, Raw: c.Body, Body: markdown.Render(c.Body),
			Ago: timefmt.Ago(at, now, nil), Mine: c.AuthorID == d.Actor.ID,
			Editing: c.ID == editing && c.AuthorID == d.Actor.ID, Edited: c.UpdatedAt != c.CreatedAt,
		})
	}
	atts, err := d.R.ListTicketAttachments(ctx, t.ID)
	if err != nil {
		return v, err
	}
	for _, a := range atts {
		at, _ := timefmt.Parse(a.CreatedAt)
		v.Attachments = append(v.Attachments, templates.AttachmentRow{
			ID: a.ID, Name: a.Name, URL: attachmentURL(gen.Attachment{ID: a.ID, Name: a.Name}), Size: units.SizeWords(a.Size),
			By: a.UploaderName, Ago: timefmt.Ago(at, now, nil), CanDelete: core.CanDeleteAttachment(d.Actor, gen.Attachment{UploadedBy: a.UploadedBy}),
		})
	}
	events, err := d.R.ListTicketEvents(ctx, gen.ListTicketEventsParams{TicketID: sqlNull(t.ID), TicketKey: v.Key})
	if err != nil {
		return v, err
	}
	for _, e := range events {
		at, _ := timefmt.Parse(e.At)
		v.Events = append(v.Events, templates.EventView{
			Actor: e.ActorName, Kind: e.Kind, ProjectKey: e.ProjectKey,
			TicketKey:   core.TicketKey{Project: e.ProjectKey, Number: e.TicketNumber}.String(),
			TicketTitle: e.TicketTitle, Payload: templates.ParsePayload(e.Payload), Ago: timefmt.Ago(at, now, nil),
			Reader: v.Key,
		})
	}
	return v, nil
}

func isDone(category string) bool { return category == "done" || category == "cancelled" }

// linkGroups sorts a ticket's links by direction and kind, in reading order.
func linkGroups(ctx context.Context, d Deps, ticketID string) ([]templates.LinkGroup, error) {
	from, err := d.R.ListLinksFrom(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	to, err := d.R.ListLinksTo(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	order := []string{"Blocks", "Blocked by", "Relates to", "Duplicates", "Duplicated by"}
	groups := map[string][]templates.TicketRef{}
	ref := func(id, key string, number int64, title, category string) templates.TicketRef {
		return templates.TicketRef{ID: id, Key: core.TicketKey{Project: key, Number: number}.String(), Title: title, Done: isDone(category)}
	}
	outLabel := map[string]string{"blocks": "Blocks", "relates": "Relates to", "duplicates": "Duplicates"}
	inLabel := map[string]string{"blocks": "Blocked by", "relates": "Relates to", "duplicates": "Duplicated by"}
	for _, l := range from {
		groups[outLabel[l.Kind]] = append(groups[outLabel[l.Kind]], ref(l.ID, l.OtherKey, l.OtherNumber, l.OtherTitle, l.OtherCategory))
	}
	for _, l := range to {
		groups[inLabel[l.Kind]] = append(groups[inLabel[l.Kind]], ref(l.ID, l.OtherKey, l.OtherNumber, l.OtherTitle, l.OtherCategory))
	}
	var out []templates.LinkGroup
	for _, label := range order {
		if items := groups[label]; len(items) > 0 {
			out = append(out, templates.LinkGroup{Label: label, Items: items})
		}
	}
	return out, nil
}

// CommentCreate adds a comment; a refused one re-renders the page with it.
func CommentCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		body := r.PostFormValue("body")
		if _, err := d.Core.AddComment(r.Context(), d.Actor.ID, t.ID, body); err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				ticketPage(w, r, d, t, p, http.StatusUnprocessableEntity, func(v *templates.TicketView) {
					v.CommentBody, v.CommentErr = body, fields["body"]
				})
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Comment added.", "/t/"+ticketKey(p, t.Number))
	}
}

// commentAction changes the {id} comment on this ticket and returns to it.
func commentAction(d Deps, okMsg string, fn func(r *http.Request, commentID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		to := "/t/" + ticketKey(p, t.Number)
		id := chi.URLParam(r, "id")
		c, err := d.R.GetComment(r.Context(), id)
		if err != nil || c.TicketID != t.ID {
			redirectWithFlash(w, r, flash.Error, "That comment no longer exists.", to)
			return
		}
		err = fn(r, id)
		var inv *core.Invalid
		switch {
		case err == nil:
			redirectWithFlash(w, r, flash.Success, okMsg, to)
		case errors.As(err, &inv):
			redirectWithFlash(w, r, flash.Error, inv.Error(), to+"?edit="+id)
		default:
			serverError(w, r, d, err)
		}
	}
}

// CommentUpdate and CommentDelete change one of the person's own comments.
func CommentUpdate(d Deps) http.HandlerFunc {
	return commentAction(d, "Comment saved.", func(r *http.Request, id string) error {
		return d.Core.EditComment(r.Context(), d.Actor.ID, id, r.PostFormValue("body"))
	})
}

func CommentDelete(d Deps) http.HandlerFunc {
	return commentAction(d, "Comment deleted.", func(r *http.Request, id string) error {
		return d.Core.DeleteComment(r.Context(), d.Actor.ID, id)
	})
}

// LinkCreate links this ticket to the one typed; a refused link re-renders
// the page with the typed key and its error.
func LinkCreate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		kind, to := r.PostFormValue("kind"), strings.TrimSpace(r.PostFormValue("to"))
		refuse := func(msg string) {
			ticketPage(w, r, d, t, p, http.StatusUnprocessableEntity, func(v *templates.TicketView) {
				v.LinkKind, v.LinkTo, v.LinkErr = kind, to, msg
			})
		}
		k, parsed := core.ParseTicketKey(to)
		if !parsed {
			refuse("That isn't a ticket key.")
			return
		}
		if _, err := d.Core.AddLink(r.Context(), d.Actor.ID, t.ID, kind, k); err != nil {
			invalidOrError(w, r, d, err, func(fields map[string]string) {
				msg := fields["to"]
				if msg == "" {
					msg = fields["kind"]
				}
				refuse(msg)
			})
			return
		}
		redirectWithFlash(w, r, flash.Success, "Link added.", "/t/"+ticketKey(p, t.Number))
	}
}

// LinkDelete removes a link that touches this ticket.
func LinkDelete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		to := "/t/" + ticketKey(p, t.Number)
		l, err := d.R.GetLink(r.Context(), chi.URLParam(r, "id"))
		if err != nil || l.FromID != t.ID && l.ToID != t.ID {
			redirectWithFlash(w, r, flash.Error, "That link no longer exists.", to)
			return
		}
		if err := d.Core.RemoveLink(r.Context(), d.Actor.ID, l.ID); err != nil {
			serverError(w, r, d, err)
			return
		}
		redirectWithFlash(w, r, flash.Success, "Link removed.", to)
	}
}

func sqlNull(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
