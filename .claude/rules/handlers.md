---
paths:
  - "internal/web/**/*.go"
---

# HTTP handlers

All routes mount in internal/web/router.go. Handlers are `func Name(d Deps) http.HandlerFunc` in internal/web/handlers. CLAUDE.md is the authority.

## Writes go through internal/core

Handlers never write directly: every change goes through `Deps.Core`, which writes its `events` row in the same transaction. `Deps` carries two writers outside core, `Touch` (a token's last use) and `MCPCalls` (the MCP log), for the token middleware and the MCP server only; `TestHandlersNeverWriteDirectly` fails on `gen.New(`, `.Exec(`, `BeginTx(`, `d.DB.`, `d.Touch.` or `d.MCPCalls.` in a handler (only /healthz pings `d.DB`). The web UI, and later the REST API and MCP, call the same core function, so they never disagree about what an operation does. Reads use `Deps.R`.

Answer a core error with `invalidOrError`: a `*core.Invalid` re-renders the form with 422 and its field messages; `core.ErrNotFound` goes home with "That no longer exists."; anything else is `serverError`. Small forms on a busy page (settings, comment edits) may instead redirect back with the message as an error toast.

## htmx fragments and the live stream

- A page part htmx swaps has an id and its own render path: the board's `BoardSection` (`GET /p/{key}/board`), or the page itself with `hx-select` (`#list`, `#feed`).
- An htmx request that fails because the page is stale answers `HX-Refresh: true` with an error toast queued (`refreshWith`), so the reload re-reads everything and shows the message.
- A page part that should follow changes carries `data-live` and `hx-trigger="vq:change"`; app.js keeps one stream per tab and triggers `vq:change` on every such part. Don't add another EventSource or htmx's SSE extension: each holds one of the browser's six connections to the host. The stream only announces that something changed; parts re-read through the normal handlers.
- A long-lived handler must clear its read and write deadlines (`http.NewResponseController`) and end on `d.Shutdown`.

## API handlers

- `api*.go` handlers answer JSON only: `api.WriteJSON`, `api.WriteFailure`, and `apiCoreFail` for core errors (a conflict carries the current ticket). Decode bodies with `api.Decode` (unknown fields refused). The actor is `apiActor(r)`, never `d.Actor`.
- Tickets are addressed by key and other objects by name in requests; internal ids appear only for comments and links, which have no key.
- The API is additive once released: add fields and endpoints, never rename or remove.

## MCP tools

- MCP tools mirror the API: each tool in `internal/mcpserver/tools.go` builds one `/api/v1` request, served in-process by the same routes (`mountAPI`) as the caller. Add the endpoint first, then the tool; never give a tool its own reads or writes.
- Tool names are snake_case verbs; the list is frozen by `TestMCPToolList`, each tool needs its row in the `mcp` help article (`TestMCPToolsDocumented`), and `TestMCPToolsMatchTheAPI` holds read tools to their REST answers.

## Admin pages

Operator pages live under `/admin`, rendered inside `templates.AdminShell`, listed in `templates.AdminPages`.

## Feedback

- Redirect after every successful POST (303). The one exception is creating an API token: the POST answer itself shows the new token, once, since it must never be in a redirect, URL or log.
- Success is a toast: `redirectWithFlash(w, r, flash.Success, msg, to)`.
- A missing entity redirects with a toast, never a 404.
- A web form body over the size limit gets the themed 413 page (`TooLarge`, passed to `middleware.BodyLimit`).
- An id in a URL (status, milestone, comment, link) must belong to the project or ticket in the same URL.

## Logging

- `d.Logger` is a `*slog.Logger`. Use the context forms: `d.Logger.ErrorContext(ctx, "create ticket", "err", err)`.
- Message first, then key-value pairs, with `"err"` for errors. Lowercase messages, no punctuation.
- Never log a token, a header or a request body.
- Log the cause and show the person a plain message; do not echo `err.Error()` into the page.

## Middleware order

Request ID, recover, host check, logger, security headers, CSP, CrossOrigin and BodyLimit are applied at the top of the router. A new route inherits them; don't re-add them.

## Related

Data rules: [database.md](database.md). Words: [copy-voice.md](copy-voice.md). Hosts and tokens: [security.md](security.md).
