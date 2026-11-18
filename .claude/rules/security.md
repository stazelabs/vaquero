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
- `middleware.HostCheck` stays first after `Recover`. It refuses any Host other than `localhost`, `127.0.0.1` and `::1`, which is the defence against DNS rebinding (a foreign site pointing its hostname at 127.0.0.1).
- The data directory is mode 0700; it will hold the local API token.
- `VQ_MODE=hosted` is refused until Phase 4 adds sign-in.

## The API and tokens

- `/api/v1` sits in its own chain: `APIHeaders` (no-store, `Vaquero-Version`, `Vaquero-Min-Client`) then `APIToken`. A token is a bearer credential; store only `apitoken.Hash` and the prefix, show the raw value once (the POST answer, never a redirect, URL or log).
- Every refusal (no token, malformed, unknown, revoked) is the same 401 body and `WWW-Authenticate`; the reason is logged with the token id only. `TestAPIRefusesBadTokens` holds it.
- The token middleware's last-used write (at most every 5 minutes) is the only write outside core, through `Deps.Touch`.
- Per-token rate limits arrive with hosted mode (Phase 4); locally the only client is you.

## Browser protections

- CSP (middleware/csp.go): per-request nonce, `script-src 'self' 'nonce-...'`, `style-src 'self'`, `frame-ancestors 'none'`, `form-action 'self'`. So: no inline script, no `style=` attributes. JS lives in internal/web/static/js/app.js. Inline script tags, if ever needed, use `templ.GetNonce`.
- Security headers (middleware/security.go): nosniff, X-Frame-Options DENY, Referrer-Policy, Permissions-Policy, COOP; HSTS only when served over https (hosted mode).
- CSRF: `middleware.CrossOrigin` (crossorigin.go) uses `http.NewCrossOriginProtection` (Sec-Fetch-Site / Origin). Forms carry no CSRF field. Requests with neither header (non-browser clients) pass. Do not add hidden-token CSRF, and add no bypass without the owner's say-so.

## Body limits

`middleware.BodyLimit(MaxBody)`: 1 MiB for every request.

## Outbound network

None. No telemetry. A later update-available check must be optional and documented.
