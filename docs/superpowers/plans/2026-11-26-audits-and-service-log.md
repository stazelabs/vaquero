# Audit records and the factory service log Implementation Plan (VAQ-105)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record each audit (kind, when, who, commit, note, tickets filed), say when each kind is due per project, expose it on the CLI, API and MCP, and show it as a Service log panel on the project's Factory page.

**Architecture:** Core owns the record (`audits`, `audit_tickets`, `projects.audit_policy`) with one `RecordAudit` operation and one `audit.recorded` event; due-ness is a pure function computed on read. A small cached git count gives "commits since". The API is the one reader every surface goes through; the Factory page renders the panel from the same read.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc, templ, htmx, `internal/gitread`.

**Spec:** `docs/superpowers/specs/2026-11-26-audits-and-service-log-design.md`. **Built after** run P (`event_tickets`, P-R1 in `docs/superpowers/plans/2026-11-26-run-p.md`), VAQ-153 (`docs/superpowers/plans/2026-11-26-activity-page.md`: `activity.Groups`) and, for Task 5, VAQ-154 (`docs/superpowers/plans/2026-11-26-factory-view.md`: the project Factory page and `factory.ProjectNow` alerts).

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; page or template changes also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-105)`, ending with the `Co-Authored-By` line.
- Only `internal/core` writes `audits`, `audit_tickets` and `audit_policy`; one event per successful record; a refused one writes nothing (`TestEveryOperationEmitsOneEventOrNothing` gains the operation).
- Kinds exactly `qa`, `hygiene`, `continuity`, `docs` (`core.AuditKinds`, in that order). Defaults: qa every 3 merged runs or 7 days; the others every 5 merged runs or 14 days.
- Commit at most 64 characters, hex only (`gitread.ValidRev`-style check), "" allowed; note at most 500 characters, NFC, no control characters (`core.HasControl`).
- Owner-only refusal (exact): "Only the owner changes the audit schedule."
- Due only with the workflow on; merged-run counts are 0 until Part 3 (a `core.MergedRunsSince` hook returning 0, which Part 3 fills).
- The migration is the next free number when built; export and import include `audits` and `audit_tickets` (`archive.Tables`, after `decisions` group, before `events`); `coreseed.Seed` records one audit with one ticket.
- Theme tokens only, no `style=`; due is marked with words and an icon, never colour alone.

## Review Focus

1. **Recording with a ticket from another project, an unknown ticket, or an old key of a moved-in ticket:** the first two are a 422 naming the ticket; the old key resolves (`core.ResolveTicket`) to the ticket now. Test in Task 1.
2. **A kind that has never run, with the workflow on vs off:** on, "due" with reason "Never run."; off, not due and no reason. Test in Task 1.
3. **An abbreviated or unknown commit, and a project with no repo path:** "commit not on main" or no count, never an error on the panel or the API. Test in Task 2.
4. **An agent changing the schedule** through the API, `--agent` or MCP: refused with the exact message; recording an audit as an agent is allowed. Test in Task 1.
5. **Two records of the same kind in one second:** the later by `at`, then by id, is "last". Test in Task 1.

---

### Task 1: The record, the schedule and due-ness (core)

**Files:**
- Create: `internal/db/migrations/00NN_audits.sql`, `internal/db/queries/audits.sql`, `internal/core/audits.go`
- Modify: `internal/core/projects.go` (`ProjectPatch.AuditPolicy *string`, owner check beside the tier map's), `internal/core/events.go` (`EvAuditRecorded = "audit.recorded"`, payload doc, `EventKinds`), `internal/core/projects.go` `DeleteProject` (audits and their rows), `internal/archive/archive.go` (`Tables`), `internal/core/coreseed/seed.go`, `internal/web/templates/activity.go` (`EventSentence`: "recorded a hygiene audit at a1b2c3d, filing 3 tickets" / "…, filing no tickets"; `EventIcon`)
- Test: `internal/core/audits_test.go`, existing `TestEveryOperationEmitsOneEventOrNothing`, `TestSeedCarriesThisReleasesPayloads`, archive round-trip tests

**Interfaces:**
- Migration: `audits (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects (id), kind TEXT NOT NULL CHECK (kind IN ('qa','hygiene','continuity','docs')), at TEXT NOT NULL, actor_id TEXT NOT NULL REFERENCES actors (id), commit_sha TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '')`, index `audits_project_kind (project_id, kind, at, id)`; `audit_tickets (audit_id TEXT NOT NULL REFERENCES audits (id), ticket_id TEXT NOT NULL REFERENCES tickets (id) ON DELETE CASCADE, PRIMARY KEY (audit_id, ticket_id))`; `ALTER TABLE projects ADD COLUMN audit_policy TEXT NOT NULL DEFAULT '' CHECK (audit_policy = '' OR json_valid(audit_policy))`.
- Produces:
  - `var AuditKinds = []string{"qa", "hygiene", "continuity", "docs"}`.
  - `type AuditInput struct{ Kind, Commit, Note string; Tickets []string /* keys as typed */ }`; `func (c *Core) RecordAudit(ctx context.Context, actorID, projectID string, in AuditInput) (gen.Audit, error)`: payload `{audit_id, kind, commit, tickets /* keys */, ticket_ids}`; refuses an archived project ("This project is archived.").
  - `type AuditCadence struct{ Runs, Days int }`; `type AuditPolicy map[string]AuditCadence`; `var DefaultAuditPolicy AuditPolicy`; `func ParseAuditPolicy(s string) (AuditPolicy, error)` (unknown kind, negative or over 365 days / 100 runs refused as `*Invalid` on `audit_policy`); `func EffectiveAuditPolicy(stored string) AuditPolicy` (missing kinds take the default).
  - `type AuditStatus struct{ Kind string; Last *gen.Audit; LastActor string; Tickets int; Due bool; DueReason string /* "Never run.", "14 days since the last one.", "5 merged runs since the last one." */; DueAt time.Time /* zero when unknown */ }`.
  - `func AuditDue(kind string, last *gen.Audit, policy AuditPolicy, workflow bool, mergedSince int, now time.Time) (due bool, reason string, dueAt time.Time)`: pure.
  - `var MergedRunsSince = func(ctx context.Context, q *gen.Queries, projectID string, since time.Time) (int, error) { return 0, nil }` (Part 3 replaces it).
  - sqlc: `ListLatestAudits(project_id)` (latest per kind, ties by `at` then `id` desc, with actor name and ticket count), `ListAuditHistory(project_id, kind, lim)`.
- [ ] **Step 1: Write the failing tests**: `TestRecordAudit` (row, `audit_tickets`, one event with the payload and origin `meta`); `TestRecordAuditTickets` (other project's ticket and unknown key → `*Invalid` naming it; an old key resolves; Review Focus 1); `TestRecordAuditAsAgent` (allowed); `TestAuditPolicyOwnerOnly` (agent `UpdateProject{AuditPolicy}` → "Only the owner changes the audit schedule."; Review Focus 4); `TestParseAuditPolicy` (table); `TestAuditDue` (never run on → "Never run."; off → not due; 14 days exactly due; days 0 and runs 0 → never due; Review Focus 2); `TestLatestAuditTie` (Review Focus 5); `TestDeleteProjectRemovesAudits`; `TestMoveKeepsAuditTickets` (a filed ticket moved away still counts).
- [ ] **Step 2:** `go test ./internal/core/ -run Audit` → FAIL.
- [ ] **Step 3:** Implement; `make gen`. **Step 4:** `go test ./internal/core/... ./internal/archive/... ./internal/db/...` → PASS (fixtures migrate).
- [ ] **Step 5:** Gate, commit `Core: audit records, the audit schedule and due-ness (VAQ-105)`.

### Task 2: Commits since an audit (`internal/gitread` and a cache)

**Files:**
- Modify: `internal/gitread/ops.go`
- Create: `internal/web/handlers/audit_since.go`
- Test: `internal/gitread/ops_test.go`, `internal/web/handlers/audit_since_test.go`

**Interfaces:**
- Produces:
  - `func (g *Repo) CommitsSince(ctx context.Context, commit string) (n int, ok bool, err error)`: resolves `commit` to one full id (`rev-parse --verify --quiet {commit}^{commit}`; ambiguous or missing → `ok` false, nil error), checks it is an ancestor of main (`merge-base --is-ancestor`; not → `ok` false), then `rev-list --count {id}..{main}`. Only allow-listed subcommands.
  - `type sinceCache struct` in handlers (mutex, map keyed by `projectID+"\x00"+mainHead+"\x00"+commit`, at most 256 entries, oldest dropped); `func (d Deps) commitsSince(ctx context.Context, p gen.Project, commit string) (n int, ok bool)`: no repo path, no commit, git unavailable or any error → `ok` false; reads main's head once per call (`Head`) to key the cache.
- [ ] **Step 1: Write the failing tests** with the gitread test repo helpers: `TestCommitsSince` (3 commits after → 3); `TestCommitsSinceAbbreviated` (a unique 7-character prefix works; an ambiguous or unknown one is `ok` false); `TestCommitsSinceNotOnMain` (a branch commit → `ok` false); handlers `TestCommitsSinceCached` (a second call with main unchanged runs no git; after a new commit on main it recounts), `TestCommitsSinceNoRepo` (Review Focus 3).
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Git: commits since an audit, cached by main's head (VAQ-105)`.

### Task 3: API, CLI and MCP

**Files:**
- Create: `internal/web/handlers/api_audits.go`, `cmd/vaquero/cli_audits.go`
- Modify: `internal/web/router.go` (`mountAPI`: `GET`/`POST /projects/{key}/audits`), `internal/api/types.go` (`Audit`, `AuditStatus`; `Project.AuditPolicy`), `internal/web/handlers/api_write.go` (project PATCH takes `audit_policy`), `internal/mcpserver/tools.go` (`record_audit`), `cmd/vaquero/cli.go` (dispatch `audit`, `audits`), `internal/help/articles/cli.md`, `api.md`, `mcp.md`, `agent-workflow.md` ("Audits" section)
- Test: `internal/web/handlers/api_audits_test.go`, `cmd/vaquero/cli_audits_test.go`, `internal/mcpserver/tools_test.go`

**Interfaces:**
- Consumes: Task 1's core calls and `AuditDue`; Task 2's `commitsSince`.
- Produces:
  - `POST /api/v1/projects/{key}/audits` body `{kind, commit?, note?, tickets?: [keys]}` → 201 `api.Audit{ID, Project, Kind, At, Actor, Commit, Note, Tickets []string}`.
  - `GET /api/v1/projects/{key}/audits` → `[]api.AuditStatus{Kind, Last *api.Audit, Due bool, DueReason string, DueAt string?, CommitsSince *int}` in `AuditKinds` order; `?kind=K` → that kind's `[]api.Audit`, newest first, `limit` default 20 (max `maxAPIList`); unknown kind a 422 naming `kind`.
  - `vaquero audit record KEY KIND [--commit SHA] [--note TEXT|-] [--ticket TICKET]...` prints "Recorded a hygiene audit of VAQ."; `vaquero audits KEY` prints a table `KIND  LAST  BY  COMMIT  TICKETS  DUE` (DUE: "due: Never run.", "in 5 days", or "-" with the workflow off).
  - MCP `record_audit` (`project`, `kind`, `commit?`, `note?`, `tickets?`) → one `POST`.
- [ ] **Step 1: Write the failing tests**: API record and read, the 422s, an agent token recording (allowed) and patching `audit_policy` (422 with the owner message); `TestAPIAuditsCommitsSince` (with a test repo: `commits_since` set; without: absent); CLI output and `--note -`; MCP schema lists `record_audit` and its call; the help tests name the new command, routes and tool.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `API, CLI, MCP: record and read audits (VAQ-105)`.

### Task 4: The schedule in settings and the Activity group

**Files:**
- Modify: `internal/web/templates/projects.templ` (`workflowSection`: an "Audit schedule" table, one row per kind with "Every N merged runs" and "or D days" number inputs, shown only with the workflow on), `internal/web/handlers/projects.go` (`POST /p/{key}/audits/schedule` → `core.UpdateProject{AuditPolicy}`; storing "" when every row equals the default), `internal/web/router.go`, `internal/activity/groups.go` (group `releases`, label "Releases, runs and audits", kinds `audit.recorded`; Part 3 adds its kinds there)
- Test: `internal/web/handlers/projects_test.go`, `internal/activity/groups_test.go` (existing every-kind test now covers `audit.recorded`)

- [ ] **Step 1: Write the failing tests**: the section shows defaults with the workflow on and is absent with it off; a save stores the policy and a toast "Audit schedule saved."; a save equal to the defaults stores ""; an invalid number re-renders with the field error; `TestEveryKindInOneGroup` passes with the new group.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS; `make e2e` for the settings page widths.
- [ ] **Step 5:** Gate, commit `Web: the audit schedule and the releases, runs and audits group (VAQ-105)`.

### Task 5: The Service log panel (after VAQ-154)

**Files:**
- Modify: `internal/web/handlers/factory.go` (`ProjectFactory` reads the latest audits and due-ness through the same helper as `GET …/audits`: extract it as `func auditStatuses(ctx context.Context, d Deps, p gen.Project) ([]api.AuditStatus, error)` in `api_audits.go` and use it in both), `internal/web/templates/factory.templ` (`serviceLog` after the stations), `internal/factory/snapshot.go` (`ProjectNow.AuditsDue int`, counted with the workflow on, for the global page's alerts), `internal/web/icons/ui.json` (due icon), `internal/help/articles/factory.md` ("Service log")
- Test: `internal/web/handlers/factory_test.go`, `internal/e2e/factory_test.go`

**Interfaces:**
- Consumes: Task 3's `auditStatuses`; VAQ-154's `ProjectFactoryPage` and `Line` alerts.
- Panel rows per the spec's sketch: kind label ("QA", "Hygiene", "Continuity", "Docs"), `timefmt.Ago` of the last, who ("you" for the owner), the commit's first 7 characters, "N tickets" (real counts), then "due" with the reason as title and the due icon, "due in N d", or nothing with the workflow off; "· N commits since" or "· commit not on main" when the repo answers. Each row links to `/activity?project=KEY&kind=releases`. A workflow-off project with no audit records shows no panel. Release and Run rows are not rendered until Part 3 provides them.
- [ ] **Step 1: Write the failing tests**: panel rows for a recorded and a never-run kind; no panel for a workflow-off project without records, a panel without due marks for one with records; the global page's project line says "2 audits due"; e2e at 375 px and 1280 px, both themes.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS (`make e2e`).
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (core audits, the tables, `CommitsSince`, the routes, the panel); commit `Web: the factory service log (VAQ-105)`.
