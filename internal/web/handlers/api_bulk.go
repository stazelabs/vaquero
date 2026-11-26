package handlers

import (
	"context"
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
		if it.Claim != nil && it.Heartbeat != nil {
			fields[at+".heartbeat"] = fmt.Sprintf("Item %d can claim or renew, not both.", n)
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
// request ends early (the client left, the server is stopping), the item
// it ends in and those not yet run are reported as cancelled, with the
// actions that didn't run in skipped.
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
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		r = r.WithContext(ctx)
		out := api.BulkAnswer{Items: make([]api.BulkResult, len(in.Items))}
		ran := 0
		for i, it := range in.Items {
			bulkStep(i, "", cancel)
			if ctx.Err() != nil {
				break
			}
			out.Items[i] = runBulkItem(r, d, i, it, cancel)
			ran = i + 1
			if ctx.Err() != nil {
				break
			}
		}
		for j := ran; j < len(in.Items) && ctx.Err() != nil; j++ {
			out.Items[j] = api.BulkResult{Key: strings.TrimSpace(in.Items[j].Key), Skipped: in.Items[j].actions(), Error: bulkCancelled()}
		}
		if n := countCancelled(out.Items); n > 0 {
			d.Logger.WarnContext(ctx, "api bulk cancelled", "err", ctx.Err(), "items", n,
				"request_id", middleware.GetRequestID(ctx))
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

// lateReadTimeout bounds the one read an item makes after its request
// ended (the name of the ticket's status).
const lateReadTimeout = 2 * time.Second

// bulkStep runs before item i and before each of its actions (action ""
// for the item itself). It is a test hook: it can end the request with
// cancel, or change the ticket from outside, at that point.
var bulkStep = func(_ int, _ string, _ context.CancelFunc) {}

// countCancelled is how many results the request's end cut short.
func countCancelled(results []api.BulkResult) int {
	n := 0
	for _, res := range results {
		if res.Error != nil && res.Error.Code == "cancelled" {
			n++
		}
	}
	return n
}

// bulkCancelled is the result's error for an item the request ended in or
// before: the actions in skipped didn't run.
func bulkCancelled() *api.Failure {
	return api.Fail(http.StatusServiceUnavailable, "cancelled", "The request ended before this item finished; the actions in skipped didn't run.")
}

// runBulkItem runs item i's actions and reports how it went.
func runBulkItem(r *http.Request, d Deps, i int, it bulkItem, cancel context.CancelFunc) api.BulkResult {
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
	if err != nil && ctx.Err() != nil {
		res.Error, res.Skipped = bulkCancelled(), todo
		return res
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
	// item or from its claim. ended: this item's update or move ended that
	// claim (a finishing move, a reassignment), so its release has nothing
	// left to do. A claim ended any other way gets the normal release.
	held := t.ClaimedBy.Valid && t.ClaimedBy.String == by && t.ClaimExpiresAt.String > timefmt.Format(d.Now())
	ended := false
	cur := t
	// The update checks the version again, atomically. A claim first bumps
	// it by one, so the update checks the claim's version, provided the
	// claim found the ticket at the caller's version (another writer may
	// have got in between).
	version := it.Version
	var failed *api.Failure
	var cause error
	// after notes a ticket an update or move returned.
	after := func(nt gen.Ticket) {
		cur = nt
		if held && !nt.ClaimedBy.Valid {
			ended = true
		}
	}
	for a, act := range todo {
		bulkStep(i, act, cancel)
		var nt gen.Ticket
		switch act {
		case "claim":
			nt, err = d.Core.Claim(ctx, by, t.ID, time.Duration(it.Claim.LeaseMinutes)*time.Minute)
			if err == nil {
				cur, held = nt, true
				if version != 0 && nt.Version == version+1 {
					version = nt.Version
				}
			}
		case "heartbeat":
			if nt, err = d.Core.Heartbeat(ctx, by, t.ID, time.Duration(it.Heartbeat.LeaseMinutes)*time.Minute); err == nil {
				cur = nt
			}
		case "update":
			n := &names{ctx: ctx, d: d, p: p, fields: map[string]string{}}
			pt := it.Update.patch(n)
			if failed, cause = n.failure(), n.err; failed == nil {
				if nt, err = d.Core.PatchTicket(ctx, by, t.ID, pt, version); err == nil {
					after(nt)
				}
			}
		case "comment":
			_, err = d.Core.AddComment(ctx, by, t.ID, *it.Comment)
		case "move":
			n := &names{ctx: ctx, d: d, p: p, fields: map[string]string{}}
			status := n.status(*it.Move)
			// A move to the status the ticket has is nothing to do: no
			// reorder, so a retried sweep leaves the columns alone.
			if failed, cause = n.failure(), n.err; failed == nil && status != cur.StatusID {
				if nt, err = d.Core.MoveTicket(ctx, by, t.ID, status, "", ""); err == nil {
					after(nt)
				}
			}
		case "release":
			if !held || !ended {
				if nt, err = d.Core.Release(ctx, by, t.ID); err == nil {
					cur = nt
				}
			}
		}
		if (err != nil || failed != nil) && ctx.Err() != nil {
			res.Error, res.Skipped = bulkCancelled(), todo[a:]
			return res
		}
		if err != nil {
			failed, cause = api.FromCore(err, nil), err
		}
		if failed != nil {
			fail(failed, cause, todo[a+1:])
			break
		}
	}
	if ctx.Err() != nil {
		// The request ended after the item's last action: it ran, but the
		// ticket isn't read again. Its version is the one the last action
		// that returned the ticket left (a comment leaves it alone), and
		// its status that ticket's, named by one read the ended request
		// doesn't stop.
		res.OK = res.Error == nil
		res.Version = cur.Version
		sctx, done := context.WithTimeout(context.WithoutCancel(ctx), lateReadTimeout)
		defer done()
		if st, err := d.R.GetStatus(sctx, cur.StatusID); err == nil {
			res.Status = st.Name
		} else {
			d.Logger.WarnContext(ctx, "api bulk item status", "err", err, "key", res.Key, "request_id", middleware.GetRequestID(ctx))
		}
		return res
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
