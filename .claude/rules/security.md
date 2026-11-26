---
paths:
  - "internal/web/middleware/**"
  - "internal/web/router.go"
  - "internal/config/**"
  - "cmd/vaquero/serve.go"
---

# Security

CLAUDE.md "Hard invariants" is the authority.

## Local mode

- Local mode has no sign-in, so it must never be reachable from the network. `config.FromEnv` refuses a non-loopback `VQ_ADDR` in local mode; do not relax that.
- `middleware.HostCheck` stays first after `Recover`. Its refusals are logged five a minute, then counted; logged paths are cut to 200 characters. It refuses any Host other than `localhost`, `127.0.0.1` and `::1`, which is the defence against DNS rebinding (a foreign site pointing its hostname at 127.0.0.1).
- The data directory is mode 0700; it will hold the local API token.
- `VQ_MODE=hosted` is refused until Phase 4 adds sign-in.

## The API and tokens

- `/api/v1` sits in its own chain: `APIHeaders` (no-store, `Vaquero-Version`, `Vaquero-Min-Client`) then `APIToken`. A token is a bearer credential; store only `apitoken.Hash` and the prefix, show the raw value once (the POST answer, never a redirect, URL or log).
- Every refusal (no token, malformed, unknown, revoked) is the same 401 body and `WWW-Authenticate`; the reason is logged with the token id only. `TestAPIRefusesBadTokens` holds it.
- The token middleware's last-used write (at most every 5 minutes) and the MCP activity log (`Deps.MCPCalls`, the `mcp_calls` table) are the only writes outside core; neither touches a tracker table.
- Agent identity: in local mode only, a request made with the owner's token and a `Vaquero-Agent` header acts as that agent (`core.EnsureAgent` creates it on first use; a bad name is a 400). Any other token keeps its own actor whatever the header says. Hosted mode will give agents their own tokens instead (Phase 4).

## MCP

- `POST /mcp` sits behind the same `APIHeaders` and `APIToken` as `/api/v1`, so the same token, the same 401 and the same agent rule apply. Other methods get the API's JSON 405.
- Each tool is one `/api/v1` request served in-process as the request's actor: a tool can do exactly what its REST call can, no more.
- Batches are refused before the SDK (one POST is one call); the body limit applies; a tool's panic is recovered into "Something went wrong." and logged.
- The activity log keeps a tool's name, actor, token prefix (never the token), a summary of its arguments with every value cut to 80 characters, the outcome and the duration. It is pruned after 30 days by the sweep.
- Per-token rate limits arrive with hosted mode (Phase 4); locally the only client is you.

## Browser protections

- CSP (middleware/csp.go): per-request nonce, `script-src 'self' 'nonce-...'`, `style-src 'self'`, `frame-ancestors 'none'`, `form-action 'self'`. So: no inline script, no `style=` attributes. JS lives in internal/web/static/js/app.js. Inline script tags, if ever needed, use `templ.GetNonce`.
- Security headers (middleware/security.go): nosniff, X-Frame-Options DENY, Referrer-Policy, Permissions-Policy, COOP; HSTS only when served over https (hosted mode).
- CSRF: `middleware.CrossOrigin` (crossorigin.go) uses `http.NewCrossOriginProtection` (Sec-Fetch-Site / Origin). Forms carry no CSRF field. Requests with neither header (non-browser clients) pass. Do not add hidden-token CSRF, and add no bypass without the owner's say-so.

## Body limits

`middleware.BodyLimit(MaxBody, handlers.TooLarge(d))`: 1 MiB for every request, except an upload route (`middleware.IsUpload`, its list in bodylimit.go, each with its method and limit): an attachment POST gets `MaxUpload` (10 MB plus 64 KiB of multipart framing), a project icon (`POST /p/*/icon`, `PUT /api/v1/projects/*/icon`) `MaxIconUpload` (256 KB plus 64 KiB). An attachment over the limit, declared or read, answers 413 in the API envelope with "Files can be at most 10 MB." (`middleware.TooLargeUpload`), never a bare 413; an icon "Use an image of 256 KB or less." (`TooLargeIcon`), except that the web icon form's declared 413 is the themed page and a read one returns to settings with that toast. A declared body over the limit elsewhere answers the API envelope (`too_large`) on `/api/v1` and `/mcp`, and the themed 413 page on every other route (a small plain page when none is passed), never plain text.

## Attachments

`GET /attachments/{id}/{name}` serves user bytes from the app's origin. The type is sniffed from the bytes at upload (`http.DetectContentType`), never taken from the client. Only PNG, JPEG, GIF and WebP are inline; everything else, SVG included, is `application/octet-stream` with `Content-Disposition: attachment`. `GET /api/v1/attachments/{id}` (token required) shares the code. Every answer from these routes carries `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox` (replacing the page CSP). Markdown renders `<img>` only for `/attachments/` destinations. A project icon image (`GET /p/{key}/icon`) is served the same way: type sniffed at upload (PNG, JPEG or WebP only, SVG refused), its etag, `private, no-cache`, nosniff and `Content-Security-Policy: sandbox`.

## Outbound network

Local-first: everything works offline with the defaults; network services are optional and used only when the operator configures them (today, an S3 bucket for attachments via `VQ_S3_*`; the secret is never logged). No telemetry; Vaquero never downloads or checks for releases.
