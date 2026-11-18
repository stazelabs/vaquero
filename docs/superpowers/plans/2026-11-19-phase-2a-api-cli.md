# Phase 2a: API and CLI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Programs can use vaquero: a token-authenticated REST API at `/api/v1`, and a `vaquero` CLI that talks to it (starting the local server when needed), with partial updates, stale-save refusal and link-loop refusal in core.

**Architecture:** One migration adds `api_tokens` and `tickets.version`. Core gains token operations, `PatchTicket` with a version check, and loop checks in `AddLink`. A small `internal/apitoken` package mints, parses and hashes tokens; `internal/api` holds JSON types and error writing; API handlers live beside the web handlers and call the same core and reader queries. `serve` holds a lock on the data directory and keeps a local token in `{data dir}/token`. The CLI is the same binary: `internal/client` is the HTTP client and profile store, and `cmd/vaquero` adds the commands.

**Tech Stack:** As Phase 1. No new dependencies (stdlib `flag`, `net/http`, `encoding/json`, `syscall.Flock`).

**Spec:** `docs/superpowers/specs/2026-11-19-phase-2-api-cli-agents-design.md` (approved 2026-11-19), extending `docs/superpowers/specs/2026-11-18-vaquero-design.md`.

**Patterns to follow:** healthcheck-email's `internal/apitoken/apitoken.go` (Mint, Valid, Parse, Prefix, touch at most every 5 minutes), `internal/web/middleware/apitoken.go` (bearer middleware: one identical 401 for every refusal, `WWW-Authenticate: Bearer realm="api", error="invalid_token"`, the reason logged with the token id only), `internal/api/api.go` (`Encode`, `WriteJSON`, `WriteError`, `Failure`) and the API test helpers in `internal/web/handlers/api_test.go`. Read them in `/Users/cstaszak/Stazelabs/projects/healthcheck-email`; that project is read-only.

## Global Constraints

- CLAUDE.md "Hard invariants" apply. Only core writes tracker tables (now including `api_tokens`), one event per successful operation, generated files committed.
- Migrations append-only: this plan adds `0005_api.sql`.
- Tokens: `vq_` + 43 base64url characters (32 random bytes). Store only `sha256` (base64url) and the 9-character prefix. Never log a token or put one in a URL. Show a new token once.
- API: JSON only; the error shape `{"error": {"code", "message", "fields"?, "current"?}}`; every `/api/v1` response sets `Cache-Control: no-store`, `Vaquero-Version` and `Vaquero-Min-Client` (both `version.Version`'s value; the minimum stays `0.0.0` until a release needs more).
- Tickets are addressed by key in every API path and CLI argument; internal ids never appear in API output.
- Gate steps run separately and are judged by exit code; commit only after exit 0. `make ci` ends each task.
- Copy rules apply to CLI output too: sentence case, no exclamation points, errors say what to do.
- Commit messages `{Area}: {summary}` ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Deferred work goes to `docs/BACKLOG.md`.

## Review Focus

1. **Two clients save the same ticket from the same version.** The second gets 409 `conflict` with the current ticket, nothing it sent is written, and the CLI exits 3. (Tasks 2 and 6.)
2. **A request carries no token, a malformed one, an unknown one or a revoked one.** Each gets the same 401 body and header; no token appears in the log. (Task 4.)
3. **A second `vaquero serve` starts on another port against the same data directory.** It refuses with the holder's pid; the first keeps serving. (Task 1.)
4. **Someone links C blocks A when A blocks B and B blocks C.** Refused, naming the loop; "relates to" in the same shape is allowed. (Task 2.)
5. **The CLI runs while the local server is down, and the port is taken by another program.** It tries to start vaquero, gives up within about 5 seconds with a message naming the address, and exits 1, never hanging. (Task 8.)

---

### Task 1: Data-directory lock, tokens, local token file

**Files:** `internal/db/migrations/0005_api.sql`, `internal/db/queries/tokens.sql`, `internal/apitoken/apitoken.go` (+test), `internal/lockfile/lockfile.go` (+test), `internal/core/tokens.go` (+test, `ops` entries), `cmd/vaquero/serve.go`, `cmd/vaquero/serve_test.go`

**Interfaces:**

```sql
CREATE TABLE api_tokens (
  id TEXT PRIMARY KEY, actor_id TEXT NOT NULL REFERENCES actors(id),
  name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
  prefix TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT);
ALTER TABLE tickets ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
```

Queries: `CreateToken`, `GetTokenByHash`, `GetToken`, `ListTokens` (newest first, with actor name), `RevokeToken`, `TouchToken` (`UPDATE … SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`).

```go
package apitoken
const Scheme = "vq_"
const TouchEvery = 5 * time.Minute
func Mint() (string, error)
func Valid(raw string) bool
func Parse(header string) (string, bool)     // "Bearer <tok>", scheme case-insensitive
func Prefix(raw string) string               // "vq_" + 6
func Hash(raw string) string                 // sha256, base64url

package lockfile
var ErrLocked = errors.New("locked")
// Acquire takes an exclusive, non-blocking flock on dir/vaquero.lock and
// writes the pid into it. Held is the pid of the holder when ErrLocked.
func Acquire(dir string) (release func(), held int, err error)

package core
// CreateToken mints a token for actorID; raw is returned once and never stored.
func (c *Core) CreateToken(ctx context.Context, by, actorID, name string) (raw string, t gen.ApiToken, err error)
func (c *Core) RevokeToken(ctx context.Context, by, tokenID string) error
const EvTokenCreated = "token.created"; const EvTokenRevoked = "token.revoked" // payload {"name","prefix"}
```

`serve` order: listen → `lockfile.Acquire(cfg.DataDir)` (refusal: close the listener, return ``Another vaquero is using {dir} (pid N).``) → open DB → owner → `ensureLocalToken` (read `{data dir}/token`; if missing, malformed, unknown or revoked, `CreateToken(owner, "local")` and write it with mode 0600) → serve. Release the lock after the WAL checkpoint.

- [ ] **Tests first:** `TestMintParseHash` (format, `Valid`, `Parse("bearer vq_…")`, bad schemes and lengths refused, `Hash` stable); `TestAcquireRefusesSecondHolder` (second `Acquire` → `ErrLocked` with this pid; after release it succeeds); core `TestCreateAndRevokeToken` (only the hash stored; `GetTokenByHash(Hash(raw))` finds it; revoke sets `revoked_at`; revoking twice is `Invalid`); `ops` entries for both; `TestServeWritesLocalToken` (file exists, mode 0600, token valid in the DB; a second boot keeps the same token); `TestServeRefusesSharedDataDir` (Review Focus 3: second `serve` on another free port with the same data dir returns the "Another vaquero" error while the first still answers `/healthz`).
- [ ] Implement, gate, commit — `"API: tokens, local token file, data directory lock"`

### Task 2: Ticket versions, partial updates, link loops

**Files:** `internal/db/queries/tickets.sql` (`version = version + 1` in `UpdateTicket`, `PlaceTicket`; new `PatchTicket` is not needed — reuse `UpdateTicket`), `internal/core/tickets.go`, `internal/core/patch.go` (+test), `internal/core/links.go` (+test), `internal/web/handlers/tickets.go`, `internal/web/templates/tickets.templ` (+tests)

**Interfaces:**

```go
// TicketPatch names the fields to change; nil keeps a field. Tags replaces
// the whole set; AddTags and RemoveTags adjust it (applied after Tags).
type TicketPatch struct {
	Title, Body, StatusID, AssigneeID, ParentID, MilestoneID *string // "" clears the last three
	Priority                                                 *int64
	Tags, AddTags, RemoveTags                                []string
	SetTags                                                  bool // Tags given (even empty)
}
// PatchTicket applies patch. version > 0 must equal the ticket's current
// version, else *Conflict. A patch that changes nothing writes nothing.
func (c *Core) PatchTicket(ctx context.Context, by, ticketID string, patch TicketPatch, version int64) (gen.Ticket, error)
// Conflict is a refused stale save: Current is the ticket as it is now.
type Conflict struct{ Current gen.Ticket }
func (e *Conflict) Error() string // "VAQ-12 changed since you read it."
```

`UpdateTicket(…, in TicketInput)` stays for callers that send everything; it becomes a full patch with version 0. `MoveTicket` raises the version too.

Link loops: before inserting "blocks" from F to T, walk "blocks" links outward from T (recursive CTE, depth cap 1000); if F is reachable, refuse `Invalid{"to": "That would make a loop: VAQ-3 already blocks VAQ-1 through VAQ-2."}` (the path, shortest first, keys only; "already blocks VAQ-1." when direct). "duplicates" is refused when T already duplicates F: `"VAQ-2 already duplicates this ticket."`.

Web edit form: a hidden `version`; a stale save re-renders the form with status 409, the typed values kept, the current version in the hidden field, and a banner "VAQ-12 changed since you opened this form. Saving again will overwrite those changes." with a link to the ticket.

- [ ] **Tests first:** `TestPatchChangesOnlyNamedFields` (status only; title, tags, parent untouched), `TestPatchEmptyStatusKeeps` (nil status keeps; the backlog item), `TestPatchTagsAddRemove`, `TestPatchClearsParent`, `TestStaleVersionRefused` (Review Focus 1: two patches from version 1 → second `*Conflict` with `Current.Version == 2`, nothing written, no event), `TestVersionRisesOnMoveAndUpdate`, `TestBlockingLoopRefused` (Review Focus 4: direct and through a chain, message names the path), `TestDuplicateBothWaysRefused`, `TestRelatesLoopAllowed`; `ops` entry for `PatchTicket`; handler `TestEditFormConflict` (two posts with the same version: second 409 with the banner and the typed title kept; a third post with the new version saves).
- [ ] Implement, gate, commit — `"Core: partial updates, versions, link loop checks"`

### Task 3: API foundation

**Files:** `internal/api/api.go` (+test), `internal/web/middleware/apitoken.go` (+test), `internal/web/handlers/api.go`, `internal/web/handlers/api_test.go`, `internal/web/router.go`, `internal/web/webtest/webtest.go`

**Interfaces:**

```go
package api
func Encode(v any) ([]byte, error)                       // indented, trailing newline
func WriteJSON(w http.ResponseWriter, status int, v any)
type Failure struct {
	Status        int
	Code, Message string
	Fields        map[string]string `json:"fields,omitempty"`
	Current       any               `json:"current,omitempty"`
}
func WriteFailure(w http.ResponseWriter, f *Failure)
func FromCore(err error, current func() any) *Failure    // Invalid→422 invalid, ErrNotFound→404 not_found, *Conflict→409 conflict (+current), else nil

package middleware
type APIAuth struct{ R, W *gen.Queries; Now func() time.Time; Logger *slog.Logger } // W only touches last_used_at — the one write outside core, through a query passed in
func APIToken(a APIAuth) func(http.Handler) http.Handler // puts the token's actor in the context
func APIHeaders(next http.Handler) http.Handler
func APIActor(ctx context.Context) (gen.Actor, bool)

package webtest
func (e *Env) Token(t testing.TB) string                  // a token for the owner
func (c *Client) API(method, path, token string, body any) *Response
```

Routes: `r.Route("/api/v1", …)` outside nothing new (HostCheck still applies); chain `APIHeaders, APIToken`; JSON `NotFound`/`MethodNotAllowed`; `GET /api/v1/me` → `{"actor": {"name", "kind"}, "version"}`. Bad JSON bodies → 400 `bad_request`; bodies over `MaxBody` → 413 `too_large`.

`Deps` gains nothing; `APIAuth.W` is built in the router from a writer `*gen.Queries` passed in `Deps` as `TouchQ` (a one-method interface `TouchToken`), so handlers still can't write. `TestHandlersNeverWriteDirectly` stays green.

- [ ] **Tests first:** `TestAPIRefusesBadTokens` (Review Focus 2: none, `Bearer x`, unknown well-formed, revoked → identical 401 body and `WWW-Authenticate`; a logger capturing output contains no `vq_`), `TestAPIMe`, `TestAPIHeaders` (no-store, both version headers), `TestAPINotFoundJSON` (with a token: 404 JSON), `TestAPIBadJSON`, `TestTouchThrottled` (two calls within 5 minutes touch once).
- [ ] Implement, gate, commit — `"API: tokens, JSON errors, version headers"`

### Task 4: API reads

**Files:** `internal/api/types.go` (DTOs), `internal/web/handlers/api_read.go`, `api_read_test.go`, router

**Interfaces (DTOs; JSON names snake_case):** `Project{Key, Name, Description, Archived bool, OpenCount int64}`, `Status{Name, Category, Position}`, `Milestone{Name, Description, DueOn, Closed bool, Done, Total int64}`, `TicketSummary{Key, Title, Status, Category, Priority, Assignee, Tags []string, Ready bool, Version int64}`, `Ticket{TicketSummary; Body, Parent, Milestone string; SubTickets []TicketSummary; Links []Link; Comments []Comment; CreatedAt, UpdatedAt string}`, `Link{ID, Kind, Direction ("out"/"in"), Key, Title}`, `Comment{ID, Author, Body, CreatedAt, UpdatedAt}`, `Event{Seq, At, Actor, Kind, Project, Ticket, Text (the EventSentence), Payload map[string]any}`, `SearchHit{Key, Title, Status, Snippet string}` (snippet with `[` `]` around hits).

Routes: `GET /projects`, `/projects/{key}`, `/projects/{key}/statuses`, `/projects/{key}/milestones`, `/projects/{key}/tickets` (query: the board filter keys plus `status` by name, `limit` ≤ 500), `/tickets/{ticket}`, `/search?q=`, `/events?after=&limit=` (≤ 500, oldest first).

- [ ] **Tests first:** `TestAPIProjects`, `TestAPITicketList` (filters, `status=Todo`, unknown status → empty, `limit`), `TestAPITicketGet` (full shape incl. links both directions, comments, version; lowercase key works; unknown key 404), `TestAPISearch` (odd input 200), `TestAPIEventsAfter` (paging by seq; `text` matches the activity wording).
- [ ] Implement, gate, commit — `"API: read projects, tickets, search and events"`

### Task 5: API writes

**Files:** `internal/web/handlers/api_write.go`, `api_write_test.go`, router

Routes: `POST /projects` `{key, name, description}`; `PATCH /projects/{key}` `{name?, description?}`; `POST /projects/{key}/key` `{key}`; `POST /projects/{key}/tickets` `{title, body?, status? (name), priority?, assignee? (actor name), parent? (key), milestone? (name), tags?}` → 201 with the ticket; `PATCH /tickets/{ticket}` with any of those fields plus `add_tags`, `remove_tags`, `version?` → 200 or 409 with `current`; `POST /tickets/{ticket}/move` `{status, before?, after?}`; `POST /tickets/{ticket}/comments` `{body}` → 201; `PATCH /comments/{id}` `{body}`; `DELETE /comments/{id}`; `POST /tickets/{ticket}/links` `{kind, to}` → 201; `DELETE /links/{id}`. Names (status, assignee, milestone) resolve within the ticket's project; an unknown name is a field error. The actor is the token's.

- [ ] **Tests first:** `TestAPICreateTicket` (names resolve; unknown status name → 422 `fields.status`), `TestAPIPatchConflict` (Review Focus 1 at the API: 409 with `current.version`), `TestAPIPatchPartial`, `TestAPIUnknownFieldRefused` (`{"titel": "x"}` → 400 naming the field; decoders use `DisallowUnknownFields`), `TestAPIMoveByName`, `TestAPICommentsAndLinks` (loop refusal surfaces as 422 with the message), `TestAPIWritesRecordTheTokenActor`.
- [ ] Implement, gate, commit — `"API: create and change projects, tickets, comments and links"`

### Task 6: Tokens page

**Files:** `internal/web/handlers/tokens.go`, `tokens_test.go`, `internal/web/templates/tokens.templ`, router, layout nav ("Tokens" in the top bar's right side, small)

Routes: `GET /settings/tokens` (list: name, prefix, created, last used, revoked), `POST /settings/tokens` `{name}` (renders the page with status 201 showing the new token once in a copyable `<input readonly>`, with "Copy this token now. It won't be shown again."), `POST /settings/tokens/{id}/revoke`. The local token is listed and revocable (a new one is written at the next start).

- [ ] **Tests first:** `TestTokensPageCreateShowsOnce` (the raw token appears in the POST response only; a following GET shows the prefix but not the token), `TestTokenRevokeStopsAPI` (revoked token → 401).
- [ ] Implement, gate, commit — `"Web: API tokens page"`

### Task 7: CLI client and profiles

**Files:** `internal/client/client.go`, `profile.go`, `start.go` (+tests)

**Interfaces:**

```go
package client
type Client struct{ BaseURL, Token, Agent string; HTTP *http.Client; Version string }
type Error struct {
	Status        int
	Code, Message string
	Fields        map[string]string
	Current       json.RawMessage
}
func (e *Error) Error() string
// Do sends body (nil, or JSON-encoded) and decodes a 2xx answer into out.
// A non-2xx answer is *Error; a server older than Min-Client never is —
// UpgradeNeeded reports when this client is below the server's minimum.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error
func (c *Client) UpgradeNeeded() (min string, need bool)

type Profile struct{ Name, URL, Token string }
type Config struct{ Current string; Profiles []Profile } // stored at ConfigPath(), mode 0600
func ConfigPath() (string, error)                       // os.UserConfigDir()/vaquero/config.json
func Load() (Config, error); func (c Config) Save() error
// Resolve picks the profile (flag > VAQUERO_PROFILE > Current > "local").
// "local" reads VQ_ADDR/VQ_DATA_DIR through config.FromEnv and the token
// from {data dir}/token.
func Resolve(name string, getenv func(string) string) (Profile, bool /*local*/, error)

// EnsureLocal starts `exe serve` in the background when the local server
// doesn't answer /healthz, with output to {data dir}/vaquero.log, and waits
// up to wait for it. It returns a plain error naming the address when the
// address is taken by something else or the server doesn't come up.
func EnsureLocal(ctx context.Context, exe, url, dataDir string, wait time.Duration) error
```

- [ ] **Tests first:** `TestDoDecodesAndErrors` (against an `httptest` server: 2xx decode, 409 → `*Error` with `Current`, version headers read), `TestProfilesRoundTrip` (Save writes 0600; Load reads back; Resolve order), `TestResolveLocalReadsTokenFile`, `TestEnsureLocalAlreadyUp` (no start attempted), `TestEnsureLocalPortTakenByOther` (Review Focus 5: a plain TCP listener that never speaks HTTP on the address → error naming it within the wait; `exe` is a test helper that exits immediately), `TestEnsureLocalStarts` (exe = the test binary re-invoked as a fake server via `TestMain` env switch, which serves `/healthz`; returns nil).
- [ ] Implement, gate, commit — `"CLI: HTTP client, profiles, starting the local server"`

### Task 8: CLI commands

**Files:** `cmd/vaquero/main.go` (dispatch), `cmd/vaquero/cli.go` (shared: flag parsing, client from profile, `--json`, exit codes, printing), `cli_projects.go`, `cli_tickets.go`, `cli_misc.go` (search, activity, token, profile), `cli_test.go`

Commands and flags (every command: `--profile NAME`, `--json`):

| Command | Does |
|---|---|
| `vaquero projects` | list |
| `vaquero project create KEY NAME [--description TEXT]` | create |
| `vaquero project show KEY` / `edit KEY [--name] [--description]` / `key KEY NEWKEY` | |
| `vaquero tickets KEY [--status S] [--tag T] [--priority P] [--milestone M] [--mine] [--agents] [--limit N]` | list (default: not done or cancelled, up to 100) |
| `vaquero ticket show TICKET` | full view: fields, description, sub-tickets, links, comments |
| `vaquero ticket new KEY TITLE [--body TEXT\|-] [--status] [--priority] [--tag T]... [--parent] [--milestone] [--assignee]` | prints the new key |
| `vaquero ticket edit TICKET [same flags] [--add-tag T]... [--remove-tag T]... [--version N]` | partial update |
| `vaquero ticket move TICKET STATUS` | |
| `vaquero comment TICKET TEXT\|-` | |
| `vaquero link TICKET blocks\|relates\|duplicates OTHER` / `unlink TICKET OTHER` | |
| `vaquero search WORDS...` | |
| `vaquero activity [--after SEQ] [--limit N]` | |
| `vaquero token create NAME` / `list` / `revoke PREFIX` | |
| `vaquero profile add NAME URL TOKEN` / `list` / `use NAME` / `remove NAME` | local config only |

Exit codes: 0 ok, 1 failure (message on stderr), 2 usage, 3 conflict (the current ticket is printed, or its JSON with `--json`). Human output: tables padded with `text/tabwriter`; keys first; no colour.

- [ ] **Tests first** (`cli_test.go`, against a `webtest` server through a profile in a temp config dir set by `t.Setenv`): `TestCLITicketLifecycle` (new → show → edit `--status` → move → comment → link; `--json` output parses), `TestCLIConflictExit3` (Review Focus 1: `edit --version 1` twice → second exit 3), `TestCLIBodyFromStdin`, `TestCLIUsageErrors` (unknown command or missing argument → exit 2 with usage), `TestCLIUpgradeMessage` (server Min-Client above the client's version → a plain message on stderr), `TestCLIProfileCommands`.
- [ ] Implement, gate, commit — `"CLI: commands for projects, tickets, comments, links, search, activity, tokens and profiles"`

### Task 9: Docs and check

- [ ] CLAUDE.md (package map: `internal/apitoken`, `internal/lockfile`, `internal/api`, `internal/client`, API routes, CLI commands; invariants: tokens, lock, version and conflict rules), `.claude/rules/security.md` (tokens and the API chain), a short `docs/CLI.md` (the command table and profiles), `docs/BACKLOG.md` (tick the before-Phase-2 items and the data-dir lock; add per-token rate limits for Phase 4), floors for new packages.
- [ ] `make ci`. By hand: stop any server, run `vaquero tickets VAQ` (the server starts), create, edit, move and comment from the CLI with the board open in Chrome and watch it update; `--json` through `jq`; two edits with the same `--version`. Commit — `"Docs: phase 2a"`
