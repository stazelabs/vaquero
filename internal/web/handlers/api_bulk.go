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
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// maxBulk is the most items one bulk call takes. The 1 MiB body limit
// still applies, so 100 long comments don't fit in one call.
const maxBulk = 100

// bulkItem is one ticket's work in a bulk call. Its actions run in the
// order bulkActions names them; version applies to its update.
type bulkItem struct {
	Key     string         `json:"key"`
	Version int64          `json:"version"`
	Claim   *leaseBody     `json:"claim"`
	Update  *ticketRequest `json:"update"`
	Comment *string        `json:"comment"`
	Move    *string        `json:"move"`
	Release bool           `json:"release"`
}

// bulkActions is the fixed order an item's actions run in.
var bulkActions = []string{"claim", "update", "comment", "move", "release"}

// actions are the item's actions, in bulkActions order.
func (it bulkItem) actions() []string {
	has := map[string]bool{"claim": it.Claim != nil, "update": it.Update != nil, "comment": it.Comment != nil,
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
			fields[at] = fmt.Sprintf("Item %d has nothing to do. Give it a claim, update, comment, move or release.", n)
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
// claim?, update?, comment?, move?, release?}]}: up to 100 items, each
// run on its own, in request order, as the request's actor. Each action
// is the same core operation its own endpoint makes, with its own
// transaction and event; the call itself writes nothing. Within an item
// the actions run claim, update, comment, move, release, and the first
// failure skips the rest of that item. A key may appear more than once;
// its items run in order. A call that can't run as sent (no items, more
// than 100, an item with no key or nothing to do) is a 422 and runs
// nothing.
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
			out.Items[i] = runBulkItem(r, d, it)
		}
		api.WriteJSON(w, http.StatusOK, out)
	}
}

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
	by := apiActor(r).ID
	// The version is the one the caller read. A claim first bumps it by
	// one, so the update checks the claim's version, provided the claim
	// found the ticket at the caller's version.
	version := it.Version
	var failed *api.Failure
	var cause error
	for i, act := range todo {
		switch act {
		case "claim":
			var nt gen.Ticket
			nt, err = d.Core.Claim(ctx, by, t.ID, time.Duration(it.Claim.LeaseMinutes)*time.Minute)
			if err == nil && version != 0 && nt.Version == version+1 {
				version = nt.Version
			}
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
			_, err = d.Core.Release(ctx, by, t.ID)
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
