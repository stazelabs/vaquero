package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// maxBulk is the most items one bulk call takes. The 1 MiB body limit
// still applies, so 100 long comments don't fit in one call.
const maxBulk = 100

// bulkItem is one ticket's work in a bulk call. Its actions run in the
// order bulkActions names them. Its version is checked before any of them
// runs, and again by its update.
type bulkItem struct {
	Key       string         `json:"key"`
	Version   int64          `json:"version"`
	Claim     *leaseBody     `json:"claim"`
	Heartbeat *leaseBody     `json:"heartbeat"`
	Update    *ticketRequest `json:"update"`
	Comment   *string        `json:"comment"`
	Move      *string        `json:"move"`
	Release   bool           `json:"release"`
}

// bulkActions is the fixed order an item's actions run in.
var bulkActions = []string{"claim", "heartbeat", "update", "comment", "move", "release"}

// actions are the item's actions, in bulkActions order.
func (it bulkItem) actions() []string {
	has := map[string]bool{"claim": it.Claim != nil, "heartbeat": it.Heartbeat != nil, "update": it.Update != nil, "comment": it.Comment != nil,
		"move": it.Move != nil, "release": it.Release}
	var out []string
	for _, a := range bulkActions {
		if has[a] {
			out = append(out, a)
		}
	}
	return out
}

// checkBulk names what keeps the call from running as sent, by field.
func checkBulk(items []bulkItem) map[string]string {
	fields := map[string]string{}
	switch {
	case len(items) == 0:
		fields["items"] = "Send at least one item."
	case len(items) > maxBulk:
		fields["items"] = fmt.Sprintf("Send at most %d items at a time.", maxBulk)
	}
	if len(fields) > 0 {
		return fields
	}
	for i, it := range items {
		at, n := fmt.Sprintf("items[%d]", i), i+1
		if strings.TrimSpace(it.Key) == "" {
			fields[at+".key"] = fmt.Sprintf("Item %d has no key.", n)
		}
		if len(it.actions()) == 0 {
			fields[at] = fmt.Sprintf("Item %d has nothing to do. Give it a claim, heartbeat, update, comment, move or release.", n)
		}
		if it.Version != 0 && it.Update == nil {
			fields[at+".version"] = fmt.Sprintf("Item %d has a version but no update to check it against.", n)
		}
		if it.Update != nil && it.Update.Version != 0 {
			fields[at+".update.version"] = fmt.Sprintf("Put item %d's version on the item, not in its update.", n)
		}
		if it.Move != nil && strings.TrimSpace(*it.Move) == "" {
			fields[at+".move"] = fmt.Sprintf("Name the status to move item %d to.", n)
		}
	}
	return fields
}

// APITicketsBulk is POST /api/v1/tickets/bulk {items: [{key, version?,
// claim?, heartbeat?, update?, comment?, move?, release?}]}: up to 100
// items, each run on its own, in request order, as the request's actor.
// Each action is the same core operation its own endpoint makes, with its
// own transaction and event (a heartbeat has none, as on its endpoint);
// the call itself writes nothing. Within an item the actions run claim,
// heartbeat, update, comment, move, release, and the first failure skips
// the rest of that item. A key may appear more than once; its items run in
// order. A call that can't run as sent (no items, more than 100, an item
// with no key or nothing to do) is a 422 and runs nothing. When the
// request ends early (the client left, the server is stopping), the items
// not yet run are reported as cancelled.
func APITicketsBulk(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Items []bulkItem `json:"items"`
		}
		if f := api.Decode(r, &in); f != nil {
			api.WriteFailure(w, f)
			return
		}
		if fields := checkBulk(in.Items); len(fields) > 0 {
			api.WriteFailure(w, invalidFields(fields, "Some items can't be run as sent."))
			return
		}
		out := api.BulkAnswer{Items: make([]api.BulkResult, len(in.Items))}
		for i, it := range in.Items {
			r = bulkStep(r, i)
			if err := r.Context().Err(); err != nil {
				d.Logger.WarnContext(r.Context(), "api bulk cancelled", "err", err, "left", len(in.Items)-i,
					"request_id", middleware.GetRequestID(r.Context()))
				for j, rest := range in.Items[i:] {
					out.Items[i+j] = api.BulkResult{Key: strings.TrimSpace(rest.Key), Skipped: rest.actions(),
						Error: api.Fail(http.StatusServiceUnavailable, "cancelled", "The request ended before this item ran; nothing in it was changed.")}
				}
				break
			}
			out.Items[i] = runBulkItem(r, d, it)
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// bulkStep is the request item i runs with (a test hook: it can end the
// request between items).
var bulkStep = func(r *http.Request, _ int) *http.Request { return r }

// runBulkItem runs one item's actions and reports how it went.
func runBulkItem(r *http.Request, d Deps, it bulkItem) api.BulkResult {
	ctx := r.Context()
	todo := it.actions()
	res := api.BulkResult{Key: strings.TrimSpace(it.Key)}
	fail := func(f *api.Failure, cause error, skipped []string) api.BulkResult {
		if f.Status >= 500 && cause != nil {
			d.Logger.ErrorContext(ctx, "api bulk item", "err", cause, "key", res.Key,
				"request_id", middleware.GetRequestID(ctx))
		}
		res.Error = f
		if len(skipped) > 0 {
			res.Skipped = skipped
		}
		return res
	}
	k, ok := core.ParseTicketKey(it.Key)
	if !ok {
		return fail(api.NotFound("That isn't a ticket key."), nil, todo)
	}
	res.Key = k.String()
	t, p, err := d.Core.ResolveTicket(ctx, d.R, k)
	if errors.Is(err, core.ErrNotFound) {
		return fail(api.NotFound("No ticket "+k.String()+"."), nil, todo)
	}
	if err != nil {
		return fail(api.Internal(), err, todo)
	}
	// A stale version stops the item before anything runs.
	if it.Version != 0 && t.Version != it.Version {
		fail(api.FromCore(&core.Conflict{Key: res.Key}, nil), nil, todo)
		todo = nil
	}
	by := apiActor(r).ID
	// held: the caller holds a live claim on the ticket, from before the
	// item or from its claim, so a release after an action that ended the
	// claim (a finishing move, a reassignment) has nothing left to do.
	held := t.ClaimedBy.Valid && t.ClaimedBy.String == by && t.ClaimExpiresAt.String > timefmt.Format(d.Now())
	// The update checks the version again, atomically. A claim first bumps
	// it by one, so the update checks the claim's version, provided the
	// claim found the ticket at the caller's version (another writer may
	// have got in between).
	version := it.Version
	var failed *api.Failure
	var cause error
	for i, act := range todo {
		switch act {
		case "claim":
			var nt gen.Ticket
			nt, err = d.Core.Claim(ctx, by, t.ID, time.Duration(it.Claim.LeaseMinutes)*time.Minute)
			if err == nil {
				held = true
				if version != 0 && nt.Version == version+1 {
					version = nt.Version
				}
			}
		case "heartbeat":
			_, err = d.Core.Heartbeat(ctx, by, t.ID, time.Duration(it.Heartbeat.LeaseMinutes)*time.Minute)
		case "update":
			n := &names{ctx: ctx, d: d, p: p, fields: map[string]string{}}
			pt := it.Update.patch(n)
			if failed, cause = n.failure(), n.err; failed == nil {
				_, err = d.Core.PatchTicket(ctx, by, t.ID, pt, version)
			}
		case "comment":
			_, err = d.Core.AddComment(ctx, by, t.ID, *it.Comment)
		case "move":
			n := &names{ctx: ctx, d: d, p: p, fields: map[string]string{}}
			status := n.status(*it.Move)
			if failed, cause = n.failure(), n.err; failed == nil {
				_, err = d.Core.MoveTicket(ctx, by, t.ID, status, "", "")
			}
		case "release":
			if held {
				var now gen.Ticket
				if now, err = d.R.GetTicket(ctx, t.ID); err == nil && !now.ClaimedBy.Valid {
					break // an earlier action ended the claim: released already
				}
			}
			if err == nil {
				_, err = d.Core.Release(ctx, by, t.ID)
			}
		}
		if err != nil {
			failed, cause = api.FromCore(err, nil), err
		}
		if failed != nil {
			fail(failed, cause, todo[i+1:])
			break
		}
	}
	// The ticket as the item left it.
	b, err := ticketBrief(r, d, t, p)
	if err != nil {
		d.Logger.ErrorContext(ctx, "api bulk item", "err", err, "key", res.Key, "request_id", middleware.GetRequestID(ctx))
		if res.Error == nil {
			res.Error = api.Internal()
		}
		return res
	}
	res.Status, res.Version, res.OK = b.Status, b.Version, res.Error == nil
	return res
}
