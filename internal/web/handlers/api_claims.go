package handlers

import (
	"bufio"
	"cmp"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/board"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// readyLimit is how many tickets GET /api/v1/ready gives without a limit.
const readyLimit = 50

// priorityRank orders priorities urgent (1) to low (4), then none (0).
func priorityRank(p int64) int64 {
	if p == 0 {
		return 5
	}
	return p
}

// ReadyTickets is the ready queue: the ready tickets of project (every
// active project when nil), urgent to low then no priority, each in board
// order, at most limit. It is the board's ready rule, read from the board;
// in a project with the agent workflow on, the board's ready cards also
// passed readiness.Check (board.Card.Readiness), so each carries it.
func ReadyTickets(r *http.Request, d Deps, project *gen.Project, limit int64) ([]api.TicketSummary, error) {
	var projects []gen.Project
	if project != nil {
		projects = []gen.Project{*project}
	} else {
		rows, err := d.R.ListProjects(r.Context())
		if err != nil {
			return nil, err
		}
		for _, p := range rows {
			if !p.ArchivedAt.Valid {
				projects = append(projects, gen.Project{ID: p.ID, Key: p.Key, Name: p.Name, Workflow: p.Workflow})
			}
		}
	}
	type item struct {
		s     api.TicketSummary
		order int
	}
	var items []item
	for _, p := range projects {
		cols, _, err := board.LoadList(r.Context(), d.R, p, apiActor(r), board.Filter{Priority: -1})
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			for _, k := range c.Cards {
				if k.Ready {
					items = append(items, item{summary(k, c.Status), len(items)})
				}
			}
		}
	}
	slices.SortStableFunc(items, func(a, b item) int {
		return cmp.Or(cmp.Compare(priorityRank(a.s.Priority), priorityRank(b.s.Priority)), cmp.Compare(a.order, b.order))
	})
	out := []api.TicketSummary{}
	for _, it := range items {
		if int64(len(out)) >= limit {
			break
		}
		out = append(out, it.s)
	}
	return out, nil
}

// APIReady is GET /api/v1/ready?project=KEY&limit=N.
func APIReady(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var project *gen.Project
		if key := strings.TrimSpace(r.URL.Query().Get("project")); key != "" {
			p, err := d.R.GetProjectByKey(r.Context(), strings.ToUpper(key))
			if errors.Is(err, sql.ErrNoRows) {
				api.WriteFailure(w, api.Fail(http.StatusNotFound, "not_found", "No project "+strings.ToUpper(key)+"."))
				return
			}
			if err != nil {
				apiFail(w, r, d, api.Internal(), err)
				return
			}
			project = &p
		}
		out, err := ReadyTickets(r, d, project, limitParam(r, readyLimit, maxAPIList))
		if err != nil {
			apiFail(w, r, d, api.Internal(), err)
			return
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// leaseBody is a claim or heartbeat request: {lease_minutes?}. The body may
// be empty.
type leaseBody struct {
	LeaseMinutes int64 `json:"lease_minutes"`
}

// decodeLease reads an optional leaseBody.
func decodeLease(w http.ResponseWriter, r *http.Request) (time.Duration, bool) {
	br := bufio.NewReader(r.Body)
	r.Body = struct {
		io.Reader
		io.Closer
	}{br, r.Body}
	if _, err := br.Peek(1); err != nil {
		return 0, true
	}
	var in leaseBody
	if f := api.Decode(r, &in); f != nil {
		api.WriteFailure(w, f)
		return 0, false
	}
	return time.Duration(in.LeaseMinutes) * time.Minute, true
}

// claimAction is POST /api/v1/tickets/{ticket}/claim, /heartbeat or
// /release: op runs as the request's actor and the ticket is the answer,
// short with ?view=summary.
func claimAction(d Deps, withLease bool, op func(r *http.Request, by, ticketID string, lease time.Duration) (gen.Ticket, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		short, ok := viewParam(w, r)
		if !ok {
			return
		}
		t, p, ok := apiTicket(w, r, d)
		if !ok {
			return
		}
		var lease time.Duration
		if withLease {
			if lease, ok = decodeLease(w, r); !ok {
				return
			}
		}
		nt, err := op(r, apiActor(r).ID, t.ID, lease)
		if err != nil {
			apiCoreFail(w, r, d, p, err)
			return
		}
		writeTicketView(w, r, d, nt, p, http.StatusOK, short)
	}
}

// APIClaim is POST /api/v1/tickets/{ticket}/claim {lease_minutes?}.
func APIClaim(d Deps) http.HandlerFunc {
	return claimAction(d, true, func(r *http.Request, by, id string, lease time.Duration) (gen.Ticket, error) {
		return d.Core.Claim(r.Context(), by, id, lease)
	})
}

// APIHeartbeat is POST /api/v1/tickets/{ticket}/heartbeat {lease_minutes?}.
func APIHeartbeat(d Deps) http.HandlerFunc {
	return claimAction(d, true, func(r *http.Request, by, id string, lease time.Duration) (gen.Ticket, error) {
		return d.Core.Heartbeat(r.Context(), by, id, lease)
	})
}

// APIRelease is POST /api/v1/tickets/{ticket}/release.
func APIRelease(d Deps) http.HandlerFunc {
	return claimAction(d, false, func(r *http.Request, by, id string, _ time.Duration) (gen.Ticket, error) {
		return d.Core.Release(r.Context(), by, id)
	})
}
