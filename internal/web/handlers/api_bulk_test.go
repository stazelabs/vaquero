package handlers_test

import (
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
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
		got[0].Version != 2 || strings.Join(got[0].Skipped, ",") != "comment" {
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
	// A claim that finds the ticket changed since the item's version keeps
	// the claim and refuses the update.
	out = bulk(t, c, tok, map[string]any{"key": "VAQ-2", "version": 1, "claim": map[string]any{}, "update": map[string]any{"title": "x"}, "release": true})
	if got := out.Items[0]; got.OK || got.Error == nil || got.Error.Code != "conflict" || strings.Join(got.Skipped, ",") != "release" {
		t.Fatalf("stale after claim: %+v", got)
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
