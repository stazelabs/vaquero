# Agent workflow: design

Milestone: **Agent workflow** (VAQ). Tickets: VAQ-107 (the umbrella), VAQ-124 (readiness), VAQ-94 (provenance), a read-only slice of VAQ-1 (git), and VAQ-77, which this design supersedes. Decided with the owner on 2026-11-25.

## Intent

Vaquero should run the loop its owner already runs by hand. Today that loop lives in chat sessions and in files Vaquero never sees (plans, run ledgers, review notes):

1. **Capture.** Requirements, small or large, become tickets, or epics with child tickets. The owner's own reviews and QA feed defects and features back in the same way.
2. **Triage.** Every ticket gets a track and an adequate plan. Only then is it ready.
3. **Execute.** Coding agents work the ready tickets in the right order, to completion, in reviewed runs.
4. **Button up.** QA, hygiene, continuity and docs audits run periodically. Their findings re-enter as tickets.

All along, the design artifacts behind the work are visible in Vaquero and are used for cross-checks. For example, continuity checks what is built against the most recent approved requirement.

**Why:** this is the core of the local-first software factory. Vaquero, git and a coding agent should be enough to build most software. The owner decides, the agents do the work, and Vaquero is where the work is defined, decided, recorded and seen.

## Goals

- The full cycle (capture, triage, plan, run, audit) works from any MCP-capable coding agent, on the owner's existing subscription (Claude Code, Codex, others).
- Readiness is real. A ticket is ready only when its plan is approved, the requirement and files that approval rests on haven't changed since, and it has an execution tier.
- Decisions are Vaquero records. They can be answered in chat or in Vaquero, and agents read them as rulings. Unattended runs park questions instead of stopping or guessing.
- Runs, approvals and provenance (which harness and model did what) are visible in Vaquero.
- Drift between requirements, design files and code is reported, never silent.

## Non-goals

- Vaquero calling model APIs itself. The door stays open: Phase 7 can add an API-backed runner that follows the same prompts and writes the same records.
- Writing to git: branches, commits, pull requests and GitHub all stay in Phase 5. This milestone only reads.
- Requirements as repo files with front matter, and Vaquero scanning the repo (option C below). Left for later, if needed.
- Hosted mode, sign-in and users (Phase 4).
- Replacing the stazelabs skills for Linear projects. They stay as they are.

## Anti-goals (it fails if any of these happen, even if it works)

- The workflow works well only in Claude Code.
- A person working by hand, in a project without the workflow turned on, meets gates or changed behaviour.
- Drift between a ticket, its files and the code goes unnoticed.
- An agent answers its own question, or merges or releases without a recorded approval.
- Vaquero stores or asks for model API keys.

## Constraints

These come from CLAUDE.md and must hold throughout:

- Only `internal/core` writes the tracker tables, and each change appends exactly one event.
- The API is additive.
- MCP tools are API requests, never their own reads or writes.
- Migrations are append-only. A run adds at most one, and released fixtures are never edited.
- CSP: no inline script, no `style=`. Templates use theme tokens.
- Local-first: no network services unless configured, and no telemetry.
- No new Go dependency, apart from the test-only chromedp exception.
- Local mode binds to loopback only.
- Each run's gate, plus `make e2e` for UI.
- A new command, route or shortcut needs its help-article line.

## Decisions made with the owner

1. **Requirements live in tickets.** Vaquero owns the requirement text. A ticket's description is the requirement. An epic's description is the large requirement, and its children are the parts.
   - **Writing convention:** a short statement of the need, then an "Acceptance criteria" Markdown task list. Triage uses it to tell thin from ready, and it keeps a later move to files mechanical.
2. **Product artifacts are repo files; plans live in Vaquero.**
   - Designs and specs, the long-lived product knowledge, stay in the repo, the source of truth next to the code. Tickets point at them with typed references (Part 2).
   - Plans are about doing the work, so they live in Vaquero as versioned documents on a ticket or epic. Agents can draft and revise them concurrently through the API without touching any worktree or committing to main.
   - Existing plan files in the repo stay referenceable.
3. **Approval is a record.** Approving a plan pins:
   - the requirement as approved;
   - the plan's revision;
   - the commit of each referenced repo file.

   Continuity and triage use these pins to report drift.
4. **Read-only git comes forward.** A project knows its repo path, and Vaquero reads it with the system `git`, so references can be checked, shown, followed across renames, and compared since approval. This is the first slice of VAQ-1.
5. **Subscriptions, not API keys.** Work runs in coding-agent harnesses on the user's own subscription. Vaquero is harness-agnostic, and API use later stays possible.
6. **The workflow ships with Vaquero as MCP prompts:** `/vaquero:triage`, `/vaquero:plan`, `/vaquero:run`, `/vaquero:audit`. `vaquero workflow <name>` prints the same text for harnesses without MCP. Prompt text adapts: "if you can delegate to subagents, do X; otherwise Y". VAQ-77's adapter is dropped.
7. **Decisions live in both places.** They are Vaquero objects, answerable in chat or in Vaquero's decision queue. There are three kinds:
   - questions for the owner;
   - rulings an agent made, marked as its call and open to overruling;
   - approvals.

   Merges and releases always need an approval. An agent never answers its own question. Overruling a ruling an agent has already acted on files a follow-up ticket; it doesn't revert silently.

   **Relayed answers (owner, 2026-11-25):**
   - In local mode every agent holds the owner's token, so the rules rest on trust (as VAQ-60 notes).
   - When an agent records an answer the owner gave in chat, it is stored as *relayed* by that agent, with the owner's words. Answers given in the web queue or the CLI, as the owner without an agent, are stored as *direct*.
   - The gates accept both, and history shows which was which.
   - An agent can never answer as itself.
   - Hosted mode, with real sign-in, may later require direct answers for merge and release.
8. **Execution tier.** Each ticket has a harness-neutral tier (`design`, `light`, `standard`, `heavy`) and an optional exact model.
   - A per-project map turns tiers into models for each harness.
   - The defaults cover Claude (light `claude-haiku-5-5`, standard `claude-sonnet-5-5`, heavy `claude-opus-5-5`) and Codex. The builder takes Codex's names from Codex's current documentation, not from memory.
   - Triage sets the tier as a ruling, with a one-line reason.
   - Reviewers default to heavy.
9. **Runs by default; a single ticket is a run of one.** A run is a set of ready tickets on one branch, worked in reviewed batches, then a final review, then merge and release approvals.
10. **Audits:** `/vaquero:audit qa|hygiene|continuity|docs`.
    - Each project's audit pack is a file in its repo.
    - Findings are filed as untriaged tickets, tagged with their source.
    - Triage proposes an audit when one is due (after N merged runs or a time span) as a decision, never as an automatic run.
11. **Triage covers seven areas:**
    1. placing tickets on tracks;
    2. readiness, including the tier;
    3. duplicates and overlaps;
    4. tickets overtaken by events;
    5. order and what's next;
    6. drift;
    7. housekeeping (stale claims, merged work left In review, tags to merge, decisions waiting).

    Triage proposes anything with consequences as a decision. It records re-tracking, linking, draft acceptance criteria and tiers itself, as rulings.
12. **Build order:** one milestone, built in this order:
    1. the record layer;
    2. read-only git and references;
    3. runs;
    4. the prompts;
    5. the views.

## Approaches considered

1. **First-class objects, with Vaquero guarding a few gates.** Chosen.
   - New records: decisions, runs, artifact references, tiers, and provenance on events.
   - Vaquero enforces only the gates that matter whatever harness drives it: readiness for `ready` when the workflow is on, and an approval before a run can be marked merged or released.
   - Everything else is up to the prompts, which are cheap to revise.
2. **Overload what exists:** decisions as comments, runs as epics, references as links, tiers as tags. Rejected: weak validation, awkward queries, and drift moved inside Vaquero.
3. **First-class objects with no gates.** Rejected: a harness that skips a step, or a person working by hand, meets no guard.

Also weighed and set aside for now:
- **Requirements as repo files** (option C): one history with the code, but heavy for small asks. The hooks below keep it possible.
- **Vaquero dispatching agents itself:** a "Start run" launcher can come later, on top of the run records.

## Design

### The workflow switch

A project setting, `workflow` (off by default). Projects with it off behave exactly as today. When it is on:

- `ready` (REST, CLI, MCP) also requires the readiness rule below. This is opt-in, so the API stays additive.
- The project should have backlog-category statuses "Needs decision" and "Needs design". Settings offers to create them, placed by VAQ-126's rule.
- Settings shows the repo path and the tier map.

VAQ already uses these statuses, so it turns the workflow on.

**Turning it on (owner, 2026-11-25).** No existing ticket has an approved plan or a tier, so `ready` empties at once. That is intended.
- Turning the switch on is followed by a triage pass.
- The first `/vaquero:triage` proposes plan approvals and tiers for the current Todo tickets, in groups the owner approves in one go: one approval decision listing the tickets, which records an approval per ticket when answered.
- The queue refills in that session.

### Part 1: the record layer

**Decisions.** New table, written through core:
- Fields: project, ticket (nullable, for project-level decisions such as "an audit is due"), run (nullable), kind (`question`, `ruling`, `approval`), subject for approvals (`plan`, `merge`, `release`), question text, options (a list of label and trade-off), recommendation, why it matters, state (`open`, `answered`, `withdrawn`), the answer (the chosen option, or free text, plus a note), who answered and when, and who asked and when.
- A ruling is created already answered (by the agent, marked as its call).
- Overruling a ruling records a new answer on it, keeps the old one in history, and the prompts then file a follow-up ticket.
- Events: `decision.asked`, `decision.ruled`, `decision.answered`, `decision.withdrawn`.
- Core refuses an answer by the actor who asked an open question. That is the "never answers its own question" rule. The owner may answer anything.
- Each answer records whether it is *direct* (the owner, no agent) or *relayed* (an agent recording the owner's chat answer, with the owner's words quoted). An agent's own answer to a question is refused.
- A group approval (one decision listing several tickets) records one approval per ticket when it is answered, one event each.

**Plans.** A new table of documents, kind `plan`, attached to a ticket or epic, with every revision kept.
- Core operations `CreatePlan` and `RevisePlan`, one event each (`plan.created`, `plan.revised`).
- Concurrent saves use the same version check as tickets: a stale save is a conflict carrying the current text.
- The ticket page shows the plan, with "Show changes" between revisions (the VAQ-21 line diff).
- REST, CLI and MCP read and write it.
- Events in the API carry a revision number and a flag, not the full text (as for descriptions since run K).
- Included in export and import.

**Readiness (VAQ-124).**
- An approval decision with subject `plan` on a ticket pins:
  - the requirement as approved, as a hash of the ticket's title and description. The ticket's `version` won't do: it rises on moves and claims too;
  - the plan revision (or, for an older plan, a repo file reference);
  - the commit of each artifact reference (Part 2). A referenced repo file must be on the main branch when the plan is approved.
- **Epics:**
  - An epic's plan approval covers its children.
  - Each child still needs its own tier, and may carry its own plan and approval, which then take precedence.
  - Editing the epic's requirement or plan makes its children unready until they are re-approved.
- **Pin comparison:** for repo files, the file on main against the pinned commit (following renames). For the plan, the current revision against the pinned one.
  - When a run changes a file other tickets rely on, those tickets become unready, and triage flags them for re-approval.
- **The readiness rule:**
  - the ticket is a todo;
  - it has a current plan approval, meaning the requirement hash and every pinned file's content still match;
  - it has a tier.

  The existing ready conditions also still apply: unassigned, unclaimed, no open blocker or sub-ticket.
- If a pinned file can't be checked (no repo path, or git absent), the rule fails with a reason, never silently passes.
- The answer to `ready`, and each ticket, carries the reason a ticket is not ready ("plan not approved", "requirement changed since approval", "file docs/… changed since approval", "no tier"). This is additive.

**Execution tier.**
- Ticket fields `tier` (`design`, `light`, `standard`, `heavy`, or unset) and `model` (optional free text, length-bounded).
- They are set through the normal ticket updates (REST PATCH, CLI flags, MCP `update_ticket`), and shown on the ticket page and card.
- A per-project tier map (setting): harness → tier → model, with shipped defaults for `claude` and `codex`. Editable in settings and through the API.
- `design`-tier tickets are spec and plan work. `/vaquero:plan` works them, never `/vaquero:run`. The map gives `design` the heavy model. `ready` includes them, marked with their tier, and the run prompt leaves them out.

**Provenance (VAQ-94).** Every new event records, as reported and never as verified:
- the surface (web, CLI, API, MCP);
- the `vaquero` client version (CLI and MCP bridge);
- the harness name and version (MCP `clientInfo`, or the `Vaquero-Harness` header);
- the model (`Vaquero-Model` header; `vaquero mcp` takes `--model` or `VAQUERO_MODEL`);
- the run (`Vaquero-Run` header, or the run the caller holds tickets in).

Each value is bounded to 80 characters. It is stored with the event (columns or one `meta` object; the builder's call) and shown in ticket history (detail line), on the activity page and on run pages. It is included in export and import. Older events have none.

### Part 2: read-only git and artifact references

**Repo path.**
- A project setting: an absolute local path to a git work tree, validated on save (`git -C path rev-parse --is-inside-work-tree`).
- Vaquero runs the system `git`, with fixed arguments, `-C path`, a timeout, `GIT_TERMINAL_PROMPT=0`, no network commands and capped output.
- If `git` is missing, the feature says so and stays off.

**The operations:**
- show a file at a revision;
- list a path's commits since a revision on the main branch (following renames);
- find commits whose messages mention a ticket key;
- list a branch's commits relative to main;
- check whether a branch is merged into main;
- check whether a tag exists.

The main branch name is a project setting, default `main`.

**Inputs are checked before reaching git:**
- paths are relative and clean, with no `..`, no leading `/` and no leading `-`;
- revisions match a commit-hash or ref-name pattern;
- nothing else is passed through.

Repo content is untrusted. Markdown renders through `internal/markdown`, other text as escaped text.

**Artifact references.**
- New table: ticket, kind (`requirement`, `design`, `plan`, `spec`, `other`), target (a repo path or a URL) and an optional label. Core operations with events: `ref.added`, `ref.removed`.
- New plans live in Vaquero (Part 1). The `plan` kind is for older plan files in the repo.
- The ticket page lists references. A repo path opens the file view: rendered at main, or at a pinned commit, with "changed since approval" and a diff when it has.
- Renames are followed by git. A reference whose file is gone is flagged.

**Commits on tickets.** Commits on main whose messages mention a ticket key (the repo's commit format, `(VAQ-N)`) are listed on the ticket page, read live. They are not stored.

### Part 3: runs

**Run records.**
- New tables: runs (project, a short name, state, branch, the plan it follows (a Vaquero plan, usually on an epic or a run-level ticket), created by and when) and run tickets (run, ticket, batch number, order).
- States: `planned`, `running`, `reviewing`, `awaiting merge`, `merged`, `released`, `abandoned`.
- Core operations with events (`run.created`, `run.updated`, `run.state_changed`, `run.ticket_added`, `run.ticket_removed`).

**The gates.**
- A run moves to `merged` only with an answered `merge` approval on the run, and only when read-only git confirms the branch is merged into main.
- **A release is a project-level approval** (subject `release`). It names the version tag and the merged runs it covers.
  - One release can cover several runs, and a run may stop at `merged`.
  - When the approval is answered and the tag exists, each covered run moves to `released`.
  - `released` always follows `merged`.
- Vaquero never merges or tags. The agent does, and Vaquero checks.

**Tickets in a run.**
- A ticket in a running run shows it on its card and page.
- Booking (move to In review, comment and release the claim) stays in the prompts, through the bulk endpoint.
- A single-ticket run is the same object.

**Run pages.** Tickets by batch, branch commits, decisions and approvals, and provenance (planned tier against actual model, per ticket).

### Part 4: the workflow prompts

**Delivery.**
- `internal/mcpserver` adds MCP `prompts` (`prompts/list`, `prompts/get`) next to tools.
- The prompt text is embedded Markdown in the binary (its own package, beside `internal/help`), with arguments (project, tickets, run, audit kind).
- `vaquero workflow [name]` prints them.
- A help article, "Agent workflow", explains the cycle for people.

**The four prompts:**
- **`triage`:** the seven areas in Decision 11. Its output is decisions and rulings recorded in Vaquero, plus a proposed next run.
- **`plan`:** turns a ticket or epic into a plan document in Vaquero, with rulings. It references any repo designs or specs the plan rests on, asks for plan approval as a decision, and drafts acceptance criteria when they are missing. It works `design`-tier tickets.
- **`run`:** takes approved, ready tickets and works them as today's runs did:
  - batches, each with an implementer and a reviewer, with fix rounds;
  - a final review, then merge and release approvals;
  - booking through bulk, and provenance headers on every call.

  It delegates to subagents at each ticket's tier when the harness can, and works sequentially in one session when it can't. Questions are parked as decisions while the run continues with work that isn't blocked.
- **`audit`:** runs the repo's audit pack for the kind and files findings as untriaged tickets with a source tag. Continuity uses the approval pins.

**Sources.** The prompt content is distilled from today's practice (runs J and K, the subagent-driven-development and brainstorming skills) and the stazelabs skills (plan, ticket, hygiene, continuity, qa, update-docs). It is written for any harness: no Claude-only tool names in required steps, and capability-conditional sections.

**MCP tools.** New tools for decisions, runs, references and the git reads, each one API request. Keep the set small: prefer extending `bulk_update` and the existing tools over many new ones. The tool list and its documentation grow by exactly what is added.

### Part 5: views

- **Decision queue** (`/decisions`, plus a header count): open questions first, each with its options and recommendation, answered in place. Rulings are listed with an overrule action. The queue is filterable by project, run and kind.
- **Ticket page:** decisions, references, readiness (and why not), tier, the commits that mention the ticket, and provenance in history.
- **Run pages** (Part 3), and a project's run list.
- **File view** (Part 2).
- **Settings:** the workflow switch, the repo path, the main branch and the tier map.

All views stay live through `/events/stream`, are checked with `make e2e` at the desk and phone widths, and are documented in the help.

### How this maps to tickets

- VAQ-107 becomes the epic. Children are one ticket per part, plus the tickets below.
- VAQ-124 → Part 1 (readiness, decisions).
- VAQ-94 → Part 1 (provenance).
- A new ticket for the read-only git slice of VAQ-1 → Part 2.
- VAQ-77 is closed as superseded by Part 4.

## Calls made after the builder check (controller, 2026-11-25; the owner can overrule)

- An epic's plan approval covers its children. Each child needs its own tier, and its own plan, if it has one, takes precedence. Editing the epic makes its children unready until re-approved.
- Pins compare against main. A referenced repo file must be on main when the plan is approved. Plans in Vaquero pin by revision, so planning needs no commit.
- `design`-tier tickets are worked by `/vaquero:plan` and mapped to the heavy model. Runs leave them out.
- A release is a project-level approval that names the version and the runs it covers. One release can cover several runs, a run can stop at merged, and released follows merged once the tag exists.

## Left to the builder

- Exact table, column and route names; whether provenance is columns or a `meta` object; how decisions store options.
- How the milestone splits into releases. A natural split is one part per run, with at most one migration each.
- The Codex model names for the default tier map, taken from Codex's current documentation.
- The final MCP tool set, within the rules above.
- Page layouts, checked by headless screenshots and e2e at 375, 768 and 1280 px, light and dark.
- The prompts' exact wording, kept testable: tests hold that every prompt is listed, documented, and names only tools that exist.
