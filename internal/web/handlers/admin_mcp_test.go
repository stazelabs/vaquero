package handlers_test

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/db/gen"
)

func TestMCPCallsLogged(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	long := strings.Repeat("word ", 60)
	mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-1"})
	mcpCall(t, cs, "get_ticket", map[string]any{"key": "VAQ-99"})
	mcpCall(t, cs, "add_comment", map[string]any{"key": "VAQ-1", "body": long})
	cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "drop_tables", Arguments: map[string]any{}})
	cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_ticket", Arguments: map[string]any{"key": 12}})
	rows, err := e.Deps.R.ListMCPCalls(bg, mcpListAll())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	// Newest first.
	want := []struct{ tool, outcome string }{
		{"get_ticket", "bad_arguments"}, {"drop_tables", "unknown_tool"}, {"add_comment", "ok"},
		{"get_ticket", "not_found"}, {"get_ticket", "ok"},
	}
	for i, w := range want {
		r := rows[i]
		if r.Tool != w.tool || r.Outcome != w.outcome || r.ActorName != "builder" || r.TokenPrefix != tok[:9] {
			t.Errorf("row %d: %+v, want %v", i, r, w)
		}
		for _, col := range []string{r.Args, r.Message} {
			if strings.Contains(col, tok) {
				t.Fatal("a token reached the log")
			}
		}
	}
	if rows[3].Message != "No ticket VAQ-99." || rows[4].Args != "key=VAQ-1" {
		t.Errorf("%+v / %+v", rows[3], rows[4])
	}
	if a := rows[2].Args; !strings.HasPrefix(a, `body="word word`) || !strings.Contains(a, "key=VAQ-1") || len([]rune(a)) > 110 {
		t.Errorf("summary %q", a)
	}
}

func TestAdminMCPPage(t *testing.T) {
	e, tok := agentEnv(t)
	builder := mcpConnect(t, e, tok, "builder")
	reviewer := mcpConnect(t, e, tok, "reviewer")
	mcpCall(t, builder, "ready_tickets", map[string]any{})
	mcpCall(t, reviewer, "get_ticket", map[string]any{"key": "VAQ-99"})
	c := e.Client()
	page := c.Get("/admin/mcp").Body
	for _, want := range []string{"MCP activity", "ready_tickets", "get_ticket", "builder", "reviewer", "not_found", "No ticket VAQ-99.",
		`data-live-mcp`, `hx-trigger="vq:mcp"`, `href="/admin/mcp"`, `data-icon="agent"`, `?tool=get_ticket`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	rev, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "?agent="+rev.ID) {
		t.Error("the chip filters by actor id")
	}
	filtered := c.Get("/admin/mcp?agent=" + rev.ID).Body
	if strings.Contains(filtered, "<td class=\"font-mono text-xs\">ready_tickets") || !strings.Contains(filtered, "No ticket VAQ-99.") {
		t.Errorf("agent filter:\n%s", filtered)
	}
	if empty := c.Get("/admin/mcp?tool=search").Body; !strings.Contains(empty, "No MCP calls") {
		t.Error("no empty state")
	}
}

func mcpListAll() gen.ListMCPCallsParams { return gen.ListMCPCallsParams{Lim: 1000} }

// VAQ-66: the filter is by actor id, so an agent called "Owner" is not the
// human Owner, and the two chips are told apart.
func TestAdminMCPFilterByActorNotName(t *testing.T) {
	e, tok := agentEnv(t)
	imp := mcpConnect(t, e, tok, "Owner")
	mcpCall(t, imp, "ready_tickets", map[string]any{})
	human := mcpConnect(t, e, tok, "")
	mcpCall(t, human, "get_ticket", map[string]any{"key": "VAQ-1"})
	agent, err := e.Deps.Core.EnsureAgent(bg, e.Deps.Actor.ID, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID == e.Deps.Actor.ID {
		t.Fatal("an agent named Owner is the human Owner")
	}
	c := e.Client()
	page := c.Get("/admin/mcp").Body
	if !strings.Contains(page, "Owner (agent)") || !strings.Contains(page, "Owner (human)") {
		t.Errorf("chips not told apart:\n%s", page)
	}
	byAgent := c.Get("/admin/mcp?agent=" + agent.ID).Body
	if !strings.Contains(byAgent, "ready_tickets</td>") || strings.Contains(byAgent, "get_ticket</td>") {
		t.Errorf("agent filter:\n%s", byAgent)
	}
	byHuman := c.Get("/admin/mcp?agent=" + e.Deps.Actor.ID).Body
	if strings.Contains(byHuman, "ready_tickets</td>") || !strings.Contains(byHuman, "get_ticket</td>") {
		t.Errorf("human filter:\n%s", byHuman)
	}
}
