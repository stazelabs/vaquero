package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// chipsFor builds the filter chips for a page at base: Mine, Agents, each
// tag on the page, and each priority. A chip that is on links to the filter
// without it.
func chipsFor(base string, f board.Filter, tags []string) []templates.Chip {
	chip := func(label, key, value string, on bool) templates.Chip {
		next := f.With(key, value)
		if on {
			next = f.With(key, "")
		}
		return templates.Chip{Label: label, Href: templates.FilterHref(base, next), On: on}
	}
	out := []templates.Chip{chip("Mine", "mine", "1", f.Mine), chip("Agents", "agents", "1", f.Agents)}
	for _, t := range tags {
		out = append(out, chip(t, "tag", t, f.Tag == t))
	}
	for p := int64(1); p <= 4; p++ {
		out = append(out, chip(templates.PriorityLabel(p), "priority", strconv.FormatInt(p, 10), f.Priority == p))
	}
	return out
}

// tagsOn lists the distinct tags on a set of columns, sorted, keeping the
// active tag even when nothing carries it.
func tagsOn(cols [][]board.Column, active string) []string {
	var out []string
	for _, group := range cols {
		for _, c := range group {
			for _, k := range c.Cards {
				for _, t := range k.Tags {
					if !slices.Contains(out, t) {
						out = append(out, t)
					}
				}
			}
		}
	}
	if active != "" && !slices.Contains(out, active) {
		out = append(out, active)
	}
	slices.Sort(out)
	return out
}

func loadBoard(r *http.Request, d Deps, p gen.Project, f board.Filter) (board.Board, error) {
	return board.Load(r.Context(), d.R, p, d.Actor, f, d.Now())
}

// ProjectShow is the project's board.
func ProjectShow(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := board.ParseFilter(r.URL.Query())
		b, err := loadBoard(r, d, p, f)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		base := projectPath(p, "")
		v := templates.BoardView{Board: b, Chips: chipsFor(base, f, tagsOn([][]board.Column{b.Active, b.Rails}, f.Tag)), Clear: base}
		render(w, r, d, http.StatusOK, templates.BoardPage(v))
	}
}

// BoardFragment is the board's swappable part, for live refreshes.
func BoardFragment(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		boardFragment(w, r, d, p, board.ParseFilter(r.URL.Query()))
	}
}

func boardFragment(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, f board.Filter) {
	b, err := loadBoard(r, d, p, f)
	if err != nil {
		serverError(w, r, d, err)
		return
	}
	render(w, r, d, http.StatusOK, templates.BoardSection(b))
}

// ProjectListView lists every status with every matching ticket.
func ProjectListView(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := board.ParseFilter(r.URL.Query())
		cols, err := board.LoadList(r.Context(), d.R, p, d.Actor, f)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		base := projectPath(p, "/list")
		v := templates.ListView{Project: p, Filter: f, Columns: cols, Chips: chipsFor(base, f, tagsOn([][]board.Column{cols}, f.Tag)), Clear: base}
		render(w, r, d, http.StatusOK, templates.ListPage(v))
	}
}

// refreshWith answers an htmx request whose page is out of date: the toast
// is queued and htmx reloads the page, which re-reads everything.
func refreshWith(w http.ResponseWriter, msg string) {
	flash.Put(w, flash.Error, msg)
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// TicketMove is a board drop: form status (a status id), before and after
// (the neighbours' ticket keys, either may be empty) and filter (the
// board's query string). An htmx request gets the refreshed board; a
// refused or stale drop reloads the page with a toast.
func TicketMove(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hx := r.Header.Get("HX-Request") == "true"
		fail := func(msg, to string) {
			if hx {
				refreshWith(w, msg)
				return
			}
			redirectWithFlash(w, r, flash.Error, msg, to)
		}
		k, ok := core.ParseTicketKey(chi.URLParam(r, "ticket"))
		if !ok {
			fail("That isn't a ticket key.", "/")
			return
		}
		t, p, err := d.Core.ResolveTicket(r.Context(), d.R, k)
		if errors.Is(err, core.ErrNotFound) {
			fail("No ticket "+k.String()+".", "/")
			return
		}
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		neighbour := func(field string) string {
			nk, ok := core.ParseTicketKey(r.PostFormValue(field))
			if !ok {
				return ""
			}
			n, _, err := d.Core.ResolveTicket(r.Context(), d.R, nk)
			if err != nil {
				return ""
			}
			return n.ID
		}
		_, err = d.Core.MoveTicket(r.Context(), d.Actor.ID, t.ID, r.PostFormValue("status"), neighbour("before"), neighbour("after"))
		var inv *core.Invalid
		switch {
		case errors.As(err, &inv):
			fail(inv.Error(), projectPath(p, ""))
			return
		case errors.Is(err, core.ErrNotFound):
			fail("That ticket no longer exists.", projectPath(p, ""))
			return
		case err != nil:
			serverError(w, r, d, err)
			return
		}
		if !hx {
			http.Redirect(w, r, projectPath(p, ""), http.StatusSeeOther)
			return
		}
		q, _ := url.ParseQuery(r.PostFormValue("filter"))
		boardFragment(w, r, d, p, board.ParseFilter(q))
	}
}
