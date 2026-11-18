---
paths:
  - "internal/web/**/*.go"
---

# HTTP handlers

All routes mount in internal/web/router.go. Handlers are `func Name(d Deps) http.HandlerFunc` in internal/web/handlers. CLAUDE.md is the authority.

## Writes go through internal/core

Handlers never write directly: every change goes through `Deps.Core`, which writes its `events` row in the same transaction. `Deps` has no writer queries, and `TestHandlersNeverWriteDirectly` fails on `gen.New(`, `.Exec(`, `BeginTx(` or `d.DB.` in a handler (only /healthz pings `d.DB`). The web UI, and later the REST API and MCP, call the same core function, so they never disagree about what an operation does. Reads use `Deps.R`.

Answer a core error with `invalidOrError`: a `*core.Invalid` re-renders the form with 422 and its field messages; `core.ErrNotFound` goes home with "That no longer exists."; anything else is `serverError`. Small forms on a busy page (settings, comment edits) may instead redirect back with the message as an error toast.

## Feedback

- Redirect after every successful POST (303).
- Success is a toast: `redirectWithFlash(w, r, flash.Success, msg, to)`.
- A missing entity redirects with a toast, never a 404.
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
