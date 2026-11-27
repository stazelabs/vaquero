package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/workflow"
)

// CallLog keeps the MCP activity log (the mcp_calls table, read by
// /admin/mcp). It is a writer, but not of the tracker: core's one-event
// rule doesn't apply.
type CallLog interface {
	RecordMCPCall(ctx context.Context, arg gen.RecordMCPCallParams) error
}

// Outcomes besides "ok" and an API error code.
const (
	OutcomeUnknownTool  = "unknown_tool"
	OutcomeBadArguments = "bad_arguments"
	// OutcomeUnknownPrompt is a prompts/get naming no shipped prompt.
	OutcomeUnknownPrompt = "unknown_prompt"
)

const (
	maxValue   = 80  // runes of one argument's value kept in the summary
	maxSummary = 300 // runes of the whole summary
	maxMessage = 200 // runes of an error message
)

// note is where a tool leaves its refusal's code for the log.
type note struct{ code string }

type noteKey struct{}

func noteCode(ctx context.Context, code string) {
	if n, ok := ctx.Value(noteKey{}).(*note); ok {
		n.code = code
	}
}

// logCalls records every tools/call: the tool, who called it, with which
// token, a short summary of its arguments, how it ended and how long it
// took. A prompts/get is recorded the same way, as tool "prompt:{name}".
// A failure to record is logged and never fails the call.
func logCalls(log CallLog, logger *slog.Logger, now func() time.Time, actorID, prefix string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if log == nil {
				return next(ctx, method, req)
			}
			var tool, args string
			known := false
			switch r := req.(type) {
			case *mcp.CallToolRequest:
				if method != "tools/call" || r.Params == nil {
					return next(ctx, method, req)
				}
				tool, args, known = r.Params.Name, Summary(r.Params.Arguments), slices.Contains(toolNames, r.Params.Name)
			case *mcp.GetPromptRequest:
				if method != "prompts/get" || r.Params == nil {
					return next(ctx, method, req)
				}
				raw, _ := json.Marshal(r.Params.Arguments)
				if len(r.Params.Arguments) == 0 {
					raw = nil
				}
				_, known = workflow.Get(r.Params.Name)
				tool, args = "prompt:"+r.Params.Name, Summary(raw)
			default:
				return next(ctx, method, req)
			}
			n := &note{}
			start := now()
			res, err := next(context.WithValue(ctx, noteKey{}, n), method, req)
			outcome, msg := "ok", ""
			switch {
			case err != nil:
				outcome, msg = "error", err.Error()
				_, prompt := req.(*mcp.GetPromptRequest)
				_, invalid := errors.AsType[*jsonrpc.Error](err)
				switch {
				case !known && prompt:
					outcome = OutcomeUnknownPrompt
				case !known:
					outcome = OutcomeUnknownTool
				case prompt && invalid:
					outcome = OutcomeBadArguments
				}
			default:
				if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
					outcome, msg = n.code, resultText(r)
					if outcome == "" {
						outcome = OutcomeBadArguments
					}
				}
			}
			rec := gen.RecordMCPCallParams{
				ID: idgen.New(), At: timefmt.Format(start), ActorID: sql.NullString{String: actorID, Valid: actorID != ""},
				TokenPrefix: prefix, Tool: cut(tool, maxValue), Args: args,
				Outcome: outcome, Message: cut(msg, maxMessage), DurationMs: now().Sub(start).Milliseconds(),
			}
			wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			if werr := log.RecordMCPCall(wctx, rec); werr != nil {
				logger.WarnContext(ctx, "mcp call log", "err", werr, "tool", rec.Tool)
			}
			cancel()
			return res, err
		}
	}
}

func resultText(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return t.Text
		}
	}
	return ""
}

// Summary is a tool's arguments as the log shows them: key=value pairs in
// key order, a value with spaces quoted, every value cut to 80 characters
// and the whole to 300, so no long text (a description, a comment) is kept.
func Summary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return cut(string(raw), maxValue)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, cut(k, 40)+"="+value(args[k]))
	}
	return cut(strings.Join(parts, " "), maxSummary)
}

func value(v any) string {
	switch x := v.(type) {
	case string:
		s := cut(x, maxValue)
		if s == "" || strings.ContainsAny(s, " \t\n\"=") {
			return strconv.Quote(s)
		}
		return s
	case []any:
		items := make([]string, len(x))
		for i, it := range x {
			items[i] = value(it)
		}
		return cut("["+strings.Join(items, ",")+"]", maxValue)
	case map[string]any:
		return "{…}"
	case nil:
		return "null"
	}
	b, _ := json.Marshal(v)
	return cut(string(b), maxValue)
}

// cut keeps the first n runes of s, marking a cut with "…".
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
