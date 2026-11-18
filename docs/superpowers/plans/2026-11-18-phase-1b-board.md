# Phase 1b: The board — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A drag-and-drop board per project (active columns, folded backlog and cancelled rails, filter chips, ready edge, milestone panel), a filtered list view, full-text search, live updates and keyboard shortcuts.

**Architecture:** One additive migration adds `tickets.status_changed_at` and two FTS5 tables kept current by triggers. A new core operation, `MoveTicket`, handles drag (status plus position, one event). A `board` read package turns a project, a filter and the clock into the board and list views. Pages are templ with htmx fragments; drag is SortableJS posting through htmx; live updates are a server-sent event stream that announces the newest event sequence number.

**Tech Stack:** As Phase 1a, plus vendored SortableJS 1.15.6 and htmx's SSE extension (both copied from `/Users/cstaszak/Stazelabs/projects/starfish/internal/web/static/vendor/`), and SQLite FTS5 (built into `modernc.org/sqlite`; verified: prefix and accent-insensitive matching work).

**Spec:** `docs/superpowers/specs/2026-11-18-phase-1b-board-design.md`, extending `docs/superpowers/specs/2026-11-18-vaquero-design.md`.

## Global Constraints

- Everything in CLAUDE.md "Hard invariants". In particular: only `internal/core` writes tracker tables (triggers that maintain the FTS tables fire on core's writes and count as part of them); one event per successful core operation; no inline script or `style=`; theme tokens only; generated files committed.
- Migrations are append-only: this plan adds `0003_board.sql` only.
- Gate before every commit, run as separate steps and judged by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`. `make ci` at the end of each task.
- New vendored files are copied byte-for-byte from starfish; `static.go` embeds them; pages load them with the CSP nonce.
- Copy follows `.claude/rules/copy-voice.md`.
- Commit messages `{Area}: {summary}`, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Deferred work goes into `docs/BACKLOG.md`.

## Review Focus

1. **Someone types search or filter input the parser doesn't expect**: `"`, `AND (`, `*`, `-x`, `NEAR`, 500 characters, an empty string, `?priority=9`, `?tag=no-such-tag`, `?milestone=garbage`. Every one gives a page (results or "No tickets match."), never a 500. (Tests in Tasks 3 and 6.)
2. **A drop arrives from a stale page**: the target status was deleted, the before/after cards have since moved to another column or project, or the ticket key no longer exists. The move lands sensibly (end of the column) or is refused with a toast, and the board re-renders from the database. (Tests in Tasks 2 and 4.)
3. **Someone drags cards into the same spot many times.** Positions are midpoints; after enough halvings the gap vanishes. The column is renumbered before that, and order is never lost. (Test in Task 2.)
4. **The server shuts down while browsers hold the live-update stream open.** Shutdown finishes in seconds, not after the 30-second drain. (Test in Task 5.)
5. **A board tab sits open for an hour.** The stream outlives the server's 60-second write timeout and keeps delivering changes. (Test in Task 5.)

---

### Task 1: Migration: status time and search index

**Files:**
- Create: `internal/db/migrations/0003_board.sql`, `internal/db/queries/board.sql`, `internal/db/queries/search.sql`, `internal/db/board_test.go`
- Modify: `internal/db/queries/tickets.sql` (`CreateTicket`, `UpdateTicket` take `status_changed_at`)
- Generated: `internal/db/gen/`

**Interfaces:**
- Schema:
  - `ALTER TABLE tickets ADD COLUMN status_changed_at TEXT NOT NULL DEFAULT ''`, then `UPDATE tickets SET status_changed_at = updated_at`.
  - `CREATE VIRTUAL TABLE tickets_fts USING fts5(title, body, content='tickets', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2')`.
  - `CREATE VIRTUAL TABLE comments_fts USING fts5(body, content='comments', content_rowid='rowid', tokenize='unicode61 remove_diacritics 2')`.
  - Triggers keep both current: insert, update of the indexed columns, and (for comments) soft delete removes the row from `comments_fts`. Backfill both with `INSERT INTO …_fts(…_fts) VALUES('rebuild')`, then delete deleted comments' rows.
- Queries (`board.sql`), every filter optional through `sqlc.narg` (`NULL` means "any"):
  - `ListBoardTickets :many` — project's tickets joined with status name, category and position, assignee name and kind; filters `assignee_id`, `assignee_kind`, `priority`, `milestone_id`, `no_milestone` (bool), `tag` (name, case-insensitive, via `EXISTS`); plus computed `ready` (the spec's definition: todo category, unassigned, no "blocks" link from a ticket whose status category isn't done/cancelled, no sub-ticket whose status category isn't done/cancelled). Ordered by status position, then ticket position.
  - `ListMilestoneProgress :many` — per milestone of a project: `id`, `name`, `due_on`, `closed_at`, `done` (done category), `total` (excluding cancelled).
  - `CountOpenWithoutMilestone :one`.
- Queries (`search.sql`): `SearchTickets :many` (params `query TEXT`, `limit`) — tickets whose title/body match, unioned with tickets whose live comments match, distinct, best rank first; returns project key, number, title, status name and category, and an FTS `snippet(...)` of the matching text with `[` `]` markers.

- [ ] **Step 1: Write the failing tests:**
  - `TestStatusChangedAtBackfilled` — a ticket made before 0003 (use `db.MigrateTo(w, 2)`, insert, then `Migrate`) has `status_changed_at = updated_at`.
  - `TestSearchIndexFollowsWrites` — create, retitle, comment, soft-delete the comment: matches appear and disappear.
  - `TestSearchIgnoresAccents` — "cafe" finds "café".
  - `TestBoardFilters` — each filter alone narrows the rows; all NULL returns everything.
  - `TestReadyFlag` — table: unassigned todo → ready; assigned → not; open blocker → not; done blocker → ready; open sub-ticket → not; in progress → not.
  - `TestMilestoneProgress` — 2 done, 1 open, 1 cancelled → done 2, total 3.
- [ ] **Step 2:** Run `go test ./internal/db/...` — Expected: FAIL.
- [ ] **Step 3:** Write the migration and queries; `make sqlc`.
- [ ] **Step 4:** Run `go test ./internal/db/...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"DB: status time, search index, board queries"`

---

### Task 2: Core: status time and `MoveTicket`

**Files:**
- Modify: `internal/core/tickets.go` (set `status_changed_at` on create and on a status change), `internal/core/events.go` (`EvTicketReordered`)
- Create: `internal/core/move.go`, `internal/core/move_test.go`
- Modify: `internal/core/fixture_test.go` (`ops` entry), `internal/web/templates/activity.go` (wording)

**Interfaces:**

```go
// MoveTicket puts a ticket in statusID between the tickets beforeID and
// afterID (either may be "": top or bottom of the column). A neighbour
// that is not in that column is ignored. A status change records
// ticket.updated with the status change (worded "moved VAQ-1 from Todo to
// In progress"); a reorder within one column records ticket.reordered.
func (c *Core) MoveTicket(ctx context.Context, by, ticketID, statusID, beforeID, afterID string) (gen.Ticket, error)
const EvTicketReordered = "ticket.reordered" // payload {"status": name}; worded "reordered VAQ-1 in Todo"
```

- Position: midpoint of the neighbours' positions; top is `first - positionStep`, bottom `last + positionStep`, empty column `positionStep`. When the gap between the two neighbours is under `1e-6`, renumber the whole column (`positionStep`, `2×positionStep`, …, keeping order) inside the same transaction, then place the ticket.
- Refusals: unknown ticket → `ErrNotFound`; status not in the ticket's project → `Invalid{"status": "Choose a status from this project."}`; dropping where it already is → success with no event.

- [ ] **Step 1: Write the failing tests:** `TestMoveBetweenColumns` (status and `status_changed_at` change, event wording via payload), `TestReorderWithinColumn` (order of three cards becomes b, a, c), `TestMoveIgnoresForeignNeighbour` (a `beforeID` from another column → lands at the end; Review Focus 2), `TestMoveToOtherProjectStatusRefused`, `TestRepeatedTopInsertsKeepOrder` (60 moves to the top of one column, each above the previous; final order exactly reversed; Review Focus 3), `TestMoveInPlaceWritesNothing`. Add `MoveTicket` to `ops` (good: T1 to In progress; bad: a status from OTH).
- [ ] **Step 2:** Run `go test ./internal/core` — Expected: FAIL.
- [ ] **Step 3:** Implement. `UpdateTicket` and `CreateTicket` set `status_changed_at` to now when the status is new or changes, else keep it.
- [ ] **Step 4:** Run `go test -race ./internal/core ./internal/web/...` — Expected: PASS (`TestEveryEventKindHasWording` covers the new kind once it is in `EventKinds`).
- [ ] **Step 5: Commit** — `"Core: move tickets on the board"`

---

### Task 3: Board read model and filters

**Files:**
- Create: `internal/board/board.go`, `filter.go`, `board_test.go`, `filter_test.go`

**Interfaces:**

```go
package board

type Filter struct {
	Mine, Agents bool
	Tag          string // "" any
	Priority     int64  // -1 any; 0..4
	Milestone    string // "" any; "none"; or a milestone id
	Assignee     string // "" any; an actor id
}
func ParseFilter(q url.Values) Filter        // unknown or malformed values become "any"
func (f Filter) Query() url.Values           // round-trips through ParseFilter
func (f Filter) With(key, value string) Filter // a chip's toggle target ("" clears that key)
func (f Filter) Active() bool

type Card struct {
	Key, Title, Assignee string
	AssigneeIsAgent, Ready bool
	Priority int64
	Tags []string
}
type Column struct {
	Status gen.Status
	Cards  []Card
	Older  int // done columns: cards hidden because they reached done over 7 days ago
}
type MilestoneProgress struct {
	ID, Name, Due string
	Done, Total int64
}
type Board struct {
	Project    gen.Project
	Filter     Filter
	Active     []Column // todo, in_progress, in_review, done categories, in status order
	Rails      []Column // backlog and cancelled, in status order
	Milestones []MilestoneProgress
	NoMilestone int64
}
func Load(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter, now time.Time) (Board, error)
func LoadList(ctx context.Context, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter) ([]Column, error) // every status, nothing hidden
```

- Done window: 7 days, by `status_changed_at`.
- `Mine` filters on the owner; `Agents` on assignee kind `agent`.

- [ ] **Step 1: Write the failing tests:** `TestParseFilterOddInput` (Review Focus 1: `priority=9`, `priority=x`, `milestone=garbage` kept as an id that matches nothing, `tag=` with 500 characters, repeated keys → first wins; never panics), `TestFilterRoundTrip`, `TestLoadSplitsActiveAndRails`, `TestDoneWindowHidesOld` (move the clock 8 days; `Older` counts it), `TestLoadAppliesFilters`, `TestMilestonePanelCounts`.
- [ ] **Step 2:** Run `go test ./internal/board` — Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./internal/board` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Board: read model and filters"`

---

### Task 4: Board page, list view and drag

**Files:**
- Create: `internal/web/templates/board.templ`, `internal/web/handlers/board.go`, `board_test.go`
- Copy: `internal/web/static/vendor/sortable.min.js` from starfish
- Modify: `internal/web/templates/projects.templ` (the Phase 1a grouped page becomes `ListPage` taking `[]board.Column` and the filter), `internal/web/handlers/projects.go` (`ProjectShow` renders the board), `internal/web/router.go`, `internal/web/static/static.go`, `internal/web/static/js/app.js`, `internal/web/static/css/input.css`, `layout.templ` (load sortable)

**Interfaces:**
- Routes: `GET /p/{key}` board; `GET /p/{key}/list` list; `GET /p/{key}/board` the board's inner fragment (`#board`, for htmx refresh); `POST /t/{ticket}/move` with form `status`, `before`, `after` (ticket keys or ""); answers the refreshed `#board` fragment for htmx requests, a 303 to the board otherwise; a refusal answers the fragment with an error toast header (`HX-Trigger: {"vq-toast": "..."}`) that `app.js` shows.
- Board markup: each column's card list is `<ol data-board-column data-status="{id}">`; each card `<li data-card="{KEY}">` linking to `/t/{KEY}`; ready cards carry `data-ready` and a left accent border; rails are `<details>`; chips are links built from `Filter.With`; the milestone panel links to `?milestone={id}` or `?milestone=none`; "N older" links to `/p/{key}/list?…` with the done filter kept.
- `app.js`: on load and after each htmx swap of `#board`, attach `Sortable` to every `[data-board-column]` with `group: "board"`, `animation: 150`; `onEnd` posts with `htmx.ajax("POST", "/t/" + key + "/move", {target: "#board", swap: "outerHTML", values: {status, before, after}})`; a `vq-toast` event shows a toast built from a `<template>` in `Shell`. `window.vqDragging` is true between `onStart` and `onEnd`.
- Chips: Mine, Agents, each open tag on the board, each priority, clear all.

- [ ] **Step 1: Write the failing tests** (`board_test.go`, webtest): `TestBoardShowsActiveColumnsAndRails`, `TestReadyCardMarked`, `TestFilterChipsInURL` (`?tag=ui` hides an untagged card; the chip links toggle off), `TestMilestonePanel` (progress text "2 of 3 done"), `TestMoveEndpoint` (POST with `HX-Request` returns the fragment with the card in the new column; without it, 303), `TestMoveToDeletedStatus` (Review Focus 2: refused, fragment returned with `HX-Trigger` containing the message), `TestMoveUnknownTicket` (toast, no 500), `TestListViewShowsEverything` (`/p/VAQ/list` lists backlog, cancelled and old done tickets), `TestBoardFragmentRoute`.
- [ ] **Step 2:** Run `go test ./internal/web/...` — Expected: FAIL.
- [ ] **Step 3:** Implement. Copy `sortable.min.js` byte-for-byte; embed it.
- [ ] **Step 4:** Run `make gen && go test ./internal/web/...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: board with drag, list view, filters, milestone panel"`

---

### Task 5: Live updates

**Files:**
- Create: `internal/web/handlers/stream.go`, `stream_test.go`
- Copy: `internal/web/static/vendor/htmx-ext-sse.min.js` from starfish
- Modify: `handlers.Deps` (add `Shutdown <-chan struct{}` and `StreamPoll time.Duration`), `cmd/vaquero/serve.go` (close `Shutdown` from `srv.RegisterOnShutdown`), `internal/web/webtest/webtest.go`, `board.templ`, `projects.templ` (list), `activity.templ`, `layout.templ`, `app.js`, `router.go`

**Interfaces:**
- `GET /events/stream`: `Content-Type: text/event-stream`, `Cache-Control: no-store`. Clears its write deadline with `http.NewResponseController(w).SetWriteDeadline(time.Time{})`. Every `StreamPoll` (1 s in serve) it reads `MAX(seq)` from `events` through `d.R` and, when it rose, writes `event: change\ndata: {seq}\n\n` and flushes. Every 25 s with no change it writes `: ping\n\n`. It returns when the request context ends or `Shutdown` closes.
- Pages: the board, list and activity content containers carry `hx-ext="sse" sse-connect="/events/stream"` on a wrapper and `hx-get="{fragment url}" hx-trigger="sse:change" hx-swap="outerHTML"`; the activity page gets a fragment route `GET /activity/feed`. `app.js` cancels an `sse:change` refresh of `#board` while `window.vqDragging` is true and runs it on drop instead.

- [ ] **Step 1: Write the failing tests:**
  - `TestStreamAnnouncesChanges` — open the stream (webtest with `StreamPoll` 20 ms), create a ticket through core, read `event: change` within 1 s.
  - `TestStreamOutlivesWriteTimeout` (Review Focus 5) — an `httptest.Server` whose `Config.WriteTimeout` is 200 ms; after 500 ms of no change, a change still arrives on the open stream.
  - `TestStreamStopsOnShutdown` (Review Focus 4) — closing `Shutdown` ends the handler within 1 s.
  - In `cmd/vaquero/serve_test.go`: `TestServeStopsPromptlyWithOpenStream` — open `/events/stream`, cancel the serve context, `serve` returns within 5 s.
- [ ] **Step 2:** Run `go test ./internal/web/... ./cmd/vaquero` — Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `make gen && go test ./internal/web/... ./cmd/vaquero` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: live updates over server-sent events"`

---

### Task 6: Search and keyboard shortcuts

**Files:**
- Create: `internal/search/search.go`, `search_test.go`, `internal/web/handlers/search.go`, `search_test.go` (handlers), `internal/web/templates/search.templ`
- Modify: `layout.templ` (header search box; the shortcuts `<dialog>`), `router.go`, `app.js`, `input.css`

**Interfaces:**

```go
package search
// MatchQuery turns typed text into a safe FTS5 query: each run of letters
// and digits becomes a quoted prefix term ("colum"*), joined by spaces
// (AND). It returns "" when there is nothing to search for.
func MatchQuery(typed string) string
type Hit struct{ Key, Title, Status, Category, Snippet string }
func Run(ctx context.Context, q *gen.Queries, typed string, limit int64) ([]Hit, error) // "" → no hits, no query
```

- `GET /search?q=…`: the results page, 50 hits max, each linking to its ticket with the snippet (markers rendered as `<mark>`, text escaped); "No tickets match." when empty; the typed text stays in the box.
- Shortcuts in `app.js`, ignored while focus is in an input, textarea, select or `contenteditable`: `c` → the current project's new-ticket page (from `data-new-ticket-url` on `<main>`, else the project list), `/` → focus the header search box, `g` then `b` / `l` → this project's board or list, `j`/`k` → move a focus ring through `[data-card]` or list rows, `enter` → open the focused one, `?` → open the shortcuts dialog, `esc` → close it.

- [ ] **Step 1: Write the failing tests:**
  - `TestMatchQuery` (Review Focus 1): `"colum"` → `"colum"*`; `fix the  gate` → `"fix"* "the"* "gate"*`; `"`, `AND (`, `*`, `-`, `NEAR(a b)` → only quoted word terms or ""; 500 characters → bounded to the first 20 terms; "" and "!!!" → "".
  - `TestRunFindsByTitleBodyAndComment`, `TestRunIgnoresDeletedComments`.
  - Handlers: `TestSearchPage` (hit with snippet and link), `TestSearchOddInput` (each Review Focus 1 string → 200), `TestSnippetEscaped` (a title containing `<b>` shows escaped), `TestShortcutsDialogPresent` (the dialog markup and the header search box are on every page).
- [ ] **Step 2:** Run the tests — Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `make gen && go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: search and keyboard shortcuts"`

---

### Task 7: Docs, floors and the browser check

**Files:**
- Modify: `CLAUDE.md` (package map: `internal/board`, `internal/search`, new routes, `Deps.Shutdown`), `.claude/rules/handlers.md` (fragments and `HX-Trigger` toasts; the stream), `docs/BACKLOG.md` (tick Phase 1b; add what was deferred), `coverage-floors.txt` (`internal/board`, `internal/search` at 5 below measured, rounded down to a multiple of 5)

- [ ] **Step 1:** Update the docs and floors.
- [ ] **Step 2:** Run `make ci` — Expected: PASS.
- [ ] **Step 3:** Browser check with `make dev` (port 7879) and Chrome, on seeded data: drag a card between columns and within one; open a rail; toggle chips and the milestone filter; change a ticket in a second tab and watch the first update; search; try every shortcut. Then view the board with the browser forced to dark mode (Chrome DevTools rendering emulation of `prefers-color-scheme: dark`, through the `javascript_tool` or by asking the owner to toggle it) and record what was seen. Expected: no console errors; dark mode readable.
- [ ] **Step 4: Commit** — `"Docs: phase 1b"`
