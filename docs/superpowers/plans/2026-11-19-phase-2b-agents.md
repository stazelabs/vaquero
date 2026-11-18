# Phase 2b: Agents — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Several agents can share one backlog: each names itself, takes work from a ready queue by claiming it with an expiring lease, and talks to vaquero over MCP (HTTP, or stdio through `vaquero mcp`); every MCP call is logged for the admin, and the board shows who holds what.

**Architecture:** One migration adds claim columns to tickets and an `mcp_calls` log. Core gains `EnsureAgent`, `Claim`, `Heartbeat`, `Release` and `ExpireClaims`. The API middleware maps a `Vaquero-Agent` header to an agent actor in local mode. A background sweeper expires leases and prunes the log. `internal/mcpserver` builds a stateless Streamable HTTP MCP server per request whose tools call the same readers and core as the API; a wrapper logs each call. `vaquero mcp` bridges stdio to `/mcp`.

**Tech Stack:** As Phase 2a, plus `github.com/modelcontextprotocol/go-sdk v1.8.0` (healthcheck-email's version).

**Spec:** `docs/superpowers/specs/2026-11-19-phase-2-api-cli-agents-design.md` (the 2b outline and the MCP activity log). Follows `docs/superpowers/plans/2026-11-19-icons-and-help.md` (the help pages and icons this plan adds to).

**Patterns to follow:** healthcheck-email's `internal/mcpserver` (per-request server, the generic `add` wrapper with `recoverTool`, `Stateless: true, JSONResponse: true`, batches refused, `TestMCPToolList` freezing names, test helpers `mcpConnect`/`mcpCall`) and `internal/sweep/sweep.go` (`Start`/`Stop`/`Tick`). Read-only.

## Global Constraints

- CLAUDE.md invariants apply; only core writes tracker tables; one event per successful operation, with one named exception below.
- Migration `0006_agents.sql` only.
- Lease: default 15 minutes, at least 1 minute, at most 2 hours.
- **Heartbeats write no event** (they would flood the history); the property test exempts `Heartbeat`, documented in `core/events.go`. Every other claim operation writes exactly one.
- MCP tools mirror the API: same names for things, same errors (as tool errors), same actor rules. Tool names are snake_case verbs.
- An agent name: 1–80 characters, letters, digits, spaces, `.`, `_`, `-`; no control characters.
- Agent identity from the header only in local mode, and only when the token belongs to the owner; otherwise the header is ignored.
- Gate steps separately, by exit code; `make ci` ends each task. Commits `{Area}: {summary}` with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Two agents claim the same ready ticket at once.** Exactly one wins; the other gets a clear refusal naming the holder and when the lease ends. (Task 3.)
2. **An agent claims, then dies.** After the lease, the sweep returns the ticket to the status it came from, unassigned, with an event saying the claim lapsed, and it is ready again. (Tasks 3 and 4.)
3. **An agent heartbeats or releases a ticket someone else holds, or one whose lease already lapsed.** Refused; nothing changes. (Task 3.)
4. **An MCP client sends a batch, an unknown tool, bad arguments or no token.** A JSON-RPC error or tool error, never a 500 or a panic; the call is logged. (Tasks 6 and 7.)
5. **`vaquero mcp` runs while the server is down, or the client closes stdin mid-request.** The bridge starts the server or exits with a plain message on stderr (stdout carries only JSON-RPC); closing stdin ends it promptly. (Task 8.)

---

### Task 1: Folded backlog items

**Files:** `cmd/vaquero/cli.go`, `cli_cmds.go`, `cli_test.go`, `docs/BACKLOG.md`

- [ ] **Tests first:** `TestCLIConflictExitsThreeWithoutCurrent` (a 409 `conflict` answer with no `current` still exits 3), `TestCLIVersionMustBePositive` (`--version 0` and `--version -3` are usage errors, exit 2).
- [ ] Implement: exit 3 keys on `Code == "conflict"`; `--version` given and below 1 is a usage error. Tick both backlog lines. Gate; commit — `"CLI: conflicts by code; --version must be positive"`

### Task 2: Agent identity

**Files:** `internal/core/actors.go` (+test), `internal/web/middleware/apitoken.go` (+test), `internal/client/client.go` (already sends `Vaquero-Agent`), `cmd/vaquero/cli.go` (`--agent NAME` flag, falls back to `VAQUERO_AGENT`)

```go
// EnsureAgent returns the agent actor called name, creating it (with an
// actor.created event) on first use.
func (c *Core) EnsureAgent(ctx context.Context, by, name string) (gen.Actor, error) // Invalid{"agent": …} for a bad name
```

APIToken: when `cfg.Mode == local`, the token's actor is the owner and `Vaquero-Agent` is non-empty, the request acts as `EnsureAgent(header)`. A bad name is a 400 `bad_request` ("Agent names are 1 to 80 letters, digits, spaces, dots, dashes or underscores.").

- [ ] **Tests first:** core `TestEnsureAgentIdempotent` (one actor, one event, kind agent), `TestAgentNameRules`; middleware/API `TestAgentHeaderActsAsAgent` (a comment through the API with `Vaquero-Agent: builder` is authored by "builder", kind agent), `TestAgentHeaderIgnoredForNonOwnerToken`, `TestBadAgentName400`; CLI `TestCLIAgentFlag`.
- [ ] Implement, gate, commit — `"Agents: identity from Vaquero-Agent in local mode"`

### Task 3: Claims in core

**Files:** `internal/db/migrations/0006_agents.sql`, `internal/db/queries/claims.sql`, `internal/core/claims.go` (+test, `ops` entries), `internal/core/events.go`, `internal/web/templates/activity.go` (wording, icons)

```sql
ALTER TABLE tickets ADD COLUMN claimed_by TEXT REFERENCES actors(id);
ALTER TABLE tickets ADD COLUMN claim_expires_at TEXT;
ALTER TABLE tickets ADD COLUMN claim_prev_status_id TEXT REFERENCES statuses(id);
CREATE INDEX tickets_claim_expiry ON tickets (claim_expires_at) WHERE claimed_by IS NOT NULL;
CREATE TABLE mcp_calls (id TEXT PRIMARY KEY, at TEXT NOT NULL, actor_id TEXT REFERENCES actors(id), token_prefix TEXT NOT NULL DEFAULT '',
  tool TEXT NOT NULL, args TEXT NOT NULL DEFAULT '', outcome TEXT NOT NULL, message TEXT NOT NULL DEFAULT '', duration_ms INTEGER NOT NULL);
CREATE INDEX mcp_calls_at ON mcp_calls (at);
```

```go
const DefaultLease, MinLease, MaxLease = 15 * time.Minute, time.Minute, 2 * time.Hour
// Claim gives a ticket to by: assignee by, status the project's first
// in-progress status, the status it came from kept, lease until now+lease.
// Refused when another live claim holds it ("VAQ-3 is claimed by builder
// until 14:05."), when it is done or cancelled, or assigned to someone else.
// Claiming one's own live claim renews it.
func (c *Core) Claim(ctx context.Context, by, ticketID string, lease time.Duration) (gen.Ticket, error)
func (c *Core) Heartbeat(ctx context.Context, by, ticketID string, lease time.Duration) (gen.Ticket, error) // no event
func (c *Core) Release(ctx context.Context, by, ticketID string) (gen.Ticket, error) // keeps status and assignee
// ExpireClaims returns every lapsed claim's ticket to the status it came
// from, unassigned, one event each (actor: the claimant). It returns how many.
func (c *Core) ExpireClaims(ctx context.Context) (int, error)
const EvTicketClaimed, EvTicketReleased, EvClaimLapsed = "ticket.claimed", "ticket.released", "ticket.claim_lapsed"
```

The ready definition (board query and Task 4's queue) adds "no live claim". Claims raise the ticket's version.

- [ ] **Tests first:** `TestClaimMovesAndAssigns`, `TestConcurrentClaimsOneWins` (Review Focus 1: 10 goroutines as 10 agents, one success, nine `Invalid` naming the winner), `TestClaimRefusals` (done, cancelled, someone else's assignment, someone else's live claim), `TestOwnClaimRenews`, `TestHeartbeatAndReleaseRules` (Review Focus 3: not the claimant, lapsed lease), `TestHeartbeatWritesNoEvent`, `TestExpireReturnsTicket` (Review Focus 2: previous status, unassigned, event, ready again), `TestLeaseBounds`, `ops` entries for Claim and Release (Heartbeat exempted with a comment).
- [ ] Implement, gate, commit — `"Core: claims with leases"`

### Task 4: Ready queue, claim API and CLI, sweeper

**Files:** `internal/db/queries/claims.sql` (`ListReady`), `internal/web/handlers/api_claims.go` (+test), router, `internal/sweep/sweep.go` (+test), `cmd/vaquero/serve.go`, `cli_cmds.go` (+tests), `internal/api/types.go` (`TicketSummary` gains `ClaimedBy`, `ClaimExpiresAt`)

Routes: `GET /api/v1/ready?project=KEY&limit=` (priority urgent→low then none, then board position), `POST /api/v1/tickets/{ticket}/claim` `{lease_minutes?}`, `POST …/heartbeat` `{lease_minutes?}`, `POST …/release`. CLI: `vaquero ready [KEY] [--limit N]`, `vaquero claim TICKET [--lease 30m]`, `vaquero heartbeat TICKET [--lease 30m]`, `vaquero release TICKET`. Sweeper: every 30 s, `ExpireClaims`, then delete `mcp_calls` older than 30 days; started and stopped by `serve` (the 30 s drain stops it first).

- [ ] **Tests first:** `TestAPIReadyOrder`, `TestAPIClaimConflict` (second claimant 422 with the holder in the message), `TestAPIHeartbeatExtends`, `TestSweepExpires` (`Tick` with a moved clock), `TestServeStopsSweeper`, CLI `TestCLIClaimFlow` (`ready` → `claim` → `heartbeat` → `release`, `--json`).
- [ ] Implement, gate, commit — `"Agents: ready queue, claim API and CLI, lease sweep"`

### Task 5: Claims on the board

**Files:** `internal/board/board.go` (`Card.ClaimedBy`, `ClaimExpiresAt`), `board.templ`, `app.js` (countdown: every 30 s rewrite `[data-claim-expires]` as "12 min left"), tests

- [ ] **Tests first:** `TestBoardShowsClaim` (the card shows the agent with the `bot` glyph and `data-claim-expires`), `TestClaimedTicketNotReady`.
- [ ] Implement; check in Chrome that the countdown ticks and the card leaves the claim when it lapses (live refresh); gate; commit — `"Board: claims and their countdowns"`

### Task 6: MCP server

**Files:** `go.mod`, `internal/mcpserver/mcpserver.go`, `tools.go` (+test), `internal/web/router.go` (`POST /mcp` behind `APIHeaders`, `APIToken`), `internal/web/handlers/mcp_test.go`

Tools (input structs with `json` and `jsonschema:"…"` descriptions; read-only ones annotated read-only): `list_projects`, `list_tickets` {project, status?, tag?, mine?, open?, limit?}, `get_ticket` {key}, `ready_tickets` {project?, limit?}, `search` {query}, `recent_activity` {after?, newest?}, `create_ticket` {project, title, body?, status?, priority?, tags?, parent?}, `update_ticket` {key, fields…, version?}, `move_ticket` {key, status}, `add_comment` {key, body}, `link_tickets` {key, kind, to}, `claim_ticket` {key, lease_minutes?}, `heartbeat` {key, lease_minutes?}, `release_ticket` {key}. Results are the API's JSON objects as text content plus structured output. The server's `Instructions` explain the claim loop in four sentences.

- [ ] **Tests first:** `TestMCPToolList` (frozen), `TestMCPClaimLoop` (with `mcpConnect`: `ready_tickets` → `claim_ticket` → `add_comment` → `release_ticket`, the comment authored by the agent from `Vaquero-Agent`), `TestMCPRefusals` (Review Focus 4: no token 401; a batch refused; unknown tool; bad arguments; a core refusal is a tool error with the message), `TestMCPToolPanicRecovered`, `TestMCPToolsMatchTheAPI` (each tool's JSON equals the matching API answer).
- [ ] Implement, gate, commit — `"MCP: the server at /mcp"`

### Task 7: MCP activity log and admin page

**Files:** `internal/mcpserver/log.go` (the wrapper records tool, actor, token prefix, an argument summary — keys and short values, long text cut to 80 characters — outcome `ok` or the error code, the message, duration), `internal/db/queries/mcp.sql`, `internal/web/handlers/admin_mcp.go` (+test), `admin.templ` (`/admin/mcp` in `AdminPages`), live refresh (`data-live`)

Page: newest first, 100 per page with "Older" paging by time, filters by agent and tool (from the rows present), each row: time, agent (glyph), tool, argument summary, outcome badge, duration.

- [ ] **Tests first:** `TestMCPCallsLogged` (ok and error calls each write one row with the right fields; no body text over 80 characters; no token), `TestAdminMCPPage` (rows, filters, live attributes), `TestMCPLogPruned` (the sweeper deletes rows over 30 days).
- [ ] Implement, gate, commit — `"Admin: MCP activity"`

### Task 8: `vaquero mcp` stdio bridge

**Files:** `cmd/vaquero/mcp.go` (+test)

`vaquero mcp [--profile NAME] [--agent NAME]`: connects like other commands (starting the local server when needed); then reads newline-delimited JSON-RPC messages from stdin, POSTs each to `/mcp` with the token and `Vaquero-Agent`, and writes each JSON response on its own line to stdout (a 202 with no body writes nothing). Every diagnostic goes to stderr. EOF on stdin ends it with exit 0. A server error becomes a JSON-RPC error response for that request id.

- [ ] **Tests first:** `TestMCPBridgeRoundTrip` (initialize, `tools/list`, `tools/call` through pipes against a webtest server), `TestMCPBridgeStdoutClean` (only JSON lines on stdout), `TestMCPBridgeEOF` (closing stdin exits promptly, Review Focus 5), `TestMCPBridgeServerDown` (exit 1, message on stderr).
- [ ] Implement, gate, commit — `"CLI: vaquero mcp, a stdio bridge"`

### Task 9: Help, docs and check

- [ ] Help articles: `agents-and-claims` (identity, the ready queue, claim/heartbeat/release, leases, expiry, what the board shows), `mcp` (connecting a client: the HTTP endpoint with a token, or `vaquero mcp` in a client's config, with a worked example for Claude Code; the tools; the activity log), update `api` and `cli`; the article tests extend to MCP tool names.
- [ ] CLAUDE.md, `.claude/rules/handlers.md` (MCP tools mirror the API), `security.md` (MCP auth, agent header rules), BACKLOG.
- [ ] `make ci`. Hand check: `claude mcp add vaquero -- vaquero mcp --agent builder` (or the bridge driven by hand with JSON-RPC lines), work a ticket through ready → claim → comment → release while the board and `/admin/mcp` are open in Chrome; let a lease lapse with a 1-minute lease and watch it return. Commit — `"Docs: phase 2b"`
