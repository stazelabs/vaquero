// Package mcpserver is Vaquero's API as an MCP server: Streamable HTTP at
// POST /mcp, stateless, plain JSON answers, behind the same bearer token
// (and the same Vaquero-Agent rule) as /api/v1. vaquero mcp bridges stdio
// to it.
//
// Each tool is one /api/v1 request, served in-process by the same router
// and handlers as REST, as the same actor: so the two can't disagree about
// names, rules or errors. A tool's text is the REST body byte for byte and
// its structured content the same JSON; a REST refusal is a tool error
// whose text is the refusal's message. The tool list is frozen by
// TestMCPToolList and its answers held to REST by TestMCPToolsMatchTheAPI.
//
// In front of the SDK, Handler answers in the API's JSON envelope what it
// can check first: the wrong Content-Type (415), an Accept without both
// answers it may send (400), an empty body (400), a body over the limit
// (413) and a JSON-RPC batch (400, "Send one request at a time."). Every
// tool recovers a panic into "Something went wrong.", logged.
package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/version"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// Instructions is what a client is told the server is for.
const Instructions = "Vaquero is a ticket tracker shared by people and agents; tickets are named by key, like VAQ-12. " +
	"To take work, call ready_tickets, then claim_ticket on one: a claim assigns it to you and lasts for a lease (15 minutes unless you ask for up to 120). " +
	"While you work, call heartbeat before the lease ends, record progress with add_comment and update_ticket, and move_ticket it when it is done. " +
	"Call release_ticket when you stop; a claim you stop renewing lapses and the ticket goes back to the queue. " +
	"To work several tickets at once, bulk_update claims, renews, updates, comments on, moves and releases up to 100 in one call, " +
	"and view \"summary\" on a write answers with a short summary instead of the whole ticket. " +
	"In a project with the agent workflow on, write a ticket's plan with save_plan, park questions and approvals for the owner with ask_decision, " +
	"record the owner's answer with answer_decision only as relayed, quoting their words (run vaquero mcp with --agent NAME), " +
	"and ready_tickets lists only tickets that pass readiness: a current plan approval."

// Handler is POST /mcp. api serves /api/v1 without the token check (the
// router's API routes); the request's actor, set by middleware.APIToken in
// front of Handler, is passed to it on every tool call.
//
// Every tool call is recorded in calls (nil: none), timed by now.
func Handler(apiRoutes http.Handler, logger *slog.Logger, calls CallLog, now func() time.Time) http.Handler {
	cache := mcp.NewSchemaCache()
	sdk := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		s, _ := r.Context().Value(serverKey{}).(*mcp.Server)
		return s
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: middleware.MaxBody})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		actor, ok := middleware.APIActor(ctx)
		if !ok {
			logger.ErrorContext(ctx, "mcp without an actor", "request_id", middleware.GetRequestID(ctx))
			api.WriteFailure(w, api.Internal())
			return
		}
		if f := precheck(r); f != nil {
			api.WriteFailure(w, f)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				api.WriteFailure(w, api.Fail(http.StatusRequestEntityTooLarge, "too_large", "The request body is too large."))
				return
			}
			api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", "The request couldn't be read."))
			return
		}
		switch first := bytes.TrimLeft(body, " \t\r\n"); {
		case len(first) == 0:
			api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", "Send one JSON-RPC request as the body."))
			return
		case first[0] == '[':
			api.WriteFailure(w, api.Fail(http.StatusBadRequest, "bad_request", "Send one request at a time."))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		caller := &caller{api: apiRoutes, actor: actor, origin: core.OriginFrom(ctx), logger: logger.With("request_id", middleware.GetRequestID(ctx))}
		s := mcp.NewServer(&mcp.Implementation{Name: "vaquero", Title: "Vaquero", Version: version.Version}, &mcp.ServerOptions{
			Instructions: Instructions,
			Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
			SchemaCache:  cache,
		})
		s.AddReceivingMiddleware(logCalls(calls, logger, now, actor.ID, middleware.APITokenPrefix(ctx)))
		addTools(s, caller)
		sdk.ServeHTTP(w, r.WithContext(context.WithValue(ctx, serverKey{}, s)))
	})
}

// serverKey holds the request's server on its context: the SDK asks for it
// twice per request, and it is built once.
type serverKey struct{}

// precheck refuses, as the SDK would, a request with the wrong
// Content-Type or an Accept without both answers it may send.
func precheck(r *http.Request) *api.Failure {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return api.Fail(http.StatusUnsupportedMediaType, "bad_request", "Send the request as application/json.")
	}
	var jsonOK, streamOK bool
	for _, v := range r.Header.Values("Accept") {
		for raw := range strings.SplitSeq(v, ",") {
			base, _, _ := strings.Cut(raw, ";")
			switch strings.ToLower(strings.TrimSpace(base)) {
			case "application/json", "application/*":
				jsonOK = true
			case "text/event-stream", "text/*":
				streamOK = true
			case "*/*":
				jsonOK, streamOK = true, true
			}
		}
	}
	if !jsonOK || !streamOK {
		return api.Fail(http.StatusBadRequest, "bad_request", "Accept must include application/json and text/event-stream.")
	}
	return nil
}
