package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/workflow"
)

// addPrompts registers every shipped workflow prompt. A prompt is embedded
// text, so prompts/get reads and writes nothing: it renders the prompt
// with workflow.Render, the same text `vaquero workflow` prints. A missing
// or disallowed argument is invalid params whose message names the
// argument and the values it takes ("Give kind: qa, hygiene, continuity or
// docs."), never a prompt with a blank in it.
func addPrompts(s *mcp.Server) {
	for _, p := range workflow.List() {
		mp := &mcp.Prompt{Name: p.Name, Title: p.Title, Description: p.Description}
		for _, a := range p.Args {
			desc := a.Description
			if len(a.Values) > 0 {
				desc += " One of " + workflow.Allowed(a.Values) + "."
			}
			mp.Arguments = append(mp.Arguments, &mcp.PromptArgument{Name: a.Name, Description: desc, Required: a.Required})
		}
		s.AddPrompt(mp, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			text, err := workflow.Render(p, req.Params.Arguments)
			if e, ok := errors.AsType[*workflow.ArgError](err); ok {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: e.Error()}
			}
			if err != nil {
				return nil, err
			}
			return &mcp.GetPromptResult{
				Description: p.Description,
				Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
			}, nil
		})
	}
}

// promptsLine is the instructions' sentence naming the prompts and their
// arguments: "Workflow prompts: plan (ticket) and audit (project, kind: qa,
// hygiene, continuity or docs); vaquero workflow NAME prints the same
// text."
func promptsLine() string {
	var names []string
	for _, p := range workflow.List() {
		var args []string
		for _, a := range p.Args {
			arg := a.Name
			if len(a.Values) > 0 {
				arg += ": " + workflow.Allowed(a.Values)
			}
			args = append(args, arg)
		}
		names = append(names, p.Name+" ("+strings.Join(args, ", ")+")")
	}
	return "Workflow prompts: " + workflow.Allowed(names) + "; vaquero workflow NAME prints the same text."
}
