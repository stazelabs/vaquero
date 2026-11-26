# Agent workflow, part 3: runs — implementation plan (VAQ-137)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Runs as records: a named set of tickets on one branch, in batches, with states from planned to released; the merge gate (an answered merge approval plus git confirming the merge) and the release gate (a project approval naming a version and the runs it covers, plus the tag); run pages and the run on tickets; the CLI, API and MCP to drive them.

**Architecture:** Core owns `runs` and `run_tickets` and every state change, checking the gates through `Core.Git`. Release approvals gain a version and their runs in `release_runs`. The sweep reconciles answered release approvals whose tag has since appeared. Pages and the API read through one helper.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc, templ, htmx, `internal/gitread` (Part 2).

**Spec:** `docs/superpowers/specs/2026-11-25-agent-workflow-design.md`, Part 3 (and Decision 9, provenance's run, Part 5's run pages). Ticket: VAQ-137.

## Rulings (controller, 2026-11-26; the owner can overrule)

- **P3-R1: a run is named within its project.** `runs (project_id, name)` is unique; names are lower-case `[a-z0-9-]`, at most 40 characters (`run-p`). `decisions.run_id` stays TEXT and means the run's **name** in the decision's project (no foreign key), so decisions already asked with a run name (`coreseed`'s `run-1`, the owner's data) link up when a run of that name is created, and no migration rewrites them. `Origin.Run` (`Vaquero-Run`) is a name too.
- **P3-R2: transitions.** `planned → running → reviewing → awaiting merge → merged → released`; `reviewing → running` and `awaiting merge → running` (fix rounds); any state before `merged` → `abandoned`. `merged` and `released` only through their gates; nothing leaves `released` or `abandoned`.
- **P3-R3: a ticket is in at most one open run** (open: any state before `merged`, and not `abandoned`). Adding it to a second is refused ("VAQ-12 is in run-o already."). `design`-tier tickets are refused ("Design-tier tickets are planned, not run."); requirements (VAQ-2, when built) too.
- **P3-R4: the merge gate.** `merged` needs an answered merge approval with the run's name, answered Approve, not superseded by a later merge approval for the run answered otherwise, and `Core.Git` confirming the run's branch is an ancestor of main (`Repo.Merged`). Without a repo path: "Set the project's repo path to confirm the merge." Refusals name the missing part.
- **P3-R5: releases.** A release approval (subject `release`) takes `version` (a tag name, `gitread.ValidRev`) and `runs` (names, each `merged` in the project). Answered Approve with the tag present, each covered run moves to `released` in the same transaction as the answer; answered Approve without the tag, the runs move when the sweep next finds it (`ReconcileReleases`, every tick, one batched read). Answering anything else moves nothing.
- **P3-R6: provenance.** With no `Vaquero-Run` header, an event on a ticket in exactly one open run records that run's name as `meta.run`.
- **P3-R7: moving between projects.** `inOpenRun` (moveproject.go's hook) is filled: a ticket or sub-ticket in an open run can't move ("VAQ-12 is in run-o; remove it from the run first.").

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; page or template changes also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-137)`, ending with the `Co-Authored-By` line.
- Only core writes the new tables; one event per change: `run.created`, `run.updated`, `run.state_changed`, `run.ticket_added`, `run.ticket_removed` (spec), payloads `{run_id, name, …}`; a release moving runs rides on `decision.answered` (payload `released_runs`) or on one `run.state_changed` per run from the sweep.
- Vaquero never merges, tags or pushes.
- The migration is the next free number when built; export and import carry `runs`, `run_tickets`, `release_runs`; `coreseed` seeds a merged run named `run-1` (so its decision links), one running run with two batches, and a release approval.
- `TestWorkflowOffUnchanged` holds: run marks appear on cards only for tickets in a run.

## Review Focus

1. **A merge approval answered Approve, then the branch rebased and force-pushed** so the approved commit is gone: `merged` is still refused until main contains the branch head as it is now. Test in Task 2.
2. **A release naming a run that isn't merged, or another project's run:** the ask is refused naming the run. Test in Task 3.
3. **The tag pushed an hour after the release approval:** the sweep moves the runs; a second tick writes nothing. Test in Task 3.
4. **Removing the last ticket of a running run, then adding it back:** batch and order kept as given; history shows both. Test in Task 1.
5. **A run name typed in another case (`Run-P`):** normalised to lower case on create and lookup. Test in Task 1.

---

### Task 1: Runs and their tickets (core)

**Files:**
- Create: `internal/db/migrations/00NN_runs.sql`, `internal/db/queries/runs.sql`, `internal/core/runs.go`
- Modify: `internal/core/events.go` (kinds, payload docs, `EventKinds`), `internal/core/moveproject.go` (`inOpenRun`), `internal/core/origin.go` or `emit` (P3-R6), `internal/archive/archive.go`, `internal/core/coreseed/seed.go`, `internal/web/templates/activity.go` (`EventSentence`)
- Test: `internal/core/runs_test.go`, `TestEveryOperationEmitsOneEventOrNothing`

**Interfaces:**
- Migration: `runs (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects (id), name TEXT NOT NULL, state TEXT NOT NULL CHECK (state IN ('planned','running','reviewing','awaiting merge','merged','released','abandoned')), branch TEXT NOT NULL DEFAULT '', plan_ticket_id TEXT REFERENCES tickets (id) ON DELETE SET NULL, created_by TEXT NOT NULL REFERENCES actors (id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE (project_id, name))`; `run_tickets (run_id TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE, ticket_id TEXT NOT NULL REFERENCES tickets (id) ON DELETE CASCADE, batch INTEGER NOT NULL CHECK (batch >= 1), position INTEGER NOT NULL, PRIMARY KEY (run_id, ticket_id))`, index `run_tickets_ticket (ticket_id)`; `release_runs (decision_id TEXT NOT NULL REFERENCES decisions (id), run_id TEXT NOT NULL REFERENCES runs (id), PRIMARY KEY (decision_id, run_id))`; `decisions.version TEXT NOT NULL DEFAULT ''`.
- Produces:
  - `var RunStates = []string{"planned", "running", "reviewing", "awaiting merge", "merged", "released", "abandoned"}`; `func OpenRunState(s string) bool`.
  - `type RunInput struct{ Name, Branch, PlanTicket string /* key */ }`; `func (c *Core) CreateRun(ctx, by, projectID string, in RunInput) (gen.Run, error)`; `func (c *Core) UpdateRun(ctx, by, runID string, branch, planTicket *string) (gen.Run, error)`.
  - `func (c *Core) AddRunTickets(ctx, by, runID string, batch int, keys []string) error` (positions after the batch's last; P3-R3 refusals; one `run.ticket_added` event listing them); `func (c *Core) RemoveRunTicket(ctx, by, runID, key string) error`.
  - `func (c *Core) SetRunState(ctx, by, runID, state string) (gen.Run, error)` for every transition except `merged` and `released` (P3-R2; "A run moves to merged through its merge gate." for those).
  - `func ResolveRun(ctx, q *gen.Queries, projectID, name string) (gen.Run, error)` (lower-cases; `ErrNotFound`).
  - Who: any actor creates and drives runs; `abandoned` too.
- [ ] **Step 1: Write the failing tests**: `TestCreateRunNames` (case folded, invalid names refused, duplicate refused; Review Focus 5); `TestRunTransitions` (table of every pair allowed and refused); `TestTicketInOneOpenRun`; `TestDesignTierRefused`; `TestRemoveAndReadd` (Review Focus 4); `TestRunProvenance` (an event with no header records the run; with a header, the header); `TestMoveInOpenRunRefused`; `TestDecisionLinksByName` (a decision asked with `run-1` before the run exists lists under it after); events one per op.
- [ ] **Step 2:** `go test ./internal/core/ -run Run` → FAIL.
- [ ] **Step 3:** Implement; `make gen`. **Step 4:** `go test ./internal/core/... ./internal/archive/...` → PASS; fixtures migrate.
- [ ] **Step 5:** Gate, commit `Core: runs and the tickets in them (VAQ-137)`.

### Task 2: The merge gate

**Files:**
- Modify: `internal/core/runs.go` (`MergeRun`), `internal/db/queries/runs.sql`
- Test: `internal/core/runs_merge_test.go` (with gitread's test repo helpers)

**Interfaces:**
- Produces: `func (c *Core) MergeRun(ctx, by, runID string) (gen.Run, error)`: from `awaiting merge` only; P3-R4's checks in order (an Approve answer on the latest answered merge approval for the run; a repo path; `Core.Git` → `Merged(branch)` true), each refusal its own message ("No approved merge for run-o yet.", "Set the project's repo path to confirm the merge.", "run-o's branch isn't merged into main yet."); one `run.state_changed` event `{from, to: "merged", approval_id, branch_head}`.
- [ ] **Step 1: Write the failing tests**: `TestMergeNeedsApproval`; `TestMergeLatestApprovalCounts` (Approve then Request changes → refused); `TestMergeNeedsGit` (no repo path; branch not merged; merged → ok); `TestMergeAfterForcePush` (Review Focus 1: approve, rewrite branch, not merged → refused; merge the new head → ok); `TestMergeOnlyFromAwaitingMerge`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Core: the merge gate (VAQ-137)`.

### Task 3: Releases

**Files:**
- Modify: `internal/core/decisions.go` (`DecisionInput.Version`, `.Runs []string`; `checkDecision` for subject release; `AnswerDecision` moves runs), `internal/core/runs.go` (`ReconcileReleases`), `internal/sweep/sweep.go` (`Tick` calls it after `ExpireClaims`)
- Test: `internal/core/releases_test.go`, `internal/sweep/sweep_test.go`

**Interfaces:**
- Produces:
  - Asking a release: `version` required (`ValidRev`: "Give the version tag, such as v0.1.7."), `runs` required, each a `merged` run of the project ("run-q isn't merged."; Review Focus 2); rows in `release_runs`.
  - Answer Approve: if `Core.Git` finds the tag (`TagExists`), each covered run → `released` in the same tx, the answer's payload gaining `released_runs`.
  - `func (c *Core) ReconcileReleases(ctx context.Context) (int, error)`: approved release approvals with covered runs still `merged` and the tag now present → `released`, one `run.state_changed` per run; no repo or no tag: nothing.
- [ ] **Step 1: Write the failing tests**: `TestReleaseAskRefusals`; `TestReleaseWithTagMovesRuns`; `TestReleaseWithoutTagWaits` then `TestReconcileReleases` (tag added → moved; second call writes nothing; Review Focus 3); `TestReleaseRequestChangesMovesNothing`; `TestSweepReconciles`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Core: release approvals move merged runs to released (VAQ-137)`.

### Task 4: API, CLI and MCP

**Files:**
- Create: `internal/web/handlers/api_runs.go` (with `func runOf(ctx context.Context, d Deps, p gen.Project, r gen.Run) (api.Run, error)`, shared with Task 5), `cmd/vaquero/cli_runs.go`
- Modify: `internal/api/types.go` (`Run{Name, Project, State, Branch, Plan, Batches [][]RunTicket, Decisions []Decision, Commits []Commit, CreatedBy, CreatedAt}`, `RunTicket{Key, Title, Status, Tier, Model /* planned */, Models []string /* from events.meta on the ticket during the run */}`; `Ticket.Run` (omitted when none); `Decision.Version`, `.Runs`), `internal/web/router.go` (`GET`/`POST /projects/{key}/runs`, `GET`/`PATCH /projects/{key}/runs/{name}`, `POST …/{name}/tickets`, `DELETE …/{name}/tickets/{ticket}`, `POST …/{name}/state`, `POST …/{name}/merge`), `internal/web/handlers/api_decisions.go` (`version`, `runs`), `internal/mcpserver/tools.go` (`get_run`, `update_run` with actions create, add, remove, state, merge), `cmd/vaquero/cli.go`, `internal/help/articles/agent-workflow.md` ("Runs"), `cli.md`, `api.md`, `mcp.md`
- Test: `internal/web/handlers/api_runs_test.go`, `cmd/vaquero/cli_runs_test.go`, `internal/mcpserver/tools_test.go`

**Interfaces:**
- CLI: `vaquero runs KEY`; `vaquero run new KEY NAME [--branch B] [--plan TICKET]`; `vaquero run show KEY NAME`; `vaquero run add KEY NAME TICKET... [--batch N]` (default the last batch, or 1); `vaquero run remove KEY NAME TICKET`; `vaquero run state KEY NAME STATE`; `vaquero run merge KEY NAME`; `vaquero decision ask … --subject release --version V --run NAME...`.
- Commits: `gitread` `BranchCommits(branch, 100)` while the run is open, the merged range after (empty without a repo path).
- [ ] **Step 1: Write the failing tests**: each route's success and core refusals as 422 or 409; `Ticket.Run` absent for tickets in no run (workflow-off goldens unchanged); CLI outputs and exit codes; MCP schemas and calls; help tests name every new command, route and tool.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `API, CLI, MCP: runs (VAQ-137)`.

### Task 5: Run pages and the run on tickets

**Files:**
- Create: `internal/web/handlers/runs.go`, `internal/web/templates/runs.templ`
- Modify: `internal/web/router.go` (`/p/{key}/runs`, `/p/{key}/runs/{name}`), `internal/web/templates/board.templ` (`cardItem`: the run's name with an icon when in an open run; `projectHeader`: a Runs link beside Settings while the project has any run), `internal/web/templates/tickets.templ` (the run on the ticket page), `internal/board/board.go` and `focus.go` (fill `FlightGroup.Run`), `internal/web/icons/ui.json`
- Test: `internal/web/handlers/runs_test.go`, `internal/e2e/runs_test.go`

**Interfaces:**
- Consumes: Task 4's `runOf`.
- Runs list: open runs first (by state order), then merged, released, abandoned folded; rows `run-p · running · batch 2 of 3 · 7 tickets · branch run-p`.
- Run page: header (name, state, branch, plan link), batches with each ticket's status, planned tier and model, the models that worked it (from provenance) with a mismatch noted ("planned heavy, worked by a light model" in plain words from the tier map), decisions and approvals on the run with their state, the commits; live (`data-live`).
- [ ] **Step 1: Write the failing tests**: list grouping; page sections with a test repo; card mark only for open runs; Focus's In flight shows the run; e2e at 375 px and 1280 px, both themes.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS (`make e2e`).
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (core runs, the gates, the sweep's reconcile, the routes, the pages; P3 rulings under the agent workflow invariants); commit `Web: run pages and runs on tickets (VAQ-137)`.
