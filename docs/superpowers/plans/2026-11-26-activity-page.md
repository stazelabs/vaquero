# Activity page Implementation Plan (VAQ-153)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/activity` (and a new `/p/{key}/activity` tab) a filterable, pageable audit log shaped for looking up one fact, with the same filters on `GET /api/v1/events`, `vaquero activity` and MCP `recent_activity`.

**Architecture:** A pure `internal/activity` package parses and prints the filter (query string ↔ struct, kind groups). `internal/db/activity.go` holds one hand-written query built from a resolved filter (as `search.go` is), keyset-paged by `seq`, with new indexes so no filter scans `events`. Handlers resolve names to ids once (`activityQuery`), then the web page, the API, and through the API the CLI and MCP, all read through it.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc for the existing queries, templ, htmx, Tailwind, chromedp e2e.

**Spec:** `docs/superpowers/specs/2026-11-26-activity-page-design.md` (read it first; this plan argues from it).

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; tasks touching a page, template, CSS or app.js also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-153)`, ending with the `Co-Authored-By` line.
- Reads only through `Deps.R` / `Deps.RDB`; nothing in this plan writes the tracker.
- No inline script, no `style=`, theme tokens only; behaviour in `internal/web/static/js/app.js`; icons from `internal/web/icons/ui.json`.
- Instants formatted only through `internal/timefmt`, displayed in UTC.
- `body_before`/`body_after` never leave the server through the API (`apiPayload`).
- `GET /api/v1/events` without the new parameters answers byte for byte as today.
- Copy: sentence case, no exclamation points, real counts ("1 event", "2 events").
- Every new CLI flag, API parameter, MCP argument and shortcut gets its help article line (tests enforce it).
- Page size 100 on the web; API keeps `limit` (default 100, max `maxAPIList`); CLI keeps `--limit` default 20.
- The migration is the next free number when the work starts (0013 on `run-o` today, which has 0012); put only the three indexes in it.

## Review Focus

1. **A moved ticket typed by its old key** (`?ticket=OLD-3`): resolves through `core.ResolveTicket` and shows the whole history, never an "unknown ticket" notice. Test in Task 3.
2. **Filters that match almost everything** (`who=agents` on the owner's data): the first page is still fast; caught by the benchmark in Task 2, not by the plan test.
3. **A date range with `from` after `to`, or a malformed date** on the web: an inline notice naming the field with a clear-this-filter link, never a 500; on the API a 422 naming the field. Tests in Tasks 3 and 5.
4. **Paging back to newer** from an older page (`after=SEQ` on the web, newest first): rows shown newest first, exactly the 100 just above SEQ, and "← Newer" disappears on reaching the top. Test in Task 4.
5. **A live change while reading page 2:** rows don't move; only the "New changes" note appears. e2e in Task 6.

---

### Task 1: The filter (`internal/activity`)

**Files:**
- Create: `internal/activity/filter.go`, `internal/activity/groups.go`
- Test: `internal/activity/filter_test.go`, `internal/activity/groups_test.go`

**Interfaces:**
- Produces:
  - `type Group struct{ Key, Label string; Kinds []string }`; `var Groups []Group` in this order and with these keys: `tickets`, `comments`, `attachments`, `links`, `claims`, `plans` (label "Plans and decisions"), `settings` (label "Project settings"), `access`, `reorders`, kinds exactly as the spec's table (expand `comment.*` etc. to the `core.Ev*` constants).
  - `func GroupByKey(key string) (Group, bool)`.
  - `type Filter struct { Project string; Who string /* "", "owner", "agents", "agent:NAME" */; Kinds []string /* group keys, sorted, deduped */; Ticket string; From, To string /* "YYYY-MM-DD" as typed */; Before, After int64; Oldest bool }`.
  - `func Parse(q url.Values) (Filter, []Problem)`; `type Problem struct{ Field, Value, Message string }`. Unknown group key, malformed date, `from` after `to`, a `who` not one of the forms, `before` with `after`, a non-positive seq: each is a Problem and that field is left empty (the rest of the filter stands). Values clipped with `units.Clip` to 100. Messages: "No kind X.", "Use a date like 2026-11-26.", "The end date is before the start date." (on `to`), "Use owner, agents or agent:NAME.", "Use before or after, not both.", "Use a sequence number above 0."
  - `func (f Filter) Query() url.Values` (stable key order; `Parse(f.Query())` round-trips), `func (f Filter) With(key, value string) Filter`, `func (f Filter) Without(key string) Filter`, `func (f Filter) ToggleKind(key string) Filter`, `func (f Filter) Active() bool` (any of project, who, kinds, ticket, dates set), `func (f Filter) FirstPage() Filter` (drops `Before`/`After`).
  - `func (f Filter) Range() (from, to time.Time, ok bool)`: UTC midnight of `From`, and UTC midnight of the day after `To`.

- [ ] **Step 1: Write the failing tests**
  - `TestEveryKindInOneGroup`: every kind in `core.EventKinds` is in exactly one group; no group names a kind not in `core.EventKinds`.
  - `TestParseRoundTrip`: `project=VAQ&who=agent:claude&kind=comments&kind=tickets&ticket=vaq-88&from=2026-11-01&to=2026-11-26&order=oldest&before=500` parses to the expected struct (Kinds `["comments","tickets"]`), and `Parse(f.Query())` equals `f`.
  - `TestParseProblems`: table of `kind=bogus`, `from=2026-13-01`, `from=2026-11-26&to=2026-11-01` (Problem on `to`), `who=someone`, `before=5&after=9`, `before=-1`; each yields one Problem with the right Field and that field cleared.
  - `TestToggleKind`, `TestFirstPageDropsCursor`, `TestRangeIsUTCDays` (`to=2026-11-26` → `2026-11-27T00:00:00Z`).
- [ ] **Step 2:** `go test ./internal/activity/` → FAIL (package missing).
- [ ] **Step 3:** Implement the files above.
- [ ] **Step 4:** `go test ./internal/activity/` → PASS.
- [ ] **Step 5:** Gate, then commit `Activity: the filter and kind groups (VAQ-153)`.

### Task 2: The query, its indexes and its speed (`internal/db`)

**Files:**
- Create: `internal/db/migrations/00NN_activity.sql` (next free number), `internal/db/activity.go`
- Test: `internal/db/activity_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (the db layer takes ids, not names).
- Produces:
  - `type ActivityQuery struct { ProjectID string; ActorID string; AgentsOnly bool; Kinds []string /* event kinds; empty = all but ticket.reordered */; TicketID string; From, To string /* timefmt.Format bounds, "" none: at >= From, at < To */; Before, After int64; Newest bool /* order of the answer */; Limit int }`.
  - `type ActivityRow struct` with every column `gen.ListEventsRow` has plus `ActorKind string`.
  - `func ListActivity(ctx context.Context, db *sql.DB, q ActivityQuery) (rows []ActivityRow, more bool, err error)`: reads `Limit+1` rows to set `more`; `After` reads ascending (`seq > After ORDER BY seq`) and, when `Newest`, reverses before returning; `Before` reads descending; neither: descending when `Newest`, else ascending from the start. `more` means more rows lie in the direction read.
- Migration contents (exactly): `CREATE INDEX events_actor ON events (actor_id, seq); CREATE INDEX events_kind ON events (kind, seq); CREATE INDEX events_at ON events (at);` with goose Down dropping them.
- Ticket mode (`TicketID` set): the row source is `ListTicketEvents`'s four lookups (copy its `WITH RECURSIVE up` and the `UNION ALL` of seq subqueries from `internal/db/queries/events.sql`, as a Go const with a comment pointing back to it); `ProjectID` is ignored (spec: the ticket filter wins); the other filters apply on top. `AgentsOnly` is `e.actor_id IN (SELECT id FROM actors WHERE kind = 'agent')`.

- [ ] **Step 1: Write the failing tests**
  - `TestListActivityFilters`: on a `coreseed.Seed` database, each filter alone and project+who+kind together returns exactly the events a Go filter over `ListEventsAfter(0, big)` keeps (seq lists equal); `ticket.reordered` appears only with `Kinds` containing it.
  - `TestListActivityTicketMatchesHistory`: for every seeded ticket, `ListActivity{TicketID}` seqs equal `gen.ListTicketEvents` seqs (guards the copied SQL).
  - `TestListActivityPaging`: with 250 events, newest first pages of 100 by `Before` cover all without overlap; `After` with `Newest` returns the 100 just above, newest first; `more` is false on the last page each way.
  - `TestListActivityDates`: an event whose `at` is out of seq order (set by a raw `UPDATE` in the test) is included or excluded by `at`, not by position.
  - `TestActivityUsesIndexes`: `EXPLAIN QUERY PLAN` (reuse `scansEvents` and the fixture loop from `TestTicketHistoryUsesIndexes`) for no filter, each filter alone, project+who+kind, who+dates, ticket+kind: none matches `scansEvents`. Expose the SQL builder as `activitySQL(q ActivityQuery) (string, []any)` for the test.
  - `BenchmarkListActivity`: seeds 100,000 events by raw insert (test only), 97% one agent, a fifth `ticket.claimed`/`ticket.released`, over 3 projects; sub-benchmarks for each filter alone and project+who+kind, first page. Not a CI gate; record results in the commit message.
- [ ] **Step 2:** `go test ./internal/db/ -run Activity` → FAIL.
- [ ] **Step 3:** Write the migration and `activity.go`. When the benchmark shows a case over 50 ms, add the composite index the plan needs (for example `(project_id, kind, seq)`) to the same migration, or steer with `INDEXED BY`/`+` unary on the less selective column; keep `TestActivityUsesIndexes` green.
- [ ] **Step 4:** `go test ./internal/db/ -run Activity` → PASS; `go test ./internal/db/ -run '^$' -bench ListActivity -benchtime 20x` → every case under 50 ms/op.
- [ ] **Step 5:** `make fixture` is not needed (no new table); `TestFixturesMigrateToHead` must pass. Gate, commit `DB: activity query and indexes (VAQ-153)`.

### Task 3: Resolving a filter (`internal/web/handlers`)

**Files:**
- Create: `internal/web/handlers/activity_query.go`
- Test: `internal/web/handlers/activity_query_test.go`

**Interfaces:**
- Consumes: `activity.Filter`, `activity.Problem` (Task 1); `appdb.ActivityQuery` (Task 2).
- Produces: `func activityQuery(ctx context.Context, d Deps, f activity.Filter, newest bool, limit int) (appdb.ActivityQuery, []activity.Problem, error)`: project key → id (case-insensitive; unknown is a Problem "No project KEY."), `owner` → the owner's actor id, `agents` → `AgentsOnly`, `agent:NAME` → that agent's id (unknown: "No agent NAME."), ticket key → `core.ResolveTicket` (unknown: "No ticket KEY."), groups → kinds, dates → `timefmt.Format` bounds. A Problem clears only its field.

- [ ] **Step 1: Write the failing tests** on `webtest.New`: each resolution above; `ticket=` an old key of a moved ticket resolves to the ticket (Review Focus 1); `from` after `to` arrives as Task 1's Problem untouched; an unknown project leaves `ProjectID` empty with one Problem.
- [ ] **Step 2:** `go test ./internal/web/handlers/ -run ActivityQuery` → FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** → PASS. Gate, commit `Handlers: resolve an activity filter (VAQ-153)`.

### Task 4: The page (`/activity` and `/p/{key}/activity`)

**Files:**
- Modify: `internal/web/handlers/activity.go`, `internal/web/router.go` (add `r.Get("/activity", handlers.ProjectActivity(d))` inside `/p/{key}`), `internal/web/templates/activity.templ`, `internal/web/templates/activity.go`, `internal/web/templates/board.templ` (`projectHeader`: Activity tab after List, `view == "activity"`), `internal/web/icons/ui.json` (the tab's icon), `internal/timefmt/timefmt.go`
- Test: `internal/web/handlers/activity_test.go`, `internal/web/templates/activity_page_test.go`, `internal/timefmt/timefmt_test.go`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces:
  - `timefmt.Stamp(t, now time.Time) string` → `"Nov 26 14:03"`, `"Nov 26, 2025 14:03"` when not `now`'s year (UTC); `timefmt.DisplayUTCSeconds(t) string` → `"2026-11-26 09:12:44 UTC"`.
  - `templates.ActivityView{ Project *gen.Project /* nil on the global page */; Filter activity.Filter; Problems []activity.Problem; Events []EventView; Newer, Older string /* hrefs, "" none */; Live bool; Projects []gen.Project; Agents []gen.Actor; TicketNote string }` and `templ ActivityPage(v ActivityView)`, `templ ProjectActivityPage(v ActivityView)` (inside `projectHeader`).
  - `EventView` gains `At time.Time`, `When string` (Stamp), `Exact string` (DisplayUTCSeconds), `ActorKind string`, `Meta map[string]string`.
  - `func EventChanges(e EventView) []Change` with `type Change struct{ Field, From, To string }`, from `payload.changes` (tags as comma lists; a body change is one Change with `Field: "description"` and the template links "Show changes" to the ticket's history instead of values).

- [ ] **Step 1: Write the failing tests**
  - `TestStamp`, `TestDisplayUTCSeconds`.
  - `TestActivityPageFilters`: `?who=owner` shows only the owner's rows; `?kind=claims` only claim rows; default hides reorders, `?kind=reorders` shows them one per row (no folding).
  - `TestActivityPaging`: 250 events; page one has "Older →" to `before=<last seq>`; that page has "← Newer" to `after=<its first seq>`; following "← Newer" returns page one's rows in the same order (Review Focus 4).
  - `TestActivityProblemsNotice`: `?kind=bogus&who=owner` is 200, shows "No kind bogus." (copy from Problem) with a link whose href is the filter without `kind`, and still filters by owner (Review Focus 3).
  - `TestActivityEmptyStates`: "Nothing has happened yet." with no events and no filter; "No events match these filters." plus a "Clear filters" link otherwise.
  - `TestProjectActivityTab`: `/p/VAQ/activity` has the tab marked current, no project menu, only VAQ rows; `/p/vaq/activity` redirects to `/p/VAQ/activity` like the other views; `?ticket=` of a ticket moved in from another project shows its old events with the note "Showing VAQ-88's whole history, from every project."
  - `TestActivityRowDetails`: a priority change row's `<details>` holds "priority", "None", "Urgent", the seq and the exact UTC time; an agent row shows the harness and model from meta.
  - Extend the existing workflow-off and shell tests as they require (the new tab appears on every project page).
- [ ] **Step 2:** `go test ./internal/timefmt/ ./internal/web/...` → FAIL.
- [ ] **Step 3:** Implement. Layout per the spec's sketch: filter bar reusing `filters.templ` menu markup (`details[data-menu]`, `data-menu-search` on Who) for Project, Who and Kind (Kind items toggle with a check mark, several allowed); a GET `<form>` with the ticket box, two `<input type="date">` and the current filter as hidden inputs, button "Apply"; an order link "Newest first" / "Oldest first"; removable chips per active filter. Rows as a `<ul>` of `<li><details data-keep="{seq}">` with `<summary>` holding when (title: Ago), who (agent badge icon + "harness · model" when meta has them), the linked ticket key or project key, the sentence; stacked at <640 px. The feed is `#feed`: with `Live` (first page and newest first) it keeps today's `hx-get`/`hx-trigger="vq:change"`/`hx-select` on the current URL; otherwise it carries `data-new-events` and a hidden note "New changes since this page loaded." with a link "Show newest" to `Filter.FirstPage()` newest first.
- [ ] **Step 4:** → PASS. `make gen`, gate, commit `Web: activity filters, paging, row detail and the project tab (VAQ-153)`.

### Task 5: The API

**Files:**
- Modify: `internal/web/handlers/api_read.go` (`APIEvents`), `internal/help/articles/api.md`
- Test: `internal/web/handlers/api_read_test.go` (or the existing events API test file)

**Interfaces:**
- Consumes: Tasks 1–3. Reads the same query keys as the web page except `order` (ignored; answers are always oldest first), plus `newest` and `limit`.
- Produces: `GET /api/v1/events` with `project`, `who`, `kind` (repeatable), `ticket`, `from`, `to`, `before`; any Problem is a 422 through `api.WriteFailure` with `fields` keyed by the query name; `newest` with `before` or `after` is a 422; header `Vaquero-More: true` when `more`; `Vaquero-Seq` unchanged.

- [ ] **Step 1: Write the failing tests**: `TestAPIEventsUnchanged` (no new parameters: body and headers identical to a golden taken before the change, `after` and `newest` both); `TestAPIEventsFilters` (`who=owner&kind=tickets`); `TestAPIEventsBefore` (oldest first, the newest that match below SEQ, `Vaquero-More` set when more lie below); `TestAPIEvents422` (`kind=bogus`, `from=2026-11-26&to=2026-11-01`, `before=5&after=9`, `newest=3&before=9`, each naming its field).
- [ ] **Step 2:** → FAIL. **Step 3:** implement (`newest=N` maps to `Newest` read then reversed; `before` reads descending, then reversed to oldest first). **Step 4:** → PASS.
- [ ] **Step 5:** api.md's events line gains the filters, `before` and `Vaquero-More`. Gate, commit `API: filters and before= on events (VAQ-153)`.

### Task 6: Live behaviour, the shortcut and the browser tests

**Files:**
- Modify: `internal/web/static/js/app.js`, `internal/web/templates/shell.templ` (shortcuts dialog: `g a`), `internal/help/articles/keyboard-shortcuts.md`, `internal/help/articles/the-board.md` (Live updates: the note on later pages)
- Create: `internal/help/articles/activity.md` (section `using`: what the page is for, each filter, paging, the row details, the project tab, links to `/help/api` and `/help/cli`)
- Test: `internal/e2e/activity_test.go`

**Interfaces:**
- Consumes: Task 4's markup (`#feed`, `[data-new-events]`, `details[data-keep]`).

- [ ] **Step 1: Write the failing e2e tests**: `TestActivityLiveFirstPage` (a change made through `Deps.Core` appears at the top); `TestActivityLaterPageShowsNote` (on `?before=…`, a change leaves the rows as they were and unhides "New changes since this page loaded."; Review Focus 5); `TestActivityKeepsOpenRows` (an open row stays open across a live refresh); `TestShortcutGoActivity` (`g a` on a project page opens `/p/KEY/activity`, elsewhere `/activity`); widths 375 px and 1280 px in both themes, no page wider than 375 px.
- [ ] **Step 2:** `make e2e` → FAIL.
- [ ] **Step 3:** app.js: `g a` beside `g f`/`g b`/`g l`; on the stream's change, `[data-new-events]` unhides its note instead of refreshing; generalise the Focus keep-open handler to any swap target containing `details[data-keep]` (it keys on `#focus` today). Help lines and the new article.
- [ ] **Step 4:** `make e2e` and `go test ./internal/help/` → PASS. Gate, commit `Web: live activity, g a and the activity help (VAQ-153)`.

### Task 7: CLI and MCP

**Files:**
- Modify: `cmd/vaquero/cli_cmds.go` (`cmdActivity`), `internal/mcpserver/tools.go` (`activityArgs`, `recent_activity`), `internal/help/articles/cli.md`, `internal/help/articles/mcp.md`
- Test: `cmd/vaquero/cli_test.go` (existing activity tests' file), `internal/mcpserver/tools_test.go`

**Interfaces:**
- Consumes: Task 5's API.
- Produces: `vaquero activity [--project KEY] [--who owner|agents|agent:NAME] [--kind GROUP]... [--ticket TICKET] [--from DATE] [--to DATE] [--before SEQ] [--after SEQ] [--limit N]`; with neither `--after` nor `--before` it asks `newest=limit` as today; prints `More: run with --before SEQ` (the first row's seq) on stderr when `Vaquero-More` is true and the read went back; a 422 is exit 2 with the API's message. MCP `recent_activity` adds optional `project`, `who`, `kinds` (array), `ticket`, `from`, `to`, `before`, passed through as one `/events` request.

- [ ] **Step 1: Write the failing tests**: CLI `--who owner --kind tickets` sends those query values; `--before` prints the More line; `--kind bogus` exits 2 naming the field. MCP: the tool's schema lists the new arguments; a call with `kinds: ["claims"]` sends `kind=claims`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement; help lines in cli.md and mcp.md. **Step 4:** → PASS.
- [ ] **Step 5:** Gate (`make ci` here, it's the last task), update CLAUDE.md's package map entries for `internal/activity`, `internal/db` (`ListActivity`, the indexes), handlers and templates, commit `CLI, MCP: activity filters (VAQ-153)`.
