package handlers_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/workflow"
)

// The server advertises prompts and lists every shipped workflow prompt
// with its arguments; the instructions name them.
func TestMCPPromptsList(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "")
	init := cs.InitializeResult()
	if init.Capabilities.Prompts == nil {
		t.Fatal("prompts not advertised")
	}
	for _, p := range workflow.List() {
		if !strings.Contains(init.Instructions, p.Name) {
			t.Errorf("instructions don't name %s", p.Name)
		}
	}
	res, err := cs.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Prompt{}
	for _, p := range res.Prompts {
		got[p.Name] = p
	}
	if len(got) != len(workflow.List()) {
		t.Fatalf("listed %d prompts", len(res.Prompts))
	}
	for _, w := range workflow.List() {
		p := got[w.Name]
		if p == nil || p.Title != w.Title || p.Description != w.Description || len(p.Arguments) != len(w.Args) {
			t.Fatalf("%s: %+v", w.Name, p)
		}
		for i, a := range w.Args {
			pa := p.Arguments[i]
			if pa.Name != a.Name || pa.Required != a.Required || !strings.HasPrefix(pa.Description, a.Description) {
				t.Errorf("%s %s: %+v", w.Name, a.Name, pa)
			}
			if len(a.Values) > 0 && !strings.Contains(pa.Description, workflow.Allowed(a.Values)) {
				t.Errorf("%s %s: description doesn't list the values: %q", w.Name, a.Name, pa.Description)
			}
		}
	}
}

// prompts/get answers the rendered text as one user message.
func TestMCPPromptGet(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "")
	args := map[string]string{"ticket": "VAQ-1"}
	res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "plan", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := workflow.Get("plan")
	want, _ := workflow.Render(p, args)
	if len(res.Messages) != 1 || res.Messages[0].Role != "user" {
		t.Fatalf("%+v", res.Messages)
	}
	if tc, ok := res.Messages[0].Content.(*mcp.TextContent); !ok || tc.Text != want {
		t.Fatalf("text: %+v", res.Messages[0].Content)
	}
	if res.Description != p.Description {
		t.Errorf("description %q", res.Description)
	}
}

// Review focus 1: a missing argument or an unknown kind is invalid params
// naming the argument and the allowed values, never a prompt with a blank.
func TestMCPPromptRefusals(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "")
	for _, c := range []struct {
		name string
		args map[string]string
		want string
	}{
		{"plan", nil, "Give ticket."},
		{"plan", map[string]string{"ticket": "\n"}, "Give ticket."},
		{"audit", map[string]string{"project": "VAQ", "kind": "perf"}, "Give kind: qa, hygiene, continuity or docs."},
		{"audit", map[string]string{"kind": "qa"}, "Give project."},
	} {
		res, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: c.name, Arguments: c.args})
		rpc, ok := errors.AsType[*jsonrpc.Error](err)
		if !ok || rpc.Code != jsonrpc.CodeInvalidParams || rpc.Message != c.want {
			t.Errorf("%s %v: %v %+v", c.name, c.args, err, res)
		}
	}
	if _, err := cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "work"}); err == nil {
		t.Error("an unshipped prompt answered")
	}
}

// Every prompts/get is in the MCP log as prompt:{name}; prompts/list isn't.
func TestMCPPromptsLogged(t *testing.T) {
	e, tok := agentEnv(t)
	cs := mcpConnect(t, e, tok, "builder")
	if _, err := cs.ListPrompts(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	_, _ = cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "plan", Arguments: map[string]string{"ticket": "VAQ-1"}})
	_, _ = cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "audit", Arguments: map[string]string{"project": "VAQ", "kind": "perf"}})
	_, _ = cs.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "nope"})
	rows, err := e.Deps.R.ListMCPCalls(bg, mcpListAll())
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ tool, args, outcome, message string }
	var got []row
	for _, r := range rows {
		if r.ActorName != "builder" || r.TokenPrefix != tok[:9] {
			t.Errorf("%+v", r)
		}
		got = append(got, row{r.Tool, r.Args, r.Outcome, r.Message})
	}
	want := []row{ // newest first
		{"prompt:nope", "", "unknown_prompt", `unknown prompt "nope"`},
		{"prompt:audit", "kind=perf project=VAQ", "bad_arguments", "Give kind: qa, hygiene, continuity or docs."},
		{"prompt:plan", "ticket=VAQ-1", "ok", ""},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("log:\n%+v\nwant\n%+v", got, want)
	}
}
