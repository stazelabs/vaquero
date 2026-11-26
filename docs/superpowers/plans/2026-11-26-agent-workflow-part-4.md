# Agent workflow, part 4: the workflow prompts — implementation plan (VAQ-138)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the workflow (triage, plan, run, audit) inside Vaquero as MCP prompts and as `vaquero workflow NAME`, written for any harness, so the whole cycle works from any MCP-capable coding agent.

**Architecture:** A new `internal/workflow` package embeds the prompt texts (Markdown with front matter, beside `internal/help`), lists them and renders one with its arguments. The MCP server registers them as prompts; the CLI prints them with no server. Tests hold the texts to the tools and commands that exist and to harness neutrality.

**Tech Stack:** Go 1.27, `embed`, `github.com/modelcontextprotocol/go-sdk` (already used; its prompt support), goldmark (front matter parsing as `internal/help` does).

**Spec:** `docs/superpowers/specs/2026-11-25-agent-workflow-design.md`: Decisions 6, 7, 8, 9, 10 and 11, and Part 4. Ticket: VAQ-138. Built after Part 3 (VAQ-137): the run prompt drives runs.

## Rulings (controller, 2026-11-26; the owner can overrule)

- **P4-R1: four prompts, fixed names:** `triage`, `plan`, `run`, `audit` (MCP names `triage` etc.; harnesses show them as `/vaquero:triage`). Arguments: `triage` (`project`), `plan` (`ticket`), `run` (`project`, `tickets?` comma-separated keys, `run?` name), `audit` (`project`, `kind`: qa, hygiene, continuity or docs).
- **P4-R2: harness neutrality is written, not templated.** Each prompt has one main path that needs only Vaquero's MCP tools (or the CLI), the shell and the repo. Harness-specific help lives in sections headed "If you can delegate to subagents" or "If you can ask the owner in a question box", never in required steps. A test fails on a harness tool name outside those sections.
- **P4-R3: audit packs.** The audit prompt reads `.vaquero/audits/{kind}.md`, else `.claude/agent-workflow/audits/{kind}.md` (the packs the owner's projects already have). With neither it stops: "No audit pack for {kind}: add .vaquero/audits/{kind}.md." The pack's headings are the existing ones (Standards, Pre-pass, Dimensions, Known non-findings), described in the prompt. Recording the audit (`record_audit`) is added to the text by VAQ-105 when that tool exists.
- **P4-R4: the texts cite only what exists.** Every MCP tool, CLI command and API route a prompt names must exist (tested against `mcpserver.ToolNames()`, the CLI's command table and the router), so a prompt never tells an agent to call something missing.
- **P4-R5: provenance is part of every prompt.** Each tells the agent to send `Vaquero-Harness`, `Vaquero-Model` and `Vaquero-Run` (or `vaquero mcp --model` and the run name), and to work tickets at their tier's model from the project's tier map.

## Global Constraints

- Gate before every commit touching Go: `make gen && make fmt && make vet && make lint && make test`.
- Commit messages `{Area}: {summary} (VAQ-138)`, ending with the `Co-Authored-By` line.
- Copy rules apply to prompt texts: sentence case, plain words, no competing products, no model provider named in required steps (the tier map names models; prompts say "the model the tier map gives").
- Prompts never tell an agent to answer a question or approval itself, merge or release without the approval, or edit an accepted architecture decision's file; each prompt states the owner-only rules it touches, in the words of the design (Decision 7).
- MCP: `prompts/list` and `prompts/get` only; the tool list doesn't change; `prompts/get` calls are logged in `mcp_calls` with tool `prompt:{name}`.
- `vaquero workflow` needs no server and no network.

## Review Focus

1. **`prompts/get` with a missing required argument or an unknown kind** (`audit` with `kind=perf`): a JSON-RPC error naming the argument and the allowed values, never a prompt with a blank in it. Test in Task 1.
2. **A harness without MCP prompt support:** `vaquero workflow run --project VAQ` prints the same text, byte for byte, as `prompts/get`. Test in Task 4.
3. **A prompt naming a tool that a later change renames:** the existence test fails in that change, not in production. Test in Task 1.
4. **An unattended run that hits a question:** the run prompt says to ask it as a decision (`ask_decision`), carry on with unblocked work, and never stop or guess. Checked by the content test in Task 3.
5. **An argument value with Markdown or newlines in it** (a ticket key typed with extra text): rendered as plain text on one line, cut at 80 characters. Test in Task 1.

---

### Task 1: The prompt package (`internal/workflow`)

**Files:**
- Create: `internal/workflow/workflow.go`, `internal/workflow/prompts/` (four stub files with front matter and one heading each, filled by Tasks 2 and 3), `internal/workflow/workflow_test.go`

**Interfaces:**
- Produces:
  - `type Arg struct{ Name, Description string; Required bool; Values []string /* allowed, empty for any */ }`; `type Prompt struct{ Name, Title, Description string; Args []Arg; Body string /* Markdown with {{name}} placeholders */ }`.
  - `func List() []Prompt` (in P4-R1 order); `func Get(name string) (Prompt, bool)`.
  - `func Render(p Prompt, args map[string]string) (string, error)`: replaces `{{name}}` with the value (control characters and newlines turned to spaces, trimmed, cut at 80 with `units.Clip`; Review Focus 5); a missing required or disallowed value is an `*ArgError{Name, Allowed}` (Review Focus 1); an optional one missing becomes "(none given)"; any `{{…}}` left unreplaced is an error.
  - Front matter: `name`, `title`, `description`, `args` (each `name`, `description`, `required`, `values`).
- [ ] **Step 1: Write the failing tests**: `TestListOrder`; `TestRenderArgs` (missing, disallowed, newline, long); `TestNoLeftoverPlaceholders`; `TestPromptsNameOnlyWhatExists` (P4-R4: extracts backticked tool names, `vaquero …` commands and `/api/v1/…` routes from every body and checks them against `mcpserver.ToolNames()`, the CLI's command table (export it from `cmd/vaquero` as a test helper list, or keep a copy in `internal/workflow` that `cmd/vaquero`'s tests hold equal), and the router's routes; Review Focus 3); `TestHarnessNeutral` (P4-R2: a deny-list of harness tool names (`Task`, `TodoWrite`, `AskUserQuestion`, `Skill`, `WebFetch`, `apply_patch`) appears only under the two allowed headings).
- [ ] **Step 2:** `go test ./internal/workflow/` → FAIL.
- [ ] **Step 3:** Implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Workflow: the prompt package (VAQ-138)`.

### Task 2: The triage and plan prompts

**Files:**
- Modify: `internal/workflow/prompts/triage.md`, `internal/workflow/prompts/plan.md`
- Test: `internal/workflow/content_test.go`

**Interfaces:**
- Sources to distil (read, don't copy): the spec's Decisions 7, 8, 11 and the triage-tracks practice (statuses Backlog, Needs decision, Needs design, Todo; `vaquero-triage-tracks` in the owner's memory is summarised in the spec's Decision 11), the stazelabs `plan` skill (`~/.claude/plugins/cache/agent-workflow/stazelabs/0.2.2/skills/plan/SKILL.md`), superpowers brainstorming and writing-plans.
- **triage** must cover, as numbered sections: the seven areas in Decision 11's order; what is a decision (anything with consequences: `ask_decision`, a recommendation first, why it matters) and what is a ruling the agent records itself (re-tracking, linking, draft acceptance criteria, tiers with a one-line reason); proposing an audit when one is due; its output (decisions and rulings in Vaquero, a proposed next run as a list of ready tickets); never answering its own questions.
- **plan** must cover: reading the ticket, its references and rulings; drafting acceptance criteria when missing; writing the plan with `save_plan` (base revision, merge on a stale save); asking plan approval with `ask_decision` (subject plan, Approve and Request changes); design-tier tickets are this prompt's, never the run prompt's; stopping when approval is asked.
- [ ] **Step 1: Write the failing tests**: `TestTriageCoversAreas` (each of the seven area names as a heading, in order; "decision" and "ruling" both defined); `TestPlanSteps` (mentions `save_plan`, `ask_decision`, "Acceptance criteria", "design"); both bodies pass Task 1's existence and neutrality tests.
- [ ] **Step 2:** → FAIL. **Step 3:** write the texts. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Workflow: the triage and plan prompts (VAQ-138)`.

### Task 3: The run and audit prompts

**Files:**
- Modify: `internal/workflow/prompts/run.md`, `internal/workflow/prompts/audit.md`
- Test: `internal/workflow/content_test.go`

**Interfaces:**
- Sources: the spec's Decisions 9 and 10, Part 3's runs and gates (`docs/superpowers/plans/2026-11-26-agent-workflow-part-3.md` for the CLI and MCP names), the stazelabs `ticket`, `hygiene`, `continuity`, `qa` and `update-docs` skills, superpowers subagent-driven-development and requesting-code-review.
- **run** must cover: creating or resuming the run (`update_run`), taking only ready, approved, non-design tickets at their tiers; batches each with an implementer and a reviewer and fix rounds; the gate (the project's own, read from its repo, for example `make ci`); booking through `bulk_update` (In review, a comment, release the claim, `view: summary`); parking questions as decisions and continuing with unblocked work (Review Focus 4); a final review; asking the merge approval, merging only after it, then `vaquero run merge`; asking the release approval with the version and runs, tagging only after it; provenance headers (P4-R5). The "If you can delegate to subagents" section says how to fan batches out at each ticket's tier; the main path works them one by one in the session.
- **audit** must cover: P4-R3's pack lookup and stop; the pack's sections; findings filed as untriaged tickets (Backlog) tagged with the source (`audit-{kind}`), grouped, each with evidence (file and line) and a proposed tier; never fixing; continuity checking what's built against the latest approved requirement (the approval pins).
- [ ] **Step 1: Write the failing tests**: `TestRunGates` (mentions the merge approval before merging, the release approval before tagging, `ask_decision` for questions, `bulk_update`); `TestRunLeavesOutDesignTier`; `TestAuditPackLookup` (both paths and the stop message); bodies pass Task 1's tests.
- [ ] **Step 2:** → FAIL. **Step 3:** write the texts. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Workflow: the run and audit prompts (VAQ-138)`.

### Task 4: MCP prompts and `vaquero workflow`

**Files:**
- Modify: `internal/mcpserver/mcpserver.go` (capabilities gain `Prompts`; `addPrompts(s)` from `workflow.List()`, each `prompts/get` rendering with `workflow.Render`, an `*ArgError` → JSON-RPC invalid params naming the argument and allowed values), `internal/mcpserver/log.go` (log `prompts/get` as `prompt:{name}`), `internal/mcpserver/instructions.go` or wherever `Instructions` lives (one line: "Workflow prompts: triage, plan, run and audit."), `cmd/vaquero/cli.go` (`workflow` command: no server), `internal/help/articles/agent-workflow.md` ("The workflow prompts": each prompt, its arguments, how to start it in a harness with and without MCP prompts), `cli.md`, `mcp.md`
- Create: `cmd/vaquero/cli_workflow.go`
- Test: `internal/mcpserver/prompts_test.go`, `cmd/vaquero/cli_workflow_test.go`

**Interfaces:**
- `vaquero workflow` lists the prompts (name, title, arguments); `vaquero workflow NAME [--project KEY] [--ticket TICKET] [--tickets K,K] [--run NAME] [--kind KIND]` prints the rendered text to stdout; an `*ArgError` is exit 2 with "Give --kind: qa, hygiene, continuity or docs." style messages.
- [ ] **Step 1: Write the failing tests**: `prompts/list` names the four with their arguments; `prompts/get` renders and refuses (Review Focus 1); the call is logged; `tools/list` unchanged; `vaquero workflow run --project VAQ` equals the MCP text byte for byte (Review Focus 2); no server is started (the test runs with no listener); help tests name the command.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (`internal/workflow`, MCP prompts, the command; P4 rulings under the agent workflow invariants); commit `MCP, CLI: the workflow prompts (VAQ-138)`.
