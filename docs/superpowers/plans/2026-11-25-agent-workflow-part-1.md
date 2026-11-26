# Agent workflow, part 1: the record layer — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Vaquero the records the agent workflow rests on:
- provenance on every event (VAQ-94);
- execution tiers, the tier map, and the per-project workflow switch;
- plans as versioned documents;
- decisions and approvals;
- readiness, gating `ready` when the workflow is on (VAQ-124).

**Architecture:**
- One migration (`0009_workflow.sql`) adds the tables and columns.
- `internal/core` gains the operations, each still one transaction and one event.
- Provenance rides on the request context into `emit`.
- Readiness is computed in Go from approvals (no SQL gate), so its reasons can be reported.
- REST and CLI routes come first, then MCP tools as API requests.
- Web changes are limited to the settings switch, the tier map, and the tier on the ticket form. The full views are part 5.

**Tech Stack:** As the repo: Go 1.27, modernc SQLite, goose, sqlc, templ/htmx. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-25-agent-workflow-design.md` (Part 1, plus the decisions it cites). Read it with this plan. Tickets VAQ-135, VAQ-124, VAQ-94.

**One correction to the spec:** the spec says a group approval "records one approval per ticket when answered, one event each". A core operation writes exactly one event (CLAUDE.md), so a group answer writes one `decision.answered` event whose payload carries `ticket_ids`, plus one approval row per ticket.

## Global Constraints

- CLAUDE.md hard invariants:
  - only core writes, with one event per change;
  - the API is additive;
  - MCP tools are API requests;
  - CSP: no inline script, no `style=`;
  - theme tokens only;
  - local-first, no telemetry.
- **Exactly one migration, `0009_workflow.sql`.** Released fixtures (`testdata/upgrade/v0.1.0`…`v0.1.5`) must still migrate (`TestFixturesMigrateToHead`). Make `make fixture VERSION=<next>` part of the release, not of this run.
- **Projects with the workflow off behave exactly as today:** `ready`, ticket answers, the board, and every existing test unchanged.
- Provenance values are stored as reported, never as identity, each cut to 80 characters with `units.Clip`. Never log or store a token.
- Copy follows copy-voice: sentence case, no product names in UI text. Exceptions: the tier map's harness keys (`claude`, `codex`) and model ids, which are data.
- Every new route, command and MCP tool gets its line in the api, cli or mcp article (the tests enforce this).
- Gate, each step separately by exit code: `make gen`, `make fmt`, `make vet`, `make lint`, `make test`; then `make ci`; `make e2e` when the web changes.
- Commits: `{Area}: {summary} (VAQ-N)` with the Co-Authored-By line, only the paths changed.

## Review Focus

1. **A project with the workflow off:** `GET /api/v1/ready`, a ticket answer, and the board are byte-for-byte unchanged (no `readiness` key, same ready set). Test in Task 5.
2. **An agent answering a question as itself:** refused. An agent relaying with no owner's words: refused. The owner directly: allowed. Test in Task 4.
3. **Two agents saving the same plan:** the second save, based on an old revision, gets a 409 carrying the current text. No revision is lost. Test in Task 3.
4. **An epic edited after its plan was approved:** every child that leans on the epic's approval becomes not ready, with the reason "requirement changed since approval". Test in Task 5.
5. **Provenance from the MCP bridge:** the harness name from `initialize` `clientInfo`, and the model from `--model` or `VAQUERO_MODEL`, reach the event of a later `tools/call`. Missing values leave the fields empty, never fail the call. Test in Task 1.

---

### Task 1: Migration 0009 and provenance on events (VAQ-94)

**Files:**
- Create: `internal/db/migrations/0009_workflow.sql`
- Modify: `internal/db/queries/events.sql` (`AppendEvent` takes `meta`; event reads return it), `internal/db/gen/*` (generated)
- Create: `internal/core/origin.go`; modify `internal/core/events.go` (`emit` writes meta)
- Modify: `internal/web/middleware/` (new `Origin` middleware), `internal/web/router.go` (mount it after `APIToken`, and on web routes)
- Modify: `cmd/vaquero/mcp.go` (remember `clientInfo`, add headers), `internal/client/` (the CLI sends its client header)
- Modify: `internal/web/handlers/api_read.go` (`api.Event` gains `meta`), `internal/api/types.go`, `internal/archive` (export and import carry `events.meta`)
- Test: `internal/core/origin_test.go`, `internal/web/middleware/origin_test.go`, `cmd/vaquero/mcp_test.go`, `internal/db/upgrade_test.go` (already covers new columns)

**The migration** (the whole schema of this part, so later tasks add only queries):
- `projects`: `workflow INTEGER NOT NULL DEFAULT 0`, `tier_map TEXT NOT NULL DEFAULT '' CHECK (tier_map = '' OR json_valid(tier_map))`.
- `tickets`:
  - `tier TEXT NOT NULL DEFAULT '' CHECK (tier IN ('', 'design', 'light', 'standard', 'heavy'))`;
  - `model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 80)`.
- `events`: `meta TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(meta))`.
- `plans`: `id` TEXT PK, `ticket_id` TEXT NOT NULL UNIQUE REFERENCES tickets, `revision` INTEGER NOT NULL, `body` TEXT NOT NULL CHECK length ≤ 200000, `created_at`, `updated_at`, `updated_by` REFERENCES actors.
- `plan_revisions`: `plan_id`, `revision`, `body`, `at`, `by`, PK (`plan_id`, `revision`).
- `decisions`:
  - `id` TEXT PK, `project_id` NOT NULL, `run_id` TEXT (no FK until part 3);
  - `kind` CHECK IN ('question', 'ruling', 'approval'), `subject` CHECK IN ('', 'plan', 'merge', 'release');
  - `question`; `options` (JSON list of `{label, tradeoff}`); `recommendation`; `why`;
  - `state` CHECK IN ('open', 'answered', 'withdrawn');
  - `answer`, `answer_note`, `answered_by`, `answered_at`, `relayed_by` (actor, nullable), `relayed_words`;
  - `asked_by`, `asked_at`.
- `decision_tickets`: `decision_id`, `ticket_id`, PK both.
- `approvals`: `id`, `ticket_id`, `decision_id`, `requirement_hash` TEXT, `plan_revision` INTEGER (0 = no plan), `pins` TEXT NOT NULL DEFAULT '[]' (JSON; repo pins arrive in part 2), `at`. Index on (`ticket_id`, `at`).
- Each is append-only SQL with a `-- +goose Down` of `SELECT 1;`, as in 0008.

**Interfaces:**
- Produces:
  - `type core.Origin struct { Surface, Client, Harness, Model, Run string }`;
  - `func core.WithOrigin(ctx context.Context, o Origin) context.Context`;
  - `func core.OriginFrom(ctx context.Context) Origin`;
  - `emit` stores `OriginFrom(ctx)` as `meta` (empty fields omitted, each clipped to 80);
  - `api.Event.Meta map[string]string` (`json:"meta,omitempty"`).
- Headers read by the middleware:
  - `Vaquero-Client`;
  - `Vaquero-Harness`;
  - `Vaquero-Model`;
  - `Vaquero-Run`;
  - Surface: `web` for page routes, `mcp` for `/mcp`, else `cli` when `Vaquero-Client` starts with `vaquero-cli/`, else `api`.
- The CLI sends `Vaquero-Client: vaquero-cli/{version.Version}`. The bridge sends `Vaquero-Client: vaquero-mcp/{version}`, `Vaquero-Harness: {clientInfo.name}/{clientInfo.version}`, and `Vaquero-Model` from `--model` or `VAQUERO_MODEL`.

- [ ] **Step 1: Write the failing tests**
  - `TestEmitRecordsOrigin`: a core op under `WithOrigin(ctx, Origin{Surface:"mcp", Harness:"claude-code/2.1.3", Model:"claude-opus-5-5"})` stores those in `meta`. A 100-character model is stored as 80 characters.
  - `TestOriginMiddlewareSurface`: page → web; `/api/v1` → api; with `Vaquero-Client: vaquero-cli/0.1.6` → cli; `/mcp` → mcp.
  - `TestMCPBridgeSendsHarnessAndModel`: after `initialize` with `clientInfo {name:"codex", version:"1.2"}` and `--model gpt-x`, a forwarded `tools/call` carries both headers.
  - `TestEventsAPIIncludesMeta`.
  - `TestExportImportKeepsMeta`.
- [ ] **Step 2: Run them:** `go test ./internal/core ./internal/web/middleware ./cmd/vaquero ./internal/archive -run 'Origin|HarnessAndModel|Meta'`. Expect them to fail to compile or to fail.
- [ ] **Step 3: Write the migration, the queries, `origin.go`, the middleware, the bridge and CLI headers, and the archive.** Then run `make gen`.
- [ ] **Step 4: Run the tests above plus `go test ./internal/db -run TestFixturesMigrateToHead`.** Expect PASS.
- [ ] **Step 5: Commit** `Events: record surface, client, harness, model and run on every change (VAQ-94)`

### Task 2: Tiers, the tier map and the workflow switch

**Files:**
- Modify:
  - `internal/core/patch.go` (the `tier` and `model` fields);
  - `internal/core/projects.go` (`UpdateProject` takes `Workflow *bool` and `TierMap *string`);
  - `internal/db/queries/{tickets,projects}.sql`.
- Create: `internal/core/tiers.go`.
- Modify:
  - `internal/api/types.go` (`Ticket.Tier`, `Ticket.Model`, `Project.Workflow`, `Project.TierMap`, all `omitempty` except `Project.Workflow`);
  - `internal/web/handlers/api_write.go` and `api_read.go`;
  - `cmd/vaquero/cli_cmds.go` (`ticket edit --tier T --model M`; `project edit --workflow on|off`);
  - `internal/mcpserver/tools.go` (`update_ticket` gains `tier` and `model`);
  - `internal/web/templates/{tickets,projects}.templ` (a tier select on the ticket form; a Workflow section in settings with the switch and the tier map as editable JSON in a textarea);
  - the help articles: api, cli, mcp, projects-and-tickets, and the new "Agent workflow" stub.
- Test: `internal/core/tiers_test.go`, `internal/web/handlers/api_tiers_test.go`, `cmd/vaquero/cli_tiers_test.go`.

**Interfaces:**
- Produces:
  - `core.Tiers = []string{"design", "light", "standard", "heavy"}`;
  - `type TierMap map[string]map[string]string` (harness → tier → model);
  - `func DefaultTierMap() TierMap`;
  - `func ParseTierMap(s string) (TierMap, error)`;
  - `func (m TierMap) Model(harness, tier string) string`.
- `DefaultTierMap`:
  - `claude`: design `claude-opus-5-5`, light `claude-haiku-5-5`, standard `claude-sonnet-5-5`, heavy `claude-opus-5-5`.
  - `codex`: model ids taken from Codex's current documentation (cite the page in the report; never from memory). If they can't be confirmed, ship `codex` with empty values and say so.
- `Project.TierMap` in the API is the effective map: the stored one, else the default.

- [ ] **Step 1: Write the failing tests:**
  - `TestPatchTicketTier`: setting `standard` changes the version and writes one `ticket.updated` event with `changes.tier`. `"huge"` gives an `*Invalid` on the `tier` field, with the message "Choose design, light, standard or heavy."
  - `TestTierMapDefaultAndParse`: the default has `claude` heavy → `claude-opus-5-5`; malformed JSON gives `*Invalid` on `tier_map`.
  - `TestProjectWorkflowSwitch`: one `project.updated` event.
  - `TestAPITicketTierRoundTrip`.
  - `TestCLITicketEditTier`.
  - `TestMCPUpdateTicketTier`.
- [ ] **Step 2: Run the tests and see them fail.**
- [ ] **Step 3: Implement.** The settings form posts the switch and the map. A refused map re-renders with its field message. When the workflow is on and the project lacks backlog-category statuses named "Needs decision" or "Needs design", the Workflow section offers a button, "Add the triage statuses". It calls `CreateStatusAt` for each missing one, one event each, placed at the end of the backlog category by VAQ-126's rule. Add the test `TestWorkflowOffersTriageStatuses`.
- [ ] **Step 4: Run the tests.** Expect PASS. Then run `make gen`.
- [ ] **Step 5: Commit** `Tickets: execution tier and model; projects: workflow switch and tier map (VAQ-135)`

### Task 3: Plans as versioned documents

**Files:**
- Create: `internal/core/plans.go`, `internal/db/queries/plans.sql`.
- Modify:
  - `internal/core/events.go` (`EvPlanCreated = "plan.created"`, `EvPlanRevised = "plan.revised"`, payload `{revision}` only);
  - `internal/web/templates/activity.go` (sentences: "wrote the plan for {ticket}", "revised the plan for {ticket} (revision N)"; icons);
  - `internal/core/core_test.go` (the `ops` list).
- Create: `internal/web/handlers/api_plans.go`.
- Modify:
  - `internal/web/router.go` (`GET` and `PUT /api/v1/tickets/{ticket}/plan`; `GET /api/v1/tickets/{ticket}/plan/revisions/{n}`);
  - `cmd/vaquero/cli_cmds.go` (`plan show TICKET [--revision N]`, `plan save TICKET FILE|- [--revision N]`);
  - `internal/mcpserver/tools.go` (`get_plan`, `save_plan`);
  - `internal/archive` (plans and plan_revisions export and import, and the load order);
  - help: api, cli, mcp.
- Test: `internal/core/plans_test.go`, `internal/web/handlers/api_plans_test.go`, `cmd/vaquero/cli_plans_test.go`.

**Interfaces:**
- Produces:
  - `func (c *Core) SavePlan(ctx, by, ticketID, body string, baseRevision int64) (gen.Plan, error)`. `baseRevision` 0 means "creating". A stale base returns `*PlanConflict{Current gen.Plan}`, which `api.FromCore` maps to 409 with `current`.
  - The API body `{"body", "revision"?}`. The answer is `{"ticket", "revision", "body", "updated_by", "updated_at"}`.
  - The plan's NFC-normalised body is what's stored and what readiness reads.

- [ ] **Step 1: Write the failing tests:**
  - `TestSavePlanCreatesAndRevises`: revision 1 then 2, the old body kept in `plan_revisions`, one event each.
  - `TestSavePlanStaleBaseConflicts`: two saves based on revision 1; the second gets `*PlanConflict` holding revision 2's body.
  - `TestSavePlanUnchangedWritesNothing`.
  - `TestAPIPlanRoundTripAndConflict409`.
  - `TestCLIPlanSaveFromStdin`.
  - `TestMCPPlanToolsMatchTheAPI`.
  - `TestExportImportKeepsPlans`.
  - The event payload carries no body (`/api/v1/events` has no plan text).
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.** Use the reader pool for GETs, and core for the save.
- [ ] **Step 4: Run the tests.** Expect PASS.
- [ ] **Step 5: Commit** `Plans: versioned plans on tickets and epics, through API, CLI and MCP (VAQ-135)`

### Task 4: Decisions and approvals

**Files:**
- Create:
  - `internal/core/decisions.go`, `internal/db/queries/decisions.sql`;
  - `internal/web/handlers/api_decisions.go`;
  - `cmd/vaquero/cli_decisions.go`.
- Modify:
  - `internal/core/events.go` (`EvDecisionAsked`, `EvDecisionRuled`, `EvDecisionAnswered`, `EvDecisionWithdrawn`; the payload carries `decision_id`, `kind`, `subject`, `ticket_ids`, and the answer label);
  - `activity.go` (the sentences);
  - `core_test.go` (the ops);
  - `router.go`;
  - `internal/mcpserver/tools.go` (`ask_decision`, `answer_decision`, `list_decisions`);
  - `internal/archive`;
  - help: api, cli, mcp, and "Agent workflow".
- Test: `internal/core/decisions_test.go`, `internal/web/handlers/api_decisions_test.go`, `cmd/vaquero/cli_decisions_test.go`.

**Interfaces:**
- **Inputs:**
  - `type DecisionInput struct { ProjectID string; TicketIDs []string; RunID string; Kind, Subject, Question string; Options []Option; Recommendation, Why string }`;
  - `type Option struct { Label, Tradeoff string }`;
  - `type Answer struct { Option string; Text string; Note string; RelayedBy string; Words string }`.
- **Operations:**
  - `func (c *Core) AskDecision(ctx, by string, in DecisionInput) (gen.Decision, error)`, for kinds question and approval.
  - `func (c *Core) RecordRuling(ctx, by string, in DecisionInput, a Answer) (gen.Decision, error)`, for the ruling kind, created already answered by `by`.
  - `func (c *Core) AnswerDecision(ctx, owner, decisionID string, a Answer) (gen.Decision, error)`.
  - `func (c *Core) WithdrawDecision(ctx, by, decisionID string) error`.
- **Answering rules, enforced in core:**
  - Only the owner (`IsOwner`) answers questions and approvals. With `RelayedBy` set (an agent's actor id), `Words` is required.
  - Overruling an answered ruling is `AnswerDecision` on it by the owner. It keeps the old answer in the event payload (`overruled`).
  - `Option` must be one of the labels, or `Text` must be given.
  - Ticket ids must belong to `ProjectID`.
- **Approval with subject `plan`:** when answered with an affirmative option, it writes one `approvals` row per ticket in `decision_tickets`. Each row has:
  - `requirement_hash = hex(sha256(NFC(title) + "\n" + NFC(description)))`;
  - `plan_revision` = the ticket's own plan revision, or, if it has none and its parent has one, the parent's; 0 if neither;
  - `pins` = `[]`.

  The affirmative option is the first option, or the one labelled "Approve" (the API documents this). Any other answer records no approval.
- **API:**
  - The handler maps a request with `Vaquero-Agent` to `RelayedBy = agent` and `owner = local owner`. It requires `words` then, and a request without `words` is a 422 on `words`: "Quote the owner's answer when relaying it."
  - Routes: `POST /api/v1/decisions`, `GET /api/v1/decisions?project=&state=&kind=&ticket=`, `GET /api/v1/decisions/{id}`, `POST /api/v1/decisions/{id}/answer`, `POST /api/v1/decisions/{id}/withdraw`.

- [ ] **Step 1: Write the failing tests:**
  - `TestAskAndAnswerQuestion`: one event each; `answered_by` is the owner, direct.
  - `TestAgentCannotAnswerAsItself`: core refuses `AnswerDecision` with `owner` = the agent's id.
  - `TestRelayedAnswerNeedsWords`.
  - `TestRelayedAnswerRecordsAgentAndWords`.
  - `TestRulingIsCreatedAnswered`.
  - `TestOverruleKeepsOldAnswer`.
  - `TestGroupPlanApprovalWritesRowsAndOneEvent`: 3 tickets give 3 approval rows and 1 event whose `ticket_ids` has 3 entries.
  - `TestApprovalPinsRequirementHashAndPlanRevision`.
  - `TestDecisionTicketsMustBeInProject`.
  - `TestAPIDecisionsRoundTrip`.
  - `TestAPIRelayedAnswerViaAgentHeader`.
  - `TestCLIDecisionAnswer`: `vaquero decision answer ID OPTION [--note T]` acts as the owner, direct. `--agent X --words "…"` relays.
  - `TestMCPDecisionToolsMatchTheAPI`.
  - `TestExportImportKeepsDecisionsAndApprovals`.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests.** Expect PASS.
- [ ] **Step 5: Commit** `Decisions: questions, rulings and approvals, answered directly or relayed (VAQ-135, VAQ-124)`

### Task 5: Readiness and the ready gate (VAQ-124)

**Files:**
- Create: `internal/readiness/readiness.go`, `internal/readiness/readiness_test.go`.
- Modify:
  - `internal/web/handlers/api_claims.go` (`ReadyTickets` filters when the project's workflow is on);
  - `api_read.go` (the ticket answer gains `readiness`);
  - `internal/api/types.go` (`Readiness struct { Ready bool; Reasons []string }`, `Ticket.Readiness *Readiness` with `omitempty`; `TicketSummary` the same);
  - `internal/mcpserver` (no new tool: `ready_tickets` and `get_ticket` carry it);
  - help: "Agent workflow" (the rule and the reasons), api.

**Interfaces:**
- Produces `func readiness.Check(ctx context.Context, q *gen.Queries, t gen.Ticket) (Readiness, error)`, which uses the reader. It's called only when the ticket's project has `workflow = 1`.
- The reasons, exact copy, in this order:
  - "Not a todo."
  - "No tier."
  - "Plan not approved."
  - "Requirement changed since approval."
  - "Plan changed since approval."
  - "Epic's requirement changed since approval."
  - "Epic's plan changed since approval."
- The rule:
  - **Status:** the ticket's status category is todo.
  - **Tier:** it has a tier.
  - **Approval:** its latest approval matches its current requirement hash and plan revision; or, with no approval of its own and no plan of its own, its parent's latest approval matches the parent's current hash and plan revision.
  - **Existing conditions:** they still apply, from the board query: unassigned, unclaimed, no open blocker or sub-ticket.
- A `design`-tier ticket can be ready. `/vaquero:run` (part 4) skips it.

- [ ] **Step 1: Write the failing tests:**
  - `TestReadyWithApprovedPlanAndTier`.
  - `TestNoTierNotReady`.
  - `TestEditedRequirementNotReady`: change the description after the approval; the reason is "Requirement changed since approval."
  - `TestRevisedPlanNotReady`.
  - `TestChildInheritsEpicApproval`.
  - `TestEditedEpicMakesChildrenNotReady`: this is Review Focus 4.
  - `TestChildOwnPlanTakesPrecedence`.
  - `TestWorkflowOffUnchanged`: this is Review Focus 1. For a project with the workflow off, compare the JSON of `GET /api/v1/ready` and of a ticket answer with a golden taken before the change, and check there is no `readiness` key.
  - `TestReadyAPIFiltersWhenWorkflowOn`.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement** the check, and the filter in `ReadyTickets`. Check only the candidates the board already marks ready, so the cost stays bounded.
- [ ] **Step 4: Run the tests.** Expect PASS. Then run the whole suite, `make ci`, and `make e2e` (the settings form changed in Task 2).
- [ ] **Step 5: Commit** `Readiness: approved plan, unchanged requirement and a tier gate ready when the workflow is on (VAQ-124)`
