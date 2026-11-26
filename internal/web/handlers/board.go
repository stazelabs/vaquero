package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/flash"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// filterBar builds the filter row for a page at base: tags (the project's,
// as the board package read them with the cards) and the open milestones
// for the menus, and the assignee filter's name.
func filterBar(r *http.Request, d Deps, p gen.Project, base string, f board.Filter, categories bool, tags []string) (templates.FilterBar, error) {
	b := templates.FilterBar{Base: base, Filter: f, Categories: categories, Tags: tags}
	ms, err := d.R.ListMilestones(r.Context(), p.ID)
	if err != nil {
		return b, err
	}
	for _, m := range ms {
		if !m.ClosedAt.Valid || m.ID == f.Milestone {
			b.Milestones = append(b.Milestones, templates.MenuOption{Value: m.ID, Label: m.Name})
		}
	}
	if f.Assignee != "" {
		if a, err := d.R.GetActor(r.Context(), f.Assignee); err == nil {
			b.AssigneeName = a.Name
		}
	}
	return b, nil
}

// ProjectFocus is the project's landing page: Next, In flight and Just
// finished. It takes the board's filters, so an old board address
// (/p/{key}?tag=ui) opens Focus filtered the same way.
func ProjectFocus(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := board.ParseFilter(r.URL.Query())
		fo, err := board.LoadFocus(r.Context(), d.R, p, d.Actor, f, d.Now())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		bar, err := filterBar(r, d, p, projectPath(p, ""), fo.Filter, false, fo.Tags)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.FocusPage(templates.FocusView{Focus: fo, Bar: bar}))
	}
}

// FocusFragment is the Focus view's lanes, for live refreshes.
func FocusFragment(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		fo, err := board.LoadFocus(r.Context(), d.R, p, d.Actor, board.ParseFilter(r.URL.Query()), d.Now())
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.FocusSection(fo))
	}
}

func loadBoard(r *http.Request, d Deps, p gen.Project, f board.Filter) (board.Board, error) {
	return board.Load(r.Context(), d.R, p, d.Actor, f, d.Now())
}

// ProjectBoard is the project's board. A tab opened before the board
// moved here refreshes its live part (#board) from this address, so an
// htmx request aimed at #board gets the fragment.
func ProjectBoard(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		f := board.ParseFilter(r.URL.Query())
		if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "board" {
			boardFragment(w, r, d, p, f)
			return
		}
		b, err := loadBoard(r, d, p, f)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		bar, err := filterBar(r, d, p, projectPath(p, "/board"), b.Filter, false, b.Tags)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.BoardPage(templates.BoardView{Board: b, Bar: bar}))
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

// ListPageSize is how many tickets a done or cancelled group shows at a time.
const ListPageSize = 50

// listPaged says whether a status category's list group is paged.
func listPaged(category string) bool { return category == "done" || category == "cancelled" }

// listShown reads how many rows the browser shows of each paged group
// (shown=STATUS:N, comma-separated or repeated), so a live refresh renders
// as many. Malformed entries are ignored.
func listShown(q url.Values) map[string]int {
	out := map[string]int{}
	for _, v := range q["shown"] {
		for part := range strings.SplitSeq(v, ",") {
			id, n, ok := strings.Cut(part, ":")
			if !ok {
				continue
			}
			if k, err := strconv.Atoi(n); err == nil && k > 0 {
				out[id] = k
			}
		}
	}
	return out
}

// listPage answers one page of a paged group's rows, with its "Show 50
// more" row. Bad input answers no rows: an offset that isn't a number from
// 0 to the group's size, or a group that isn't one of cols' paged ones (an
// unknown status, an active one, or one the filter leaves out).
func listPage(w http.ResponseWriter, r *http.Request, d Deps, p gen.Project, f board.Filter, cols []board.Column, group, offsetText string) {
	i := slices.IndexFunc(cols, func(c board.Column) bool { return c.Status.ID == group && listPaged(c.Status.Category) })
	offset, err := strconv.Atoi(offsetText)
	if i < 0 || err != nil || offset < 0 || offset > len(cols[i].Cards) {
		w.WriteHeader(http.StatusOK)
		return
	}
	c := cols[i]
	end := min(offset+ListPageSize, len(c.Cards))
	g := templates.ListGroup{Column: c, Shown: c.Cards[offset:end], Paged: true}
	if end < len(c.Cards) {
		g.Next = end
	}
	render(w, r, d, http.StatusOK, templates.ListRows(p, f, g))
}

// ProjectListView lists every status with every matching ticket. Done and
// cancelled groups show ListPageSize at a time (more when the page says it
// shows more: shown=STATUS:N); ?group=STATUS&offset=N answers the rows of
// that group's next page alone (listPage).
func ProjectListView(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		q := r.URL.Query()
		f := board.ParseFilter(q)
		cols, tags, err := board.LoadList(r.Context(), d.R, p, d.Actor, f)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if q.Has("group") {
			listPage(w, r, d, p, f, cols, q.Get("group"), q.Get("offset"))
			return
		}
		shown := listShown(q)
		groups := make([]templates.ListGroup, 0, len(cols))
		for _, c := range cols {
			g := templates.ListGroup{Column: c, Shown: c.Cards, Open: true}
			if listPaged(c.Status.Category) {
				g.Paged = true
				g.Forced = f.Category != ""
				g.Open = g.Forced
				n := ListPageSize
				if want := shown[c.Status.ID]; want > n {
					n = (want + ListPageSize - 1) / ListPageSize * ListPageSize
				}
				if n < len(c.Cards) {
					g.Shown, g.Next = c.Cards[:n], n
				}
			}
			groups = append(groups, g)
		}
		bar, err := filterBar(r, d, p, projectPath(p, "/list"), f, true, tags)
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		render(w, r, d, http.StatusOK, templates.ListPage(templates.ListView{Project: p, Filter: f, Groups: groups, Bar: bar}))
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
			fail(inv.Error(), projectPath(p, "/board"))
			return
		case errors.Is(err, core.ErrNotFound):
			fail("That ticket no longer exists.", projectPath(p, "/board"))
			return
		case err != nil:
			serverError(w, r, d, err)
			return
		}
		if !hx {
			http.Redirect(w, r, projectPath(p, "/board"), http.StatusSeeOther)
			return
		}
		q, _ := url.ParseQuery(r.PostFormValue("filter"))
		boardFragment(w, r, d, p, board.ParseFilter(q))
	}
}
