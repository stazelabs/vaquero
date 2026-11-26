package handlers_test

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// summaryKeys are the only fields a ?view=summary answer may carry.
var summaryKeys = []string{"assignee", "category", "claim_expires_at", "claimed_by", "id", "key", "status", "version"}

// checkSummary holds body to the short answer: only summaryKeys, with key,
// status, category and version always there, and id exactly when wantID.
func checkSummary(t *testing.T, what, body string, wantID bool) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("%s: %v %s", what, err, body)
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(summaryKeys, k) {
			t.Errorf("%s: unexpected field %q in %s", what, k, body)
		}
	}
	for _, k := range []string{"key", "status", "category", "version"} {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: no %q in %s", what, k, body)
		}
	}
	if _, ok := m["id"]; ok != wantID {
		t.Errorf("%s: id present %v, want %v: %s", what, ok, wantID, body)
	}
	return m
}

// VAQ-117 R10: every write endpoint gives the short answer on
// ?view=summary and the full ticket otherwise.
func TestAPIWritesSummaryView(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	c.Agent = "builder"

	r := c.API("POST", "/api/v1/tickets/VAQ-1/claim?view=summary", tok, map[string]int{"lease_minutes": 30})
	if r.Status != 200 {
		t.Fatalf("claim: %d %s", r.Status, r.Body)
	}
	m := checkSummary(t, "claim", r.Body, false)
	if m["key"] != "VAQ-1" || m["status"] != "In progress" || m["category"] != "in_progress" || m["assignee"] != "builder" ||
		m["claimed_by"] != "builder" || m["claim_expires_at"] != "2026-11-18T12:30:00.000Z" || m["version"] != float64(2) {
		t.Fatalf("claim: %s", r.Body)
	}

	r = c.API("POST", "/api/v1/tickets/VAQ-1/heartbeat?view=summary", tok, nil)
	if r.Status != 200 {
		t.Fatalf("heartbeat: %d %s", r.Status, r.Body)
	}
	checkSummary(t, "heartbeat", r.Body, false)

	r = c.API("PATCH", "/api/v1/tickets/VAQ-1?view=summary", tok, map[string]any{"title": "Renamed", "version": 2})
	if r.Status != 200 {
		t.Fatalf("patch: %d %s", r.Status, r.Body)
	}
	if m := checkSummary(t, "patch", r.Body, false); m["version"] != float64(3) {
		t.Fatalf("patch: %s", r.Body)
	}

	r = c.API("POST", "/api/v1/tickets/VAQ-1/comments?view=summary", tok, map[string]string{"body": "Working on it."})
	if r.Status != 201 {
		t.Fatalf("comment: %d %s", r.Status, r.Body)
	}
	if m := checkSummary(t, "comment", r.Body, true); m["key"] != "VAQ-1" || m["id"] == "" {
		t.Fatalf("comment: %s", r.Body)
	}

	r = c.API("POST", "/api/v1/tickets/VAQ-1/move?view=summary", tok, map[string]string{"status": "In review"})
	if r.Status != 200 {
		t.Fatalf("move: %d %s", r.Status, r.Body)
	}
	if m := checkSummary(t, "move", r.Body, false); m["status"] != "In review" || m["category"] != "in_review" {
		t.Fatalf("move: %s", r.Body)
	}

	r = c.API("POST", "/api/v1/tickets/VAQ-1/release?view=summary", tok, nil)
	if r.Status != 200 {
		t.Fatalf("release: %d %s", r.Status, r.Body)
	}
	if m := checkSummary(t, "release", r.Body, false); m["claimed_by"] != nil || m["assignee"] != "builder" {
		t.Fatalf("release: %s", r.Body)
	}

	// The default, and view=full, stay the whole ticket.
	for _, path := range []string{"/api/v1/tickets/VAQ-1/move", "/api/v1/tickets/VAQ-1/move?view=full"} {
		r = c.API("POST", path, tok, map[string]string{"status": "Todo"})
		if r.Status != 200 || !strings.Contains(r.Body, `"body":`) || !strings.Contains(r.Body, `"comments":`) {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
	}
	r = c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]string{"body": "Again."})
	if r.Status != 201 || !strings.Contains(r.Body, `"author": "builder"`) {
		t.Fatalf("comment default: %d %s", r.Status, r.Body)
	}
}

// An unknown view is refused before anything is written.
func TestAPIUnknownViewWritesNothing(t *testing.T) {
	e, tok := agentEnv(t)
	c := e.Client()
	before, _ := e.Deps.R.CountEvents(bg)
	for _, rq := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/tickets/VAQ-1/claim?view=short", nil},
		{"POST", "/api/v1/tickets/VAQ-1/heartbeat?view=short", nil},
		{"POST", "/api/v1/tickets/VAQ-1/release?view=short", nil},
		{"PATCH", "/api/v1/tickets/VAQ-1?view=short", map[string]string{"title": "x"}},
		{"POST", "/api/v1/tickets/VAQ-1/move?view=short", map[string]string{"status": "Done"}},
		{"POST", "/api/v1/tickets/VAQ-1/comments?view=short", map[string]string{"body": "x"}},
	} {
		r := c.API(rq.method, rq.path, tok, rq.body)
		if r.Status != 422 || !strings.Contains(r.Body, `"view": "No view short. Use full or summary."`) {
			t.Errorf("%s %s: %d %s", rq.method, rq.path, r.Status, r.Body)
		}
	}
	if after, _ := e.Deps.R.CountEvents(bg); after != before {
		t.Fatalf("events %d → %d", before, after)
	}
}

// The matching MCP tools take view and pass it to the API.
func TestMCPWritesSummaryView(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	for _, tc := range []struct {
		tool   string
		args   map[string]any
		wantID bool
	}{
		{"claim_ticket", map[string]any{"key": "VAQ-1", "view": "summary"}, false},
		{"heartbeat", map[string]any{"key": "VAQ-1", "view": "summary"}, false},
		{"update_ticket", map[string]any{"key": "VAQ-1", "title": "Renamed", "view": "summary"}, false},
		{"add_comment", map[string]any{"key": "VAQ-1", "body": "On it.", "view": "summary"}, true},
		{"move_ticket", map[string]any{"key": "VAQ-1", "status": "In review", "view": "summary"}, false},
		{"release_ticket", map[string]any{"key": "VAQ-1", "view": "summary"}, false},
	} {
		res, text := mcpCall(t, cs, tc.tool, tc.args)
		if res.IsError {
			t.Fatalf("%s: %s", tc.tool, text)
		}
		checkSummary(t, tc.tool, text, tc.wantID)
		checkStructured(t, tc.tool, res, text)
	}
	// The same request over REST gives the same bytes (a heartbeat changes
	// nothing the answer shows on a fixed clock).
	mcpCall(t, cs, "claim_ticket", map[string]any{"key": "VAQ-1"})
	_, text := mcpCall(t, cs, "heartbeat", map[string]any{"key": "VAQ-1", "view": "summary"})
	c := e.Client()
	c.Agent = "builder"
	if want := c.API("POST", "/api/v1/tickets/VAQ-1/heartbeat?view=summary", tok, nil).Body; text != want {
		t.Fatalf("heartbeat:\n got %s\nwant %s", text, want)
	}
	if res, text := mcpCall(t, cs, "heartbeat", map[string]any{"key": "VAQ-1", "view": "short"}); !res.IsError || text != "No view short. Use full or summary." {
		t.Fatalf("bad view: %v %q", res.IsError, text)
	}
}
