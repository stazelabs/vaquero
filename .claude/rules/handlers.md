---
paths:
  - "internal/web/**/*.go"
---

# HTTP handlers

All routes mount in internal/web/router.go. Handlers are `func Name(d Deps) http.HandlerFunc` in internal/web/handlers. CLAUDE.md is the authority.

## Writes go through internal/core

Once internal/core exists (Phase 1), handlers must not call writer queries directly: every mutation goes through core, which writes its `events` row in the same transaction. The web UI, REST API and MCP all call the same core function, so the three never disagree about what an operation does. Reads may use `Deps.R` directly.

## Feedback

- Redirect after every successful POST (303).
- A missing entity redirects with a flash, never a 404 (once flash exists).

## Logging

- `d.Logger` is a `*slog.Logger`. Use the context forms: `d.Logger.ErrorContext(ctx, "create ticket", "err", err)`.
- Message first, then key-value pairs, with `"err"` for errors. Lowercase messages, no punctuation.
- Never log a token, a header or a request body.
- Log the cause and show the person a plain message; do not echo `err.Error()` into the page.

## Middleware order

Request ID, recover, host check, logger, security headers, CSP, CrossOrigin and BodyLimit are applied at the top of the router. A new route inherits them; don't re-add them.

## Related

Data rules: [database.md](database.md). Words: [copy-voice.md](copy-voice.md). Hosts and tokens: [security.md](security.md).
