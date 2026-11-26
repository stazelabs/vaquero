# Structured requirements Implementation Plan (VAQ-2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ticket kinds (`task`, `requirement`), acceptance criteria as tracked items (`AC-n` markers core writes into descriptions, plain or Given/When/Then), each criterion's state and evidence, `implements` links, the owner's acceptance of requirements, warnings on review or done with open criteria, and a Requirements view, across web, API, CLI and MCP.

**Architecture:** A pure `internal/criteria` package parses and marks the "Acceptance criteria" section. Core runs it inside every description save, keeps `criteria` and `criterion_evidence` in step, and enforces the kind and owner rules. Checks on evidence are worked out on read through `internal/gitread`, cached by main's head, like references.

**Tech Stack:** Go 1.27, SQLite (modernc), sqlc, goldmark (already used for parsing), templ, htmx.

**Spec:** `docs/superpowers/specs/2026-11-26-requirements-design.md`.

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`; page, template or app.js changes also run `make e2e`.
- Commit messages `{Area}: {summary} (VAQ-2)`, ending with the `Co-Authored-By` line.
- Only core writes `kind`, `next_ac`, `criteria`, `criterion_evidence` and the markers; one event per change (marking inside a description save rides on its `ticket.updated`, payload gaining `criteria: {added, removed, reset}` numbers).
- `TestWorkflowOffUnchanged` stays byte for byte: answers omit `kind` and `criteria` for a task with no criteria; ticket lists default to `kind=task`.
- Messages exact: "Requirements aren't claimed; claim a ticket that implements it.", "Only the owner accepts or retires a requirement.", "A requirement isn't part of a ticket tree; link tickets to it with implements.", "Remove its implements links first.", "Add evidence: a test, a commit, a review or a note.", "Only the owner waives a criterion.", the warning "{n} of {m} acceptance criteria aren't met.", the legacy evidence note "Checked in the text before criteria were tracked."
- Marker form `AC-{n}: ` right after the list bullet; numbers per ticket, never reused.
- The migration is the next free number when built; export and import carry the new tables and columns; `coreseed` seeds a requirement with criteria (one Given/When/Then), an implementing task and evidence.

## Review Focus

1. **A description with an "Acceptance criteria" heading inside a fenced code block**, or a list under another heading: never parsed as criteria. Test in Task 2.
2. **An agent rewriting the whole description with markers stripped and one item reworded:** unchanged items keep their numbers and states; the reworded one is new and the old one `removed`. Test in Task 3.
3. **Two saves racing on the same ticket** (version check): the stale one is refused before any marking, so numbers never collide. Test in Task 3.
4. **Evidence naming a test file that's on a branch but not main, or a commit not yet merged:** shown unchecked, still counts; checked once merged, with no new save. Test in Task 4.
5. **A requirement moved to done by an agent through bulk_update:** refused per item with the owner message, the rest of the batch unaffected. Test in Task 1.

---

### Task 1: Kinds, the rules around them, and `implements`

**Files:**
- Create: `internal/db/migrations/00NN_requirements.sql`, `internal/core/kinds.go`
- Modify: `internal/core/tickets.go` (create/patch: `Kind`; tree rule; status rule), `internal/core/claims.go` (refuse requirements), `internal/core/links.go` (`implements`), `internal/core/moveproject.go` (refuse with links), `internal/db/queries/*.sql` (board, Focus, list, ready, stats and readiness reads filter `kind = 'task'`), `internal/archive/archive.go` (`Tables`), `internal/core/coreseed/seed.go`
- Test: `internal/core/kinds_test.go`, `internal/board/*_test.go`, `internal/web/handlers` workflow-off goldens unchanged

**Interfaces:**
- Migration: `tickets.kind TEXT NOT NULL DEFAULT 'task' CHECK (kind IN ('task','requirement'))`, `tickets.next_ac INTEGER NOT NULL DEFAULT 1`, index `tickets_kind (project_id, kind)`; `ticket_links.kind` CHECK widened with `implements`; `criteria (ticket_id TEXT NOT NULL REFERENCES tickets (id) ON DELETE CASCADE, number INTEGER NOT NULL, text TEXT NOT NULL, form TEXT NOT NULL CHECK (form IN ('line','gwt')), state TEXT NOT NULL CHECK (state IN ('open','met','waived','removed')), updated_at TEXT NOT NULL, updated_by TEXT NOT NULL REFERENCES actors (id), PRIMARY KEY (ticket_id, number))`; `criterion_evidence (id TEXT PRIMARY KEY, ticket_id TEXT NOT NULL, number INTEGER NOT NULL, kind TEXT NOT NULL CHECK (kind IN ('test','commit','review','note')), target TEXT NOT NULL, by TEXT NOT NULL REFERENCES actors (id), at TEXT NOT NULL, FOREIGN KEY (ticket_id, number) REFERENCES criteria (ticket_id, number) ON DELETE CASCADE)`.
- Produces: `var Kinds = []string{"task", "requirement"}`; `TicketInput.Kind`, `TicketPatch.Kind *string` (change only while no `implements` links either way); `core.LinkKinds` gains `implements` (task → requirement, same project only: "A ticket implements a requirement in its own project."); `func IsRequirement(t gen.Ticket) bool`.
- [ ] **Step 1: Write the failing tests**: `TestRequirementOffBoardFocusReady` (a requirement in todo is on no board column, not in Focus, not in `ready`, not in `ListBoardReadyTickets`, not in project stats); `TestRequirementNotClaimed`; `TestRequirementTreeRefused` (parent set on a requirement, a requirement as parent); `TestRequirementStatusOwnerOnly` (agent into done, out of done, into cancelled → owner message; agent backlog → todo allowed; the same through `bulk_update`, Review Focus 5); `TestImplementsLink` (task → requirement ok; requirement → task, task → task, other project refused); `TestMoveWithImplementsRefused`; `TestKindChangeWithLinksRefused`; workflow-off goldens unchanged.
- [ ] **Step 2:** `go test ./internal/core/ ./internal/board/ ./internal/web/handlers/ -run 'Requirement|Implements|Kind|WorkflowOff'` → FAIL.
- [ ] **Step 3:** Implement; `make gen`. **Step 4:** → PASS; `go test ./...`.
- [ ] **Step 5:** Gate, commit `Core: ticket kinds, requirements and implements links (VAQ-2)`.

### Task 2: Parsing and marking criteria (`internal/criteria`)

**Files:**
- Create: `internal/criteria/criteria.go`, `internal/criteria/criteria_test.go`

**Interfaces:**
- Produces:
  - `type Item struct{ Number int /* 0 when unmarked */; Text string /* first line, marker and checkbox removed */; Form string /* "line" | "gwt" */; Lines []string /* continuation lines */; Checked *bool /* a legacy [ ]/[x], nil when none */ }`.
  - `func Parse(body string) []Item`: top-level list items under the first heading whose text, case-folded and trimmed, is "acceptance criteria", up to the next heading of the same or higher level; headings and lists inside fenced code are ignored (goldmark AST, Review Focus 1). `gwt` when continuation lines start with Given/When/Then/And/But (any case).
  - `type Known struct{ Number int; Text string; State string }`.
  - `type Result struct{ Body string; Items []Item /* all numbered */; Added, Removed, Reworded []int; Rematched []int; Legacy map[int]bool /* number → was checked */ }`.
  - `func Mark(body string, known []Known, next int) (Result, int /* next after */)`: (1) items with a marker keep it (a marker naming an unknown or duplicate number is treated as unmarked); (2) unmarked items match, by text after NFC and whitespace folding, a known number whose marker vanished in this body; (3) the rest take `next`, `next+1`, …; writes `AC-{n}: ` after the bullet, drops `[ ]`/`[x]` (recording them in `Legacy`), and leaves every other byte of the body unchanged. `Reworded` lists kept numbers whose text changed.
- [ ] **Step 1: Write the failing tests** (table-driven): headings at levels 2 and 3 and any case; a heading in a code fence ignored; nested lists not criteria; a Given/When/Then item; checkbox items `[ ]` and `[x]` become legacy and lose the box; markers kept through reordering; a stripped-markers body rematched; a reworded item reported; a duplicate marker renumbered; bytes outside the section unchanged (compare prefix and suffix).
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/criteria/` → PASS.
- [ ] **Step 5:** Gate, commit `Criteria: parse and mark acceptance criteria (VAQ-2)`.

### Task 3: Criteria in core (saves, states, evidence, the warning)

**Files:**
- Create: `internal/core/criteria.go`, `internal/db/queries/criteria.sql`
- Modify: `internal/core/tickets.go` (`PatchTicket`/`UpdateTicket`/`CreateTicket`/`ImportTicket`: run `criteria.Mark` on a body change only, before `RequirementHash` is computed and stored, within the tx after the version check), `internal/core/board.go` or wherever `MoveTicket` lives (the warning), `internal/core/events.go` (`EvCriterionUpdated`, `EvCriterionRemoved`; payload docs), `internal/web/templates/activity.go` (`EventSentence`)
- Test: `internal/core/criteria_test.go`, `TestEveryOperationEmitsOneEventOrNothing`

**Interfaces:**
- Consumes: Task 2's `Mark`, `Known`, `Result`.
- Produces:
  - Description saves: `criteria` rows inserted (`Added`, state `open`, or `met` with one `note` evidence "Checked in the text before criteria were tracked." when `Legacy[n]` is true), `removed` for `Removed`, `open` for a `met` one in `Reworded`; `next_ac` advanced; `ticket.updated` payload `criteria: {added, removed, reset}` when any.
  - `type Evidence struct{ Kind, Target string }`.
  - `func (c *Core) MarkCriterion(ctx context.Context, by, ticketID string, n int, state string /* "met" | "open" | "waived" */, ev []Evidence, note string) (gen.Criterion, error)`: met needs ≥1 evidence (test target must be `path:Name`; "Give a test as path:Name."); open by the owner or the actor whose `met` it was; waived owner only with a note; refused on `removed`. One `criterion.updated` event `{number, from, to, evidence: [{kind, target}], note?}`.
  - `func OpenCriteria(ctx context.Context, q *gen.Queries, ticketID string) (open, total int, err error)`; status moves into in review or done of a task with open > 0 return `Warning` "{open} of {total} acceptance criteria aren't met." through the existing warning path (as `claimWarning`).
- [ ] **Step 1: Write the failing tests**: `TestSaveAddsMarkers` (body rewritten with `AC-1`, `AC-2`; rows; payload); `TestStatusSaveDoesNotMark` (a status patch on a body with unmarked items leaves it); `TestLegacyChecked` (`[x]` → met with the note); `TestRematchKeepsState` and `TestRewordResets` (Review Focus 2); `TestRaceStaleRefusedBeforeMarking` (Review Focus 3); `TestRequirementHashAfterMarking` (hash equals the hash of the marked body); `TestMarkCriterionRules` (each refusal, each permission); `TestMoveWarning` (review with 2 of 5 open → warning; none open → none; a requirement's move → no warning); `TestEveryOperationEmitsOneEventOrNothing` with `MarkCriterion`.
- [ ] **Step 2:** → FAIL. **Step 3:** implement; `make gen`. **Step 4:** `go test ./internal/core/...` → PASS.
- [ ] **Step 5:** Gate, commit `Core: criteria on save, their states and evidence (VAQ-2)`.

### Task 4: Checks, API, CLI and MCP

**Files:**
- Create: `internal/web/handlers/api_criteria.go` (with `func criteriaOf(ctx context.Context, d Deps, p gen.Project, t gen.Ticket) ([]api.Criterion, error)`, shared with Task 5), `internal/web/handlers/evidence_check.go`, `cmd/vaquero/cli_criteria.go`
- Modify: `internal/api/types.go` (`Criterion{Number, Text, Form, State, Evidence []EvidenceItem, UpdatedBy, UpdatedAt}`, `EvidenceItem{Kind, Target, By, At, Checked bool, Check string}`; `Ticket.Kind`/`.Criteria *CriteriaCount` with `omitempty` rules per Global Constraints), `internal/web/router.go` (`GET /tickets/{ticket}/criteria`, `POST /tickets/{ticket}/criteria/{n}`), `internal/web/handlers/api_read.go` (`?kind=`, default task; unknown a 422), `internal/web/handlers/api_bulk.go` (item action `criteria: [{number, state, evidence, note}]`, after update and before move), `internal/mcpserver/tools.go` (`update_criterion`; `kind` on `create_ticket` and `list_tickets`), `cmd/vaquero/cli.go`, `cmd/vaquero/cli_cmds.go` (`ticket new --kind`), `internal/help/articles/requirements.md` (new), `cli.md`, `api.md`, `mcp.md`, `markdown.md` (markers)
- Test: `internal/web/handlers/api_criteria_test.go`, `internal/web/handlers/evidence_check_test.go`, `cmd/vaquero/cli_criteria_test.go`, `internal/mcpserver/tools_test.go`

**Interfaces:**
- `func (d Deps) checkEvidence(ctx context.Context, p gen.Project, e gen.CriterionEvidence) (checked bool, msg string)`: test `path:Name` → file exists on main and contains `Name` (gitread `Show`); commit → ancestor of main (gitread, as `CommitsSince`'s ancestry check); review → the reference resolves (Part 2's reference check); note → never checked, no message. Messages: "Not on main yet", "Commit not merged", "Reference not found". Cache by (project, main head, evidence id), at most 512 entries. No repo path: unchecked, no message.
- CLI: `vaquero criteria TICKET` (table `AC  STATE  TEXT  EVIDENCE`), `vaquero criterion met TICKET AC-3 [--test PATH:NAME]... [--commit SHA]... [--review REF]... [--note TEXT]...`, `vaquero criterion reopen|waive TICKET AC-3 [--note TEXT]`, `vaquero requirements KEY` (`REQ  STATE  CRITERIA  TICKETS  TITLE`, "Ready to accept" in STATE when every criterion is met or waived).
- [ ] **Step 1: Write the failing tests**: API read with checks against a test repo (a test on main checked; one on a branch "Not on main yet", checked after a merge with no new save: Review Focus 4); POST refusals as 422 with the core messages; `?kind=requirement`; workflow-off goldens unchanged; bulk `criteria` action order and per-item refusal; CLI tables and flag errors (exit 2); MCP schema and calls; help tests name every new command, route and tool.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `API, CLI, MCP: criteria, evidence checks and requirements (VAQ-2)`.

### Task 5: The web (Requirements view, ticket page, cards)

**Files:**
- Create: `internal/web/handlers/requirements.go`, `internal/web/templates/requirements.templ`
- Modify: `internal/web/router.go` (`/p/{key}/requirements`), `internal/web/templates/board.templ` (`projectHeader`: Requirements tab after List; `cardItem`: "3/5" with an icon when a ticket has criteria), `internal/web/templates/tickets.templ` (criteria block after the description: each `AC-n`, its text, state, evidence with check messages, and, for the owner, Waive; for anyone, Mark met with an evidence form; Reopen per the rules; a requirement shows its implementing tickets and Accept for the owner when ready), `internal/web/handlers/tickets.go` (form posts to `MarkCriterion`; the board move answers the warning as a toast), `internal/web/icons/ui.json`, `internal/web/templates/tickets.templ` new-ticket form (Kind select: Task, Requirement)
- Test: `internal/web/handlers/requirements_test.go`, `internal/web/handlers/tickets_test.go`, `internal/e2e/requirements_test.go`

**Interfaces:**
- Consumes: Task 4's `criteriaOf`; Task 3's `MarkCriterion` and the warning.
- Requirements view: grouped draft (backlog category), active (todo, in progress, in review), accepted (done), retired (cancelled, folded); rows `VAQ-12 · title · 4/5 criteria met · 3 tickets · Ready to accept`; empty state "No requirements yet." with a "New requirement" link (`/p/{key}/new?kind=requirement`).
- [ ] **Step 1: Write the failing tests**: the tab and its groups; "Ready to accept" only when all met or waived; the ticket page's criteria block, its forms and their refusals shown as field errors; Accept visible to the owner only; the card's "3/5" only with criteria (workflow-off goldens unchanged); the move toast "2 of 5 acceptance criteria aren't met."; e2e at 375 px and 1280 px, both themes, no page wider than 375 px.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS (`make e2e`).
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (`internal/criteria`, core kinds and criteria, the tables, the routes and tab); commit `Web: requirements and acceptance criteria (VAQ-2)`.
