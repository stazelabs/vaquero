# Phase 2 design: API, CLI and agents

Date: 2026-11-19. Extends `2026-11-18-vaquero-design.md` (Phase 2). Status: draft for approval.

## Intent

Make vaquero usable by programs: scripts and the `vaquero` command line through an HTTP API, and agents through MCP. Several agents must be able to share one backlog without colliding, which is the coordination half: a ready queue, claims with expiring leases, and per-agent identity.

Phase 2 ships in two parts, each usable on its own:

- **2a, API and CLI**: API tokens, the REST API, the update and conflict rules below, the data-directory lock, and the `vaquero` CLI as an HTTP client.
- **2b, agents**: agent identity, the ready queue, claims (claim, heartbeat, release, expiry), MCP over HTTP plus the `vaquero mcp` stdio bridge, and claims on the board.

## Decided by the user

From the main design:

- Everything goes through one HTTP API. Locally the server binds loopback and the CLI starts it on demand.
- The CLI has profiles (the local instance and named remotes); every command accepts `--json`.
- MCP is Streamable HTTP at `/mcp` with bearer tokens; `vaquero mcp` is a thin stdio bridge to it.
- Local agents declare their own names; hosted agents get their own tokens (Phase 4).
- A claim sets the assignee, moves the ticket to the first in-progress status, records where it came from, and returns a lease. Heartbeat extends it, release gives it up keeping status and assignee, and expiry returns the ticket to the status it came from, unassigned, with an event saying the lease lapsed.

Decided on 2026-11-19:

- **Updates change only the named fields.** A field left out keeps its value.
- **A stale save is refused.** Every ticket carries a version. A save based on an older version is refused with the current ticket ("VAQ-12 changed since you read it"), and the caller re-reads and retries.
- **Link loops are refused.** "A blocks B" is refused when B already blocks A, directly or through a chain. "A duplicates B" is refused when B duplicates A. "Relates to" stays free.

## Builder's calls (proposed)

### Tokens

- An `api_tokens` table: the actor the token acts as, a display name, a prefix shown in lists (`vq_` plus 6 characters), the SHA-256 of the whole token, created, last used and revoked times. Tokens are 32 random bytes, base64url, prefixed `vq_`; they are high-entropy, so a plain SHA-256 is enough (no server secret to manage). A token is shown once, at creation.
- On first boot in local mode, vaquero mints a token for the owner named "local" and writes it to `{data dir}/token` (mode 0600). The CLI and the stdio bridge read it from there.
- The tokens page lists tokens (name, prefix, last used) and creates and revokes them. It is the first page of the admin section.

### The admin section (requested 2026-11-19)

Operator pages live under `/admin`, linked as "Admin" in the top bar, with their own side list. In local mode the owner is the admin; Phase 4 adds roles. 2a: the admin home (version, data directory, database size, schema version) and tokens. 2b: MCP activity. Later: backups, system details, users and roles.

### The data directory lock

`serve` takes an exclusive `flock` on `{data dir}/vaquero.lock` before opening the database and holds it until exit. A second server on the same data directory, on any port, exits with "Another vaquero is using {data dir} (pid N)." This closes the Phase 0 open question and is needed before the CLI starts servers on demand.

### The REST API (`/api/v1`)

- JSON in and out. Bearer token in `Authorization`. Every response carries `Vaquero-Version` (the server's release) and `Vaquero-Min-Client`; the CLI prints a plain "upgrade vaquero" message when it is older than the minimum.
- Errors: `{"error": {"code": "invalid" | "not_found" | "conflict" | "unauthorized" | "too_large", "message": "...", "fields": {...}}}` with 422, 404, 409, 401 and 413. A `conflict` carries the current object under `"current"`.
- Resources (2a): projects (list, get, create, update, change key), statuses and milestones (list), tickets (list with the board's filters plus `status`, get with tags, links, sub-tickets and comments, create, `PATCH` with optional `version`, move), comments (add, edit, delete), links (add, remove), search, and events (`GET /api/v1/events?after={seq}&limit=`, newest last, for agents that poll).
- Tickets are addressed by key (`/api/v1/tickets/VAQ-12`); ids never appear in URLs.
- The API is additive once released (main design).

### Core changes

- `tickets.version` (integer, starts at 1) rises on every change to the ticket's own fields, status or position. The web edit form carries it too, so a person editing in a browser while an agent changed the ticket sees the conflict instead of overwriting it.
- `core.PatchTicket(ctx, by, ticketID, patch, version)`: `patch` has a pointer per field (nil keeps it) and `AddTags`/`RemoveTags` alongside a full `Tags` replacement; `version` 0 skips the check. The web form and `UpdateTicket` become a full patch.
- `AddLink` refuses blocking loops (a recursive walk of "blocks" links) and two-way duplicates.

### The CLI

- Same binary. Commands (2a): `projects`, `project create|show|edit|key`, `tickets` (list, the board's filters as flags), `ticket show|new|edit|move`, `comment`, `link`/`unlink`, `search`, `activity`, `token create|list|revoke`, `profile add|list|use|remove`. Human-readable output by default; `--json` everywhere. Long text (`--body -`) reads standard input.
- Profiles live in `{user config dir}/vaquero/config.json` (mode 0600). The implicit `local` profile points at the local server and its token file; named profiles hold a URL and token for hosted instances (Phase 4).
- On demand: with the local profile, a refused connection starts `vaquero serve` in the background (its output to `{data dir}/vaquero.log`), waits up to 5 seconds for `/healthz`, then carries on. `vaquero stop` is not part of 2a.
- Exit codes: 0 success, 1 failure, 2 usage, 3 conflict (so scripts can retry).

### Agents (2b, outlined; planned after 2a ships)

- Agent identity: in local mode a `Vaquero-Agent: {name}` header on API and MCP requests names the actor; vaquero creates the agent actor on first use. Without it, the token's owner acts.
- Claims: `claimed_by`, `claim_expires_at`, `claim_prev_status_id` on tickets; `core.Claim`, `Heartbeat`, `Release`; a background sweep every 30 s expires leases. Default lease 15 minutes, set per claim up to 2 hours.
- Ready queue: the board's ready definition plus "no live claim", ordered by priority then position; `GET /api/v1/ready` and an MCP tool.
- MCP tools mirror the API one to one, sharing its readers and writers; the tool list is frozen by a test.
- Board cards show the claiming agent and the time left on its lease.
- **MCP activity** (requested 2026-11-19): every MCP tool call is logged to an `mcp_calls` table (time, actor, token prefix, tool, a short argument summary with no long text, outcome: ok or the error code and message, duration). `/admin/mcp` lists them newest first, filterable by agent and tool, and refreshes live. API calls can share the log later. Rows older than 30 days are pruned by the background sweep.

## Not in Phase 2

Hosted sign-in and per-user tokens beyond the local owner (Phase 4), webhooks (Phase 6), git integration (Phase 5).
