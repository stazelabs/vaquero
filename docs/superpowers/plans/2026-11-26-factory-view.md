# Factory view Implementation Plan (VAQ-154)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A global `/factory` page and a per-project `/p/{key}/factory` tab that answer "are the agents starved of work?": the ready queue and its runway, 24 hours of ready history, empty ready asks, why todo tickets aren't ready, decisions waiting on the owner, line running hours, workers, and each project's stations.

**Architecture:** A new `internal/factory` package does all the counting: `Snapshot` classifies every open ticket into a station with its reasons (one batched query plus one `readiness.CheckAll`), `Misses` counts empty ready answers in memory, and the claims, pace and history readers turn events and samples into hours. The sweep writes a `Snapshot` and the drained `Misses` every 5 minutes into two new tables; the pages read the live `Snapshot` for "now" and the tables for history.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc, templ, htmx, Tailwind, chromedp e2e.

**Spec:** `docs/superpowers/specs/2026-11-26-factory-view-design.md` (read it first). Built after VAQ-153 (`docs/superpowers/plans/2026-11-26-activity-page.md`): it uses that plan's `events (kind, seq)` index, the project header's Activity tab, `g a`'s shortcut code, and `/activity` filters (`project`, `who`, `kind`, `ticket`, `from`, `to`).

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; page, template, CSS or app.js changes also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-154)`, ending with the `Co-Authored-By` line.
- Nothing here writes the tracker or appends events. Handlers write nothing (`TestHandlersNeverWriteDirectly`); the sweep writes the two new tables through `gen.New(s.DB)` as it prunes `mcp_calls`.
- No `style=`, no `<svg>` in templates, theme tokens only (new colours as tokens in `internal/web/static/css/input.css`, light and dark); every coloured part also carries its count in words.
- UTC everywhere, through `internal/timefmt`.
- `SampleEvery = 5 * time.Minute`, `KeepSamples = 90 * 24 * time.Hour`, stuck after 24 h, pace over 7 days, history over 24 h.
- Archived projects are left out of every count.
- Workflow-off projects get no readiness reasons, only the board's conditions; `TestWorkflowOffUnchanged` stays green.
- The two new tables are not exported: add them to the left-out list in `internal/archive/archive.go`'s doc comment and `archive_test.go`'s `left` slice; add them to `scripts/fixture`'s table list only if its test demands every table.
- Copy (exact): "No pace yet.", "History starts {Nov 26}.", "A ticket can have more than one reason.", "Empty for {N} h of 24 · {M} of them while an agent was working", "Asked and found nothing: {N} times today, last at {15:04}" ("1 time"), "about {N} h of work at this week's pace".

## Review Focus

1. **A fresh install or a project created an hour ago:** no samples, no done tickets: the band says "History starts …" and "No pace yet.", nothing divides by zero, the strip shows only sampled hours. Tests in Tasks 4 and 5.
2. **A claim that ended without a claim event** (ticket reassigned, or moved to done): line running stops at that `ticket.updated`, not at "now". Test in Task 3.
3. **A ticket at the 24 h stuck boundary and in a project archived since the last sample:** stuck is strictly over 24 h; the archived project drops from lines and band at once even though old samples name it. Tests in Tasks 1 and 5.
4. **An unscoped empty ready ask with several projects:** counts once globally and once on each project page. Test in Task 2.
5. **The sweep failing to sample** (a read error): logged, the other sweep work still runs, the next tick tries again; no half-written sample (one transaction). Test in Task 4.

---

### Task 1: Stations and reasons now (`internal/factory`)

**Files:**
- Create: `internal/factory/snapshot.go`, `internal/db/queries/factory.sql` (then `make gen`)
- Test: `internal/factory/snapshot_test.go`

**Interfaces:**
- Produces:
  - sqlc `ListFactoryTickets :many`: every ticket in a non-archived project whose status category isn't cancelled, with `id, project_id, number, title, status_name, category, assignee_id, claimed_by, claim_expires_at, status_changed_at, workflow` (the project's), `has_open_blocker`, `has_open_sub` (the same rules `ListOpenBlockers` / `ListParentsWithOpenSubTickets` use). Done tickets only when `status_changed_at` is within the last 7 days (pass the bound).
  - `type Station string` with consts `Intake, Triage, Ready, NotReady, Working, Review, Shipped`; `var Stations = []Station{…}` in that order.
  - `type ProjectNow struct { ProjectID string; Count map[Station]int; ShippedToday, ShippedWeek int; Reasons map[string]int; Oldest map[Station]time.Time; Stuck []Ticket; Claims []Claim; OpenDecisions []Decision }`; `type Ticket struct{ ID, Key, Title string }`; `type Claim struct{ Ticket Ticket; Agent, Harness, Model string; Expires time.Time }`; `type Decision struct{ ID, Kind, Question string; Tickets []Ticket }`.
  - `func Snapshot(ctx context.Context, q *gen.Queries, now time.Time) (map[string]*ProjectNow, error)`: one `ListFactoryTickets`, one `readiness.CheckAll` over the todo tickets of workflow projects, one open-decisions read (`ListDecisions` with `state` open and no project, plus `ListDecisionTicketKeys`), and the claimants' latest `meta` for harness and model (one batched read; leave them empty if none). Fixed number of queries.
  - Triage is a backlog-category status whose name `core.SameTag`-style equals "Needs decision" or "Needs design" (reuse the constant names `triage-statuses` uses). Reasons: with the workflow on, each `readiness` reason; for every project the board's conditions as "Blocked by another ticket", "Has open sub-tickets", "Claimed", "Assigned"; one ticket counts under each of its reasons. Stuck: Working or Review with `status_changed_at` more than 24 h before `now`.
- [ ] **Step 1: Write the failing tests** on a `dbtest` database arranged through core: `TestSnapshotStations` (one ticket per station lands there; a cancelled and an archived project's ticket appear nowhere); `TestSnapshotReasonsCountEach` (a workflow ticket with no tier and a blocker counts under both); `TestSnapshotWorkflowOffNoReadiness` (a workflow-off todo with no tier is Ready); `TestSnapshotStuckBoundary` (exactly 24 h is not stuck, 24 h + 1 s is); `TestSnapshotQueries` counts queries (as `TestProjectsStatsOneQuery` does) and holds them fixed for 1 and 50 tickets.
- [ ] **Step 2:** `go test ./internal/factory/` → FAIL.
- [ ] **Step 3:** Implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Factory: stations and reasons now (VAQ-154)`.

### Task 2: Counting empty ready answers

**Files:**
- Create: `internal/factory/misses.go`
- Modify: `internal/web/handlers/deps.go` (`ReadyMisses *factory.Misses`), `internal/web/handlers/api_claims.go` (`APIReady`), `internal/web/webtest/webtest.go` and `cmd/vaquero/serve.go` (one shared `*factory.Misses` for Deps and the sweep)
- Test: `internal/factory/misses_test.go`, `internal/web/handlers/api_claims_test.go`

**Interfaces:**
- Produces: `type Misses struct` (mutex inside, zero value usable); `func (m *Misses) Record(projectID string, at time.Time)` ("" for unscoped); `type Miss struct{ ProjectID string; Count int; Last time.Time }`; `func (m *Misses) Drain() []Miss` (returns and resets, sorted by ProjectID); `func (m *Misses) Restore(ms []Miss)` (adds drained counts back, keeping the later `Last`).
- `APIReady` calls `d.ReadyMisses.Record(…)` when its answer is empty (nil-safe when Deps has none).
- [ ] **Step 1: Write the failing tests**: `TestMissesDrain` (two records for VAQ and one unscoped drain as two Miss rows with counts 2 and 1 and the later time; a second Drain is empty); `TestMissesConcurrent` (`-race`, 100 goroutines); `TestMissesRestore`; `TestAPIReadyRecordsEmpty` (an empty `GET /api/v1/ready?project=VAQ` records VAQ, an unscoped one records "", a non-empty answer records nothing).
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** `go test -race ./internal/factory/ ./internal/web/handlers/ -run 'Misses|APIReady'` → PASS.
- [ ] **Step 5:** Gate, commit `Factory: count empty ready answers (VAQ-154)`.

### Task 3: Claims, line running, workers and pace

**Files:**
- Create: `internal/factory/claims.go`, `internal/factory/pace.go`; add queries to `internal/db/queries/factory.sql`
- Test: `internal/factory/claims_test.go`, `internal/factory/pace_test.go`

**Interfaces:**
- Produces:
  - sqlc `ListClaimEventsSince :many` (`ticket.claimed`, `ticket.released`, `ticket.claim_lapsed` since an instant, through `events_kind`, with actor kind and name, `meta`, ticket and project ids) and `ListTicketUpdatesFor :many` (`ticket.updated` events for a set of ticket ids since an instant, through `events_ticket`; `sqlc.slice`).
  - `type Interval struct{ ProjectID, TicketID, Agent, Harness, Model string; From, To time.Time }`.
  - `func ClaimIntervals(ctx context.Context, q *gen.Queries, since, now time.Time) ([]Interval, error)`: agent claims only (actor kind `agent`). A claim ends at the first of: `ticket.released` or `ticket.claim_lapsed` for that ticket, a `ticket.updated` whose `changes` has `assignee`, or `status` into a done or cancelled status (map names through the project's statuses now), a later `ticket.claimed` of the same ticket, or `now`. A claim begun before `since` is read from the claim event just before `since` (one more lookup per ticket set, batched).
  - `func HoursBusy(iv []Interval, from time.Time, hours int, projectID string) []bool` ("" any project): hour i is busy when any interval overlaps `[from+i h, from+(i+1) h)`.
  - `type Worker struct{ Agent, Harness, Model string; BusyHours float64; Finished int; Holds *Ticket }`; `func Workers(iv []Interval, finished map[string]int, now map[string]*ProjectNow) []Worker` ordered by BusyHours desc then Agent (`finished` keyed by agent: done transitions by that actor in the window; `now` is Task 1's Snapshot, for what each holds).
  - `func Pace(ctx context.Context, q *gen.Queries, projectID string, now time.Time, iv []Interval) (perHour float64, ok bool, err error)`: done transitions (a `ticket.updated` whose `changes.status.to` names a done status of its project) over the 7 days before `now`, divided by busy hours over the same window; `ok` false when either is zero. Runway hours = ready ÷ perHour.
- [ ] **Step 1: Write the failing tests** with a fixed clock: `TestClaimEndsOnRelease`, `TestClaimEndsOnLapse`, `TestClaimEndsOnReassign` and `TestClaimEndsOnDone` (Review Focus 2: the interval ends at the update, not at now), `TestClaimStillOpenEndsNow`, `TestClaimBegunBeforeWindow`, `TestOwnerClaimsDontCount`, `TestHoursBusyOverlap` (a claim 10:50–11:05 marks hours 10 and 11), `TestPaceNoHistory` (`ok` false), `TestPace` (6 done over 12 busy hours is 0.5).
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Factory: claim intervals, workers and pace (VAQ-154)`.

### Task 4: Samples (migration and sweep)

**Files:**
- Create: `internal/db/migrations/00NN_factory.sql` (next free number after VAQ-153's), `internal/sweep/sample.go`, `internal/factory/history.go`; add queries to `internal/db/queries/factory.sql`
- Modify: `internal/sweep/sweep.go` (`Tick` calls the sampler; `Sweeper` gains `Misses *factory.Misses`), `cmd/vaquero/serve.go`, `internal/archive/archive.go` (doc comment) and `archive_test.go` (`left`)
- Test: `internal/sweep/sample_test.go`, `internal/factory/history_test.go`

**Interfaces:**
- Migration (exact columns): `factory_samples (at TEXT NOT NULL, project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE, ready, not_ready, intake, triage, working, review, claims, open_decisions INTEGER NOT NULL, reasons TEXT NOT NULL /* JSON object reason → count */, PRIMARY KEY (project_id, at))` plus `CREATE INDEX factory_samples_at ON factory_samples (at)`; `ready_misses (at TEXT NOT NULL, project_id TEXT REFERENCES projects (id) ON DELETE CASCADE /* NULL: unscoped */, count INTEGER NOT NULL, last_at TEXT NOT NULL)` with `CREATE INDEX ready_misses_at ON ready_misses (at)`. `DeleteProject` needs no change (cascade); confirm with a test.
- Produces:
  - `sweep.SampleEvery`, `sweep.KeepSamples`; `func (s *Sweeper) sample(ctx context.Context) error`: at most once per `SampleEvery` (the first tick runs it): `factory.Snapshot` through `gen.New(s.DB)`, then in one transaction insert one `factory_samples` row per project and one `ready_misses` row per drained Miss, then prune both tables older than `KeepSamples`. On error it logs, puts the drained misses back (`Misses.Restore`), and returns nil so the rest of `Tick` runs.
  - `type Hour struct{ At time.Time; Ready int; Sampled, Busy bool }`; `type History struct{ Hours []Hour /* 24, oldest first */; Since time.Time /* first sample overall, zero if none */; EmptyHours, EmptyBusyHours int; Misses int; LastMiss time.Time }`.
  - `func LoadHistory(ctx context.Context, q *gen.Queries, projectID string /* "" global */, now time.Time, iv []Interval) (History, error)`: per hour the lowest summed ready over its samples (global: sum per sample instant across projects); `Sampled` false for hours with none; misses: global sums every row, a project sums its rows plus the unscoped ones.
- [ ] **Step 1: Write the failing tests**: `TestSampleWritesOneRowPerProject` (archived projects skipped); `TestSampleEveryFiveMinutes` (ticks at 0, 30 s, 5 min write two samples); `TestSampleFailureKeepsMisses` (a failing read leaves no rows, the misses come back on the next drain, `Tick` still prunes `mcp_calls`; Review Focus 5); `TestSamplesPruned`; `TestSamplesGoWithProject`; `TestHistoryLowestPerHour`; `TestHistoryNoSamples` (`Since` zero, no hour sampled; Review Focus 1); `TestHistoryMissesScope` (Review Focus 4: one unscoped and one VAQ miss: global 2, VAQ 2, OTH 1); `TestArchiveLeavesSamplesOut` via the existing `left` test.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS; `TestFixturesMigrateToHead` passes.
- [ ] **Step 5:** Gate, commit `Sweep: factory samples and empty ready answers (VAQ-154)`.

### Task 5: The global page (`/factory`)

**Files:**
- Create: `internal/web/handlers/factory.go`, `internal/web/templates/factory.templ`, `internal/web/templates/factory.go` (view types and cell helpers)
- Modify: `internal/web/router.go`, `internal/web/templates/layout.templ` (nav link after Activity), `internal/web/icons/ui.json`, `internal/web/static/css/input.css` (station tokens), `internal/help/articles/factory.md` (new, section `using`), `internal/help/articles/data-and-backups.md` (samples left out of exports)
- Test: `internal/web/handlers/factory_test.go`, `internal/web/templates/factory_test.go`

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces:
  - `templates.FactoryView{ Band Band; Lines []Line; Workers []factory.Worker; Now time.Time }`; `Band{ Ready int; RunwayHours float64; HasPace bool; History factory.History; Reasons []ReasonCount /* in readiness.Reasons order, then the board's */; Decisions []factory.Decision; RunningHours, RunningShare, LastWeekShare float64; ReasonsHref, ReadyHref string }`; `Line{ Project gen.Project; Now *factory.ProjectNow; Cells []string /* 40 class names */; RunwayHours float64; HasPace bool }`.
  - `func StationCells(counts map[factory.Station]int, n int) []string`: n cells split by largest remainder in `factory.Stations` order, each `bg-station-{name}`; a station with a non-zero count gets at least one cell when n allows.
  - `func StripHeight(ready, max int) string`: one of `h-1 h-2 h-3 h-5 h-8` (zero → the empty marker class).
  - Lines ordered by Ready + Working + Review desc, then key. Running share = busy hours ÷ 24; last week = busy hours over the 7 days before the last 24 ÷ 168.
  - Ready links to the busiest project's Focus (`/p/{key}`), reasons to its Focus with the Waiting lane open (`#lane-waiting` or the lane's existing anchor; add `data-keep` open by fragment if needed); decisions listed in a `<details>` with ticket links; the `Shipped` count links to `/activity?kind=tickets&from={today}`; lapsed claims to `/activity?kind=claims&from={today}`.
  - The page is live: `#factory` with `data-live`, `hx-get="/factory"`, `hx-trigger="vq:change"`, `hx-select="#factory"`.
- [ ] **Step 1: Write the failing tests**: `TestStationCells` (counts 17/1/4/9/2/1/6 over 40 cells sum to 40 and keep order; a count of 1 among large ones still gets a cell); `TestFactoryPageEmpty` (fresh instance: "History starts", "No pace yet.", 200; Review Focus 1); `TestFactoryPageBand` (arranged tickets and samples show the ready count, the reason counts with "A ticket can have more than one reason.", the decisions list, "Asked and found nothing: 1 time today"); `TestFactoryLines` (order, archived project absent, a claim shows agent, ticket key and time left; Review Focus 3); `TestFactoryNavLink`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS; `go test ./internal/help/` passes with the new article.
- [ ] **Step 5:** `make gen`, gate, commit `Web: the factory page (VAQ-154)`.

### Task 6: The project page, its tab, `g y` and browser tests

**Files:**
- Modify: `internal/web/handlers/factory.go` (`ProjectFactory`), `internal/web/router.go` (`/p/{key}/factory`), `internal/web/templates/factory.templ` (`ProjectFactoryPage`), `internal/web/templates/board.templ` (`projectHeader`: Factory tab after Activity), `internal/web/static/js/app.js` (`g y`), `internal/web/templates/layout.templ` (shortcuts dialog), `internal/help/articles/keyboard-shortcuts.md`, `internal/help/articles/factory.md`
- Test: `internal/web/handlers/factory_test.go`, `internal/e2e/factory_test.go`

**Interfaces:**
- Consumes: Task 5's view types; `ProjectFactoryPage(p gen.Project, v FactoryView)` with one Line, the band and workers scoped to `p`.
- Stations left to right as tiles (count, oldest ticket's age through `timefmt.Ago`, the working agents on the Working tile), alerts above; stacked below `md`.
- [ ] **Step 1: Write the failing tests**: `TestProjectFactoryScoped` (only VAQ's numbers; OTH's claim absent; misses are VAQ's plus unscoped); `TestProjectFactoryTab` (tab current, `/p/vaq/factory` redirects to `/p/VAQ/factory`); e2e `TestFactoryLive` (a claim made through `Deps.Core` appears without reload), `TestShortcutGoFactory` (`g y` on a project page opens `/p/KEY/factory`, elsewhere `/factory`), widths 375 px and 1280 px in both themes, no page wider than 375 px.
- [ ] **Step 2:** `go test ./internal/web/handlers/ -run Factory` and `make e2e` → FAIL.
- [ ] **Step 3:** Implement; help lines for `/factory`, the tab and `g y`.
- [ ] **Step 4:** → PASS.
- [ ] **Step 5:** `make ci`, update CLAUDE.md's package map (`internal/factory`, the sweep's sampler, the two tables, the routes and tab), commit `Web: the project factory tab and g y (VAQ-154)`.
