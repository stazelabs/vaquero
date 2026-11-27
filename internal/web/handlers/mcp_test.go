package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/mcpserver"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

// headers adds a bearer token and, when set, Vaquero-Agent to each request.
type headers struct{ token, agent string }

func (h headers) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.token != "" {
		r.Header.Set("Authorization", "Bearer "+h.token)
	}
	if h.agent != "" {
		r.Header.Set("Vaquero-Agent", h.agent)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func mcpConnect(t *testing.T, e *webtest.Env, token, agent string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: e.Server.URL + "/mcp", HTTPClient: &http.Client{Transport: headers{token, agent}},
		DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// mcpCall calls a tool and returns its result and its one text content.
func mcpCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d contents", name, len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: content %T", name, res.Content[0])
	}
	return res, tc.Text
}

var frozenTools = []string{
	"answer_decision", "ask_decision", "bulk_update", "claim_ticket", "create_comment", "create_reference", "create_ticket", "delete_reference", "get_me", "get_plan", "get_ticket",
	"heartbeat", "link_tickets", "list_commits", "list_decisions", "list_projects", "list_statuses", "list_tags", "list_tickets", "move_ticket", "read_file", "ready_tickets", "recent_activity", "save_plan",
	"search", "unclaim_ticket", "unlink_tickets", "update_ticket",
}

func TestMCPToolList(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "")
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("%s: no description or schema", tool.Name)
		}
		// A tool's caller sees the answer's body, never its headers.
		if strings.Contains(tool.Description, "Vaquero-") {
			t.Errorf("%s: its description names a header", tool.Name)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, frozenTools) {
		t.Fatalf("tools changed:\n got %v\nwant %v", names, frozenTools)
	}
	if !slices.Equal(mcpserver.ToolNames(), frozenTools) {
		t.Fatalf("ToolNames: %v", mcpserver.ToolNames())
	}
	if init := cs.InitializeResult(); !strings.Contains(init.Instructions, "claim_ticket") {
		t.Fatalf("instructions: %q", init.Instructions)
	}
}

func TestMCPClaimLoop(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	_, text := mcpCall(t, cs, "ready_tickets", map[string]any{"project": "VAQ"})
	if !strings.Contains(text, `"key": "VAQ-1"`) {
		t.Fatalf("ready: %s", text)
	}
	res, text := mcpCall(t, cs, "claim_ticket", map[string]any{"key": "VAQ-1", "lease_minutes": 30})
	if res.IsError || !strings.Contains(text, `"claimed_by": "builder"`) {
		t.Fatalf("claim: %s", text)
	}
	if res, text := mcpCall(t, cs, "create_comment", map[string]any{"key": "vaq-1", "body": "Working on it."}); res.IsError {
		t.Fatalf("comment: %s", text)
	}
	if res, text := mcpCall(t, cs, "heartbeat", map[string]any{"key": "VAQ-1"}); res.IsError {
		t.Fatalf("heartbeat: %s", text)
	}
	if res, text := mcpCall(t, cs, "unclaim_ticket", map[string]any{"key": "VAQ-1"}); res.IsError || strings.Contains(text, "claimed_by") {
		t.Fatalf("release: %s", text)
	}
	_, text = mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	var tk struct {
		Comments []struct{ Author, Body string }
	}
	if err := json.Unmarshal([]byte(text), &tk); err != nil || len(tk.Comments) != 1 || tk.Comments[0].Author != "builder" {
		t.Fatalf("%v %s", err, text)
	}
}

// Review Focus 4.
func TestMCPRefusals(t *testing.T) {
	e, tok := agentEnv(t)
	post := func(token, body string) (int, string) {
		req, _ := http.NewRequest("POST", e.Server.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b strings.Builder
		_, _ = io.Copy(&b, res.Body)
		return res.StatusCode, b.String()
	}
	if code, body := post("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); code != 401 || !strings.Contains(body, "unauthorized") {
		t.Errorf("no token: %d %s", code, body)
	}
	if code, body := post(tok, `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`); code != 400 || !strings.Contains(body, "one request at a time") {
		t.Errorf("batch: %d %s", code, body)
	}
	if code, _ := post(tok, ``); code != 400 {
		t.Errorf("empty: %d", code)
	}
	req, _ := http.NewRequest("GET", e.Server.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 405 {
		t.Errorf("GET: %v %v", res.StatusCode, err)
	}

	cs := mcpConnect(t, e, tok, "builder")
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "drop_tables", Arguments: map[string]any{}}); err == nil {
		t.Error("unknown tool accepted")
	}
	for name, args := range map[string]map[string]any{
		"missing key":   {},
		"wrong type":    {"key": 12},
		"unknown field": {"key": "VAQ-1", "nope": true},
	} {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_ticket", Arguments: args})
		if err == nil && !res.IsError {
			t.Errorf("%s: accepted", name)
		}
	}
	if res, text := mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-99"}); !res.IsError || text != "No ticket VAQ-99." {
		t.Errorf("not found: %v %q", res.IsError, text)
	}
	mcpCall(t, cs, "claim_ticket", map[string]any{"key": "VAQ-1"})
	other := mcpConnect(t, e, tok, "reviewer")
	if res, text := mcpCall(t, other, "claim_ticket", map[string]any{"key": "VAQ-1"}); !res.IsError || !strings.Contains(text, "VAQ-1 is claimed by builder until") {
		t.Errorf("core refusal: %v %q", res.IsError, text)
	}
	if res, text := mcpCall(t, cs, "update_ticket", map[string]any{"key": "VAQ-1", "title": "x", "version": 1}); !res.IsError || !strings.Contains(text, "changed since you read it") {
		t.Errorf("conflict: %v %q", res.IsError, text)
	}
}

// Each tool answers with the REST body for the same request.
func TestMCPToolsMatchTheAPI(t *testing.T) {
	e, tok := agentEnv(t)
	p, _ := e.Deps.R.GetProjectByKey(bg, "VAQ")
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, p.ID, core.TicketInput{Title: "Second board ticket", Tags: []string{"ui"}}); err != nil {
		t.Fatal(err)
	}
	c := e.Client()
	cs := mcpConnect(t, e, tok, "")
	for _, tc := range []struct {
		tool string
		args map[string]any
		path string
	}{
		{"list_projects", nil, "/api/v1/projects"},
		{"list_tickets", map[string]any{"project": "VAQ", "tag": "ui"}, "/api/v1/projects/VAQ/tickets?tag=ui"},
		{"list_tickets", map[string]any{"project": "VAQ", "open": true, "limit": 1}, "/api/v1/projects/VAQ/tickets?limit=1&open=1"},
		{"get_ticket", map[string]any{"key": "VAQ-2"}, "/api/v1/tickets/VAQ-2"},
		{"ready_tickets", map[string]any{}, "/api/v1/ready"},
		{"search", map[string]any{"query": "board"}, "/api/v1/search?q=board"},
		{"recent_activity", map[string]any{"newest": 3}, "/api/v1/events?newest=3"},
	} {
		res, text := mcpCall(t, cs, tc.tool, tc.args)
		if res.IsError {
			t.Errorf("%s: %s", tc.tool, text)
			continue
		}
		if want := c.API("GET", tc.path, tok, nil).Body; text != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.tool, text, want)
		}
		checkStructured(t, tc.tool, res, text)
	}
}

// checkStructured holds a result to the MCP schema: structuredContent is a
// JSON object (VAQ-95: a bare list failed in strict clients). It is the REST
// body, or {"items": body} when the body is a list.
func checkStructured(t *testing.T, tool string, res *mcp.CallToolResult, text string) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Errorf("%s: no structured content", tool)
		return
	}
	got, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Errorf("%s: structured content is not an object: %.80s", tool, got)
		return
	}
	want := text
	if strings.HasPrefix(strings.TrimSpace(text), "[") {
		want = `{"items":` + text + `}`
	}
	var a, b any
	_ = json.Unmarshal(got, &a)
	if err := json.Unmarshal([]byte(want), &b); err != nil || !reflect.DeepEqual(a, b) {
		t.Errorf("%s: structured content %.120s, want %.120s", tool, got, want)
	}
}

// get_ticket shows a ticket's attachments, with no tool of their own.
func TestMCPGetTicketListsAttachments(t *testing.T) {
	e, tok := agentEnv(t)
	if r := e.Client().UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "shot.png", "", pngBytes); r.Status != 201 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	cs := mcpConnect(t, e, tok, "builder")
	res, text := mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	var tk struct {
		Attachments []struct {
			Name        string
			ContentType string `json:"content_type"`
			URL         string
			Size        int64
		}
	}
	if err := json.Unmarshal([]byte(text), &tk); err != nil || res.IsError || len(tk.Attachments) != 1 ||
		tk.Attachments[0].Name != "shot.png" || tk.Attachments[0].ContentType != "image/png" ||
		!strings.HasPrefix(tk.Attachments[0].URL, "/attachments/") || tk.Attachments[0].Size != int64(len(pngBytes)) {
		t.Fatalf("%v %s", err, text)
	}
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "get_ticket" && !strings.Contains(tool.Description, "attachments") {
			t.Errorf("get_ticket description: %q", tool.Description)
		}
	}
}

// VAQ-100: a tool refusal carries the field messages, so an agent sees
// which names were wrong, not only the generic sentence.
func TestMCPToolErrorCarriesFields(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	res, text := mcpCall(t, cs, "create_ticket", map[string]any{"project": "VAQ", "title": "x", "status": "Nope", "milestone": "Never"})
	if !res.IsError || !strings.Contains(text, "No status Nope in VAQ.") || !strings.Contains(text, "No milestone Never in VAQ.") {
		t.Fatalf("%v %q", res.IsError, text)
	}
	res, text = mcpCall(t, cs, "create_ticket", map[string]any{"project": "VAQ", "title": "x", "milestone": "Never"})
	if !res.IsError || text != "No milestone Never in VAQ." {
		t.Fatalf("%v %q", res.IsError, text)
	}
}

func TestMCPReferences(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	res, text := mcpCall(t, cs, "create_reference", map[string]any{"key": "VAQ-1", "kind": "design", "target": "https://example.com/d", "label": "Mock"})
	var ref struct{ ID, Kind, Target, Label string }
	if err := json.Unmarshal([]byte(text), &ref); res.IsError || err != nil || ref.ID == "" || ref.Label != "Mock" {
		t.Fatalf("add: %v %s", err, text)
	}
	if res, text := mcpCall(t, cs, "create_reference", map[string]any{"key": "VAQ-1", "kind": "spec", "target": "javascript:x"}); !res.IsError || !strings.Contains(text, "Use a repo path or an http or https URL.") {
		t.Fatalf("refused: %s", text)
	}
	_, text = mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	if !strings.Contains(text, `"references"`) || !strings.Contains(text, ref.ID) {
		t.Fatalf("get_ticket: %s", text)
	}
	if res, text := mcpCall(t, cs, "delete_reference", map[string]any{"key": "VAQ-1", "id": ref.ID}); res.IsError || !strings.Contains(text, `"ok"`) {
		t.Fatalf("remove: %s", text)
	}
	if res, _ := mcpCall(t, cs, "delete_reference", map[string]any{"key": "VAQ-1", "id": ref.ID}); !res.IsError {
		t.Fatal("removed twice")
	}
}

func TestMCPReadsAndUnlink(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	if _, err := e.Deps.Core.CreateTicket(bg, e.Deps.Actor.ID, mustProjectID(t, e, "VAQ"), core.TicketInput{Title: "Second", Tags: []string{"ui"}}); err != nil {
		t.Fatal(err)
	}
	if res, text := mcpCall(t, cs, "list_tags", map[string]any{"project": "VAQ"}); res.IsError || !strings.Contains(text, `"ui"`) {
		t.Fatalf("list_tags: %s", text)
	}
	if res, text := mcpCall(t, cs, "list_statuses", map[string]any{"project": "VAQ"}); res.IsError || !strings.Contains(text, "Todo") {
		t.Fatalf("list_statuses: %s", text)
	}
	if res, text := mcpCall(t, cs, "get_me", map[string]any{}); res.IsError || !strings.Contains(text, "builder") {
		t.Fatalf("get_me: %s", text)
	}
	if res, text := mcpCall(t, cs, "list_commits", map[string]any{"ticket": "VAQ-1"}); !res.IsError || !strings.Contains(text, "No repository set") {
		t.Fatalf("list_commits: %v %s", res.IsError, text)
	}
	if res, text := mcpCall(t, cs, "link_tickets", map[string]any{"key": "VAQ-1", "kind": "relates", "to": "VAQ-2"}); res.IsError {
		t.Fatalf("link: %s", text)
	}
	_, text := mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	var tk struct{ Links []struct{ ID string } }
	if err := json.Unmarshal([]byte(text), &tk); err != nil || len(tk.Links) != 1 || tk.Links[0].ID == "" {
		t.Fatalf("links: %v %s", err, text)
	}
	if res, text := mcpCall(t, cs, "unlink_tickets", map[string]any{"link_id": tk.Links[0].ID}); res.IsError || !strings.Contains(text, `"ok"`) {
		t.Fatalf("unlink: %s", text)
	}
	if res, _ := mcpCall(t, cs, "unlink_tickets", map[string]any{"link_id": tk.Links[0].ID}); !res.IsError {
		t.Fatal("unlinked twice")
	}
}

func mustProjectID(t *testing.T, e *webtest.Env, key string) string {
	t.Helper()
	ps, err := e.Deps.R.ListProjects(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.Key == key {
			return p.ID
		}
	}
	t.Fatal("no project")
	return ""
}
