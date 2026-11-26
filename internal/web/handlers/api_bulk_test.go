package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/web/handlers"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

type bulkResult struct {
	Key     string
	OK      bool
	Status  string
	Version int64
	Error   *struct {
		Code, Message string
		Fields        map[string]string
	}
	Skipped []string
}

type bulkAnswer struct{ Items []bulkResult }

// bulkEnv is agentEnv with VAQ-2 and VAQ-3 as well.
func bulkEnv(t *testing.T) (*webtest.Env, string) {
	t.Helper()
	e, tok := agentEnv(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	for _, title := range []string{"Second", "Third"} {
		if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	return e, tok
}

func events(t *testing.T, e *webtest.Env) int64 {
	t.Helper()
	n, err := e.Deps.R.CountEvents(bg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func bulk(t *testing.T, c *webtest.Client, tok string, items ...map[string]any) bulkAnswer {
	t.Helper()
	r := c.API("POST", "/api/v1/tickets/bulk", tok, map[string]any{"items": items})
	if r.Status != 200 {
		t.Fatalf("bulk: %d %s", r.Status, r.Body)
	}
	var out bulkAnswer
	decode(t, r.Body, &out)
	if len(out.Items) != len(items) {
		t.Fatalf("%d results for %d items: %s", len(out.Items), len(items), r.Body)
	}
	return out
}

// VAQ-117 R11: one refused item doesn't stop the others; each change is its
// own event and the call adds none.
func TestAPIBulkItemsStandAlone(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	before := events(t, e)
	out := bulk(t, c, tok,
		map[string]any{"key": "VAQ-1", "comment": "Looked at it.", "move": "In review"},
		map[string]any{"key": "VAQ-99", "comment": "Nobody home."},
		map[string]any{"key": "VAQ-2", "move": "Nope"},
		map[string]any{"key": "vaq-003", "update": map[string]any{"title": "Third, renamed", "priority": 2}},
	)
	got := out.Items
	if !got[0].OK || got[0].Key != "VAQ-1" || got[0].Status != "In review" || got[0].Version != 2 || got[0].Error != nil || got[0].Skipped != nil {
		t.Errorf("item 1: %+v", got[0])
	}
	if got[1].OK || got[1].Key != "VAQ-99" || got[1].Error == nil || got[1].Error.Code != "not_found" || got[1].Error.Message != "No ticket VAQ-99." ||
		strings.Join(got[1].Skipped, ",") != "comment" || got[1].Status != "" || got[1].Version != 0 {
		t.Errorf("item 2: %+v", got[1])
	}
	if got[2].OK || got[2].Error == nil || got[2].Error.Code != "invalid" || got[2].Error.Message != "No status Nope in VAQ." ||
		got[2].Error.Fields["status"] != "No status Nope in VAQ." || got[2].Status != "Todo" || got[2].Version != 1 || got[2].Skipped != nil {
		t.Errorf("item 3: %+v", got[2])
	}
	if !got[3].OK || got[3].Key != "VAQ-3" || got[3].Version != 2 {
		t.Errorf("item 4: %+v", got[3])
	}
	// VAQ-1's comment and move, VAQ-3's update.
	if after := events(t, e); after != before+3 {
		t.Fatalf("events %d → %d, want 3 more", before, after)
	}
	var tk struct{ Title, Status string }
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-3", tok, nil).Body, &tk)
	if tk.Title != "Third, renamed" {
		t.Fatalf("%+v", tk)
	}
}

// Within an item the first failure skips the rest: a refused claim means
// no comment, no move and no release.
func TestAPIBulkSkipsAfterAFailedClaim(t *testing.T) {
	e, tok := bulkEnv(t)
	rev := e.Client()
	rev.Agent = "reviewer"
	if r := rev.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	c := e.Client()
	c.Agent = "builder"
	c.API("GET", "/api/v1/me", tok, nil) // the agent's first use is an event of its own
	before := events(t, e)
	out := bulk(t, c, tok,
		map[string]any{"key": "VAQ-1", "claim": map[string]any{}, "comment": "Mine now.", "move": "Done", "release": true},
		map[string]any{"key": "VAQ-2", "claim": map[string]any{"lease_minutes": 120}, "comment": "Taking this."},
	)
	got := out.Items[0]
	if got.OK || got.Error == nil || got.Error.Code != "invalid" || !strings.HasPrefix(got.Error.Message, "VAQ-1 is claimed by reviewer until") ||
		strings.Join(got.Skipped, ",") != "comment,move,release" || got.Status != "In progress" {
		t.Fatalf("item 1: %+v", got)
	}
	if !out.Items[1].OK || out.Items[1].Status != "In progress" {
		t.Fatalf("item 2: %+v", out.Items[1])
	}
	// VAQ-2's claim and comment only.
	if after := events(t, e); after != before+2 {
		t.Fatalf("events %d → %d, want 2 more", before, after)
	}
	// An agent acting through Vaquero-Agent claims as itself.
	var tk struct {
		ClaimedBy      string `json:"claimed_by"`
		ClaimExpiresAt string `json:"claim_expires_at"`
		Comments       []struct{ Author string }
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &tk)
	if tk.ClaimedBy != "builder" || tk.ClaimExpiresAt != "2026-11-18T14:00:00.000Z" || len(tk.Comments) != 1 || tk.Comments[0].Author != "builder" {
		t.Fatalf("%+v", tk)
	}
	// Claiming one's own claim again renews it, so a bulk claim is also a
	// bulk renewal; release gives it up.
	out = bulk(t, c, tok, map[string]any{"key": "VAQ-2", "claim": map[string]any{}}, map[string]any{"key": "VAQ-2", "release": true})
	if !out.Items[0].OK || !out.Items[1].OK {
		t.Fatalf("%+v", out.Items)
	}
	tk.ClaimedBy = ""
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &tk)
	if tk.ClaimedBy != "" {
		t.Fatalf("%+v", tk)
	}
}

// An item's version applies to its update; a stale one refuses that item
// alone. A claim in the same item doesn't make its version stale.
func TestAPIBulkVersionConflict(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	if r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]any{"title": "Changed"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	before := events(t, e)
	out := bulk(t, c, tok,
		map[string]any{"key": "VAQ-1", "version": 1, "update": map[string]any{"title": "Stale"}, "comment": "Not posted."},
		map[string]any{"key": "VAQ-2", "version": 1, "update": map[string]any{"title": "Fresh"}},
		map[string]any{"key": "VAQ-3", "version": 1, "claim": map[string]any{}, "update": map[string]any{"priority": 1}},
	)
	got := out.Items
	if got[0].OK || got[0].Error == nil || got[0].Error.Code != "conflict" || got[0].Error.Message != "VAQ-1 changed since you read it." ||
		got[0].Version != 2 || strings.Join(got[0].Skipped, ",") != "update,comment" {
		t.Errorf("item 1: %+v", got[0])
	}
	if !got[1].OK || got[1].Version != 2 {
		t.Errorf("item 2: %+v", got[1])
	}
	if !got[2].OK || got[2].Version != 3 || got[2].Status != "In progress" {
		t.Errorf("item 3: %+v", got[2])
	}
	if after := events(t, e); after != before+3 {
		t.Fatalf("events %d → %d, want 3 more", before, after)
	}
}

// R14: a stale version stops the whole item before anything runs, the
// claim included.
func TestAPIBulkStaleVersionRunsNothing(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	if r := c.API("PATCH", "/api/v1/tickets/VAQ-2", tok, map[string]any{"title": "Changed"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	before := events(t, e)
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-2", "version": 1, "claim": map[string]any{}, "update": map[string]any{"title": "x"}, "release": true})
	if got := out.Items[0]; got.OK || got.Error == nil || got.Error.Code != "conflict" || got.Error.Message != "VAQ-2 changed since you read it." ||
		strings.Join(got.Skipped, ",") != "claim,update,release" || got.Version != 2 || got.Status != "Todo" {
		t.Fatalf("stale: %+v", got)
	}
	if after := events(t, e); after != before {
		t.Fatalf("events %d → %d", before, after)
	}
	var tk struct {
		Title     string
		ClaimedBy string `json:"claimed_by"`
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-2", tok, nil).Body, &tk)
	if tk.Title != "Changed" || tk.ClaimedBy != "" {
		t.Fatalf("%+v", tk)
	}
}

// R12: a release after a move that finished the ticket (which ends the
// claim) has nothing left to do and succeeds.
func TestAPIBulkReleaseAfterFinishingMove(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	c.API("GET", "/api/v1/me", tok, nil)
	before := events(t, e)
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "claim": map[string]any{}, "comment": "Done.", "move": "Done", "release": true})
	if got := out.Items[0]; !got.OK || got.Error != nil || got.Skipped != nil || got.Status != "Done" {
		t.Fatalf("%+v", got)
	}
	// claim, comment, move; the release writes nothing.
	if after := events(t, e); after != before+3 {
		t.Fatalf("events %d → %d, want 3 more", before, after)
	}
	var tk struct {
		Status    string
		ClaimedBy string `json:"claimed_by"`
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Status != "Done" || tk.ClaimedBy != "" {
		t.Fatalf("%+v", tk)
	}
	// A claim already held before the item counts too.
	if r := c.API("POST", "/api/v1/tickets/VAQ-2/claim", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	out = bulk(t, c, tok, map[string]any{"key": "VAQ-2", "move": "Cancelled", "release": true})
	if got := out.Items[0]; !got.OK || got.Status != "Cancelled" {
		t.Fatalf("%+v", got)
	}
	// Without a claim ended in the item, release is refused as on its own.
	out = bulk(t, c, tok, map[string]any{"key": "VAQ-3", "comment": "x", "release": true})
	if got := out.Items[0]; got.OK || got.Error == nil || got.Error.Message != "VAQ-3 isn't claimed." {
		t.Fatalf("%+v", got)
	}
}

// R13: heartbeat renews each claim, writes no event and keeps the version;
// one on a claim you don't hold fails that item.
func TestAPIBulkHeartbeat(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	for _, k := range []string{"VAQ-1", "VAQ-2"} {
		if r := c.API("POST", "/api/v1/tickets/"+k+"/claim", tok, nil); r.Status != 200 {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
	}
	rev := e.Client()
	rev.Agent = "reviewer"
	rev.API("POST", "/api/v1/tickets/VAQ-3/claim", tok, nil)
	before := events(t, e)
	out := bulk(t, c, tok,
		map[string]any{"key": "VAQ-1", "heartbeat": map[string]any{"lease_minutes": 90}},
		map[string]any{"key": "VAQ-2", "heartbeat": map[string]any{}},
		map[string]any{"key": "VAQ-3", "heartbeat": map[string]any{}, "comment": "Not mine."},
	)
	if !out.Items[0].OK || out.Items[0].Version != 2 || !out.Items[1].OK || out.Items[1].Version != 2 {
		t.Fatalf("%+v", out.Items)
	}
	if got := out.Items[2]; got.OK || got.Error == nil || got.Error.Message != "VAQ-3 is claimed by reviewer, not you." || strings.Join(got.Skipped, ",") != "comment" {
		t.Fatalf("%+v", got)
	}
	if after := events(t, e); after != before {
		t.Fatalf("events %d → %d", before, after)
	}
	var tk struct {
		ClaimExpiresAt string `json:"claim_expires_at"`
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.ClaimExpiresAt != "2026-11-18T13:30:00.000Z" {
		t.Fatalf("%+v", tk)
	}
}

// Without Vaquero-Agent the owner's token acts as the owner.
func TestAPIBulkActsAsTheOwner(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	bulk(t, c, tok, map[string]any{"key": "VAQ-1", "comment": "Mine."})
	var tk struct{ Comments []struct{ Author string } }
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.Comments) != 1 || tk.Comments[0].Author != e.Deps.Actor.Name {
		t.Fatalf("%+v, owner %q", tk, e.Deps.Actor.Name)
	}
}

// A request that ends between items reports the rest as cancelled and
// runs none of them.
func TestAPIBulkStopsWhenTheRequestEnds(t *testing.T) {
	e, tok := bulkEnv(t)
	handlers.CancelBulkAt(t, 1)
	c := e.Client()
	before := events(t, e)
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "comment": "Runs."}, map[string]any{"key": "VAQ-2", "comment": "Doesn't.", "move": "Done"},
		map[string]any{"key": "VAQ-3", "release": true})
	if !out.Items[0].OK {
		t.Fatalf("%+v", out.Items[0])
	}
	for _, got := range out.Items[1:] {
		if got.OK || got.Error == nil || got.Error.Code != "cancelled" || len(got.Skipped) == 0 {
			t.Fatalf("%+v", got)
		}
	}
	if strings.Join(out.Items[1].Skipped, ",") != "comment,move" {
		t.Fatalf("%+v", out.Items[1])
	}
	if after := events(t, e); after != before+1 {
		t.Fatalf("events %d → %d, want 1 more", before, after)
	}
}

// The same ticket may appear more than once; its items run in request order.
func TestAPIBulkDuplicateKeysRunInOrder(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "comment": "First."}, map[string]any{"key": "vaq-001", "comment": "Second."})
	if !out.Items[0].OK || !out.Items[1].OK || out.Items[1].Key != "VAQ-1" {
		t.Fatalf("%+v", out.Items)
	}
	var tk struct{ Comments []struct{ Body string } }
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.Comments) != 2 || tk.Comments[0].Body != "First." || tk.Comments[1].Body != "Second." {
		t.Fatalf("%+v", tk)
	}
}

// A call that can't be run as sent runs nothing: a 422 (or a 400 for a
// body that isn't the request, a 413 over the body limit) for the whole
// call.
func TestAPIBulkRefusedWhole(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	ok := map[string]any{"key": "VAQ-1", "comment": "Fine."}
	many := make([]map[string]any, 101)
	for i := range many {
		many[i] = ok
	}
	long := make([]map[string]any, 100)
	for i := range long {
		long[i] = map[string]any{"key": "VAQ-1", "comment": strings.Repeat("x", 19999)}
	}
	before := events(t, e)
	for _, tc := range []struct {
		name   string
		body   any
		status int
		want   string
	}{
		{"no items", map[string]any{"items": []any{}}, 422, `"items": "Send at least one item."`},
		{"no items field", map[string]any{}, 422, `"items": "Send at least one item."`},
		{"over 100", map[string]any{"items": many}, 422, `"items": "Send at most 100 items at a time."`},
		{"no key", map[string]any{"items": []any{ok, map[string]any{"comment": "x"}}}, 422, `"items[1].key": "Item 2 has no key."`},
		{"no actions", map[string]any{"items": []any{ok, map[string]any{"key": "VAQ-2"}}}, 422, `"items[1]": "Item 2 has nothing to do.`},
		{"version without update", map[string]any{"items": []any{map[string]any{"key": "VAQ-2", "version": 1, "move": "Done"}}}, 422, `"items[0].version": "Item 1 has a version but no update to check it against."`},
		{"version in update", map[string]any{"items": []any{map[string]any{"key": "VAQ-2", "update": map[string]any{"version": 1}}}}, 422, `"items[0].update.version": "Put item 1's version on the item, not in its update."`},
		{"blank move", map[string]any{"items": []any{ok, map[string]any{"key": "VAQ-2", "move": " "}}}, 422, `"items[1].move": "Name the status to move item 2 to."`},
		{"unknown field", map[string]any{"items": []any{map[string]any{"key": "VAQ-2", "stop": true}}}, 400, `"code": "bad_request"`},
		{"over the body limit", map[string]any{"items": long}, 413, `"code": "too_large"`},
	} {
		r := c.API("POST", "/api/v1/tickets/bulk", tok, tc.body)
		if r.Status != tc.status || !strings.Contains(r.Body, tc.want) {
			t.Errorf("%s: %d %s", tc.name, r.Status, r.Body)
		}
	}
	if after := events(t, e); after != before {
		t.Fatalf("events %d → %d", before, after)
	}
	// Exactly 100 is allowed.
	out := bulk(t, c, tok, many[:100]...)
	for i, it := range out.Items {
		if !it.OK {
			t.Fatalf("item %d: %+v", i+1, it)
		}
	}
	if after := events(t, e); after != before+100 {
		t.Fatalf("events %d → %d, want 100 more", before, after)
	}
}

// bulk_update is the REST call: the same answer, as an object.
func TestMCPBulkUpdateMatchesTheAPI(t *testing.T) {
	e, tok := bulkEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	items := []map[string]any{
		{"key": "VAQ-1", "comment": "Checked."},
		{"key": "VAQ-99", "comment": "Missing."},
		{"key": "VAQ-2", "move": "Nope"},
		{"key": "VAQ-3", "heartbeat": map[string]any{"lease_minutes": 5}, "release": true},
		{"key": "VAQ-3", "heartbeat": map[string]any{}},
	}
	res, text := mcpCall(t, cs, "bulk_update", map[string]any{"items": items})
	if res.IsError {
		t.Fatalf("bulk_update: %s", text)
	}
	c := e.Client()
	c.Agent = "builder"
	if want := c.API("POST", "/api/v1/tickets/bulk", tok, map[string]any{"items": items}).Body; text != want {
		t.Fatalf("bulk_update:\n got %s\nwant %s", text, want)
	}
	checkStructured(t, "bulk_update", res, text)
	var tk struct{ Comments []struct{ Author string } }
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if len(tk.Comments) != 2 || tk.Comments[0].Author != "builder" {
		t.Fatalf("%+v", tk)
	}
	if res, text := mcpCall(t, cs, "bulk_update", map[string]any{"items": []any{}}); !res.IsError || text != "Send at least one item." {
		t.Fatalf("empty: %v %q", res.IsError, text)
	}
}

// A claim ended outside the item (released elsewhere, or lapsed) between
// the item's start and its release gets the normal release, refused: only
// the item's own update or move ending it counts as released.
func TestAPIBulkReleaseAfterAClaimEndedElsewhere(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	if r := c.API("POST", "/api/v1/tickets/VAQ-1/claim", tok, nil); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	builder, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "builder")
	if err != nil {
		t.Fatal(err)
	}
	handlers.OnBulkStep(t, 0, "release", func(context.CancelFunc) {
		if _, err := e.Deps.Core.Release(bg, builder.ID, mustTicketID(t, e, 1)); err != nil {
			t.Error(err)
		}
	})
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "comment": "Stopping.", "release": true})
	if got := out.Items[0]; got.OK || got.Error == nil || got.Error.Message != "VAQ-1 isn't claimed." {
		t.Fatalf("%+v", got)
	}
}

// A request that ends in the middle of an item reports that item and the
// rest as cancelled, with what didn't run in skipped.
func TestAPIBulkCancelledMidItem(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	handlers.OnBulkStep(t, 0, "move", func(cancel context.CancelFunc) { cancel() })
	before := events(t, e)
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "comment": "Runs.", "move": "Done", "release": true},
		map[string]any{"key": "VAQ-2", "comment": "Doesn't."})
	if got := out.Items[0]; got.OK || got.Error == nil || got.Error.Code != "cancelled" || strings.Join(got.Skipped, ",") != "move,release" || got.Status != "" {
		t.Fatalf("item 1: %+v", got)
	}
	if got := out.Items[1]; got.OK || got.Error == nil || got.Error.Code != "cancelled" || strings.Join(got.Skipped, ",") != "comment" {
		t.Fatalf("item 2: %+v", got)
	}
	if after := events(t, e); after != before+1 {
		t.Fatalf("events %d → %d, want 1 more", before, after)
	}
	var tk struct{ Status string }
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Status != "Todo" {
		t.Fatalf("%+v", tk)
	}
}

// A bulk move to the status the ticket already has changes nothing, so a
// retried sweep doesn't reshuffle the column.
func TestAPIBulkMoveToSameStatusIsANoOp(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	before := events(t, e)
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "move": "todo"}, map[string]any{"key": "VAQ-2", "move": "In review"},
		map[string]any{"key": "VAQ-2", "move": "In review"})
	for i, got := range out.Items {
		if !got.OK || got.Version != []int64{1, 2, 2}[i] {
			t.Fatalf("item %d: %+v", i+1, got)
		}
	}
	if after := events(t, e); after != before+1 {
		t.Fatalf("events %d → %d, want 1 more", before, after)
	}
}

// Claim and heartbeat in one item is refused whole.
func TestAPIBulkClaimAndHeartbeatRefused(t *testing.T) {
	e, tok := bulkEnv(t)
	r := e.Client().API("POST", "/api/v1/tickets/bulk", tok, map[string]any{"items": []any{
		map[string]any{"key": "VAQ-1", "claim": map[string]any{}, "heartbeat": map[string]any{}}}})
	if r.Status != 422 || !strings.Contains(r.Body, `"items[0].heartbeat": "Item 1 can claim or renew, not both."`) {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// VAQ-122: a request that ends after an item's last action has run reports
// the item done, with the status and version of the ticket that action
// returned. Here the move to Done ends the claim, so the release has
// nothing to do and the request ends as it is reached.
func TestAPIBulkLateCancelKeepsStatusAndVersion(t *testing.T) {
	e, tok := bulkEnv(t)
	c := e.Client()
	c.Agent = "builder"
	handlers.OnBulkStep(t, 0, "release", func(cancel context.CancelFunc) { cancel() })
	out := bulk(t, c, tok, map[string]any{"key": "VAQ-1", "claim": map[string]any{}, "move": "Done", "release": true},
		map[string]any{"key": "VAQ-2", "comment": "Doesn't."})
	var tk struct {
		Status  string
		Version int64
	}
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Status != "Done" {
		t.Fatalf("%+v", tk)
	}
	if got := out.Items[0]; !got.OK || got.Error != nil || got.Status != "Done" || got.Version != tk.Version {
		t.Fatalf("item 1: %+v, ticket %+v", got, tk)
	}
	if got := out.Items[1]; got.OK || got.Error == nil || got.Error.Code != "cancelled" {
		t.Fatalf("item 2: %+v", got)
	}
}
