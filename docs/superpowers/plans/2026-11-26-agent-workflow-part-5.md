# Agent workflow, part 5: the decision queue and the Trace panel — implementation plan (VAQ-139)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A web decision queue (`/decisions`, with a count in the header) where the owner answers questions and approvals directly and overrules rulings; the Trace panel leading the ticket page and the Repo line on the project page; and links to the queue wherever earlier work left a `vaquero decision show` hint.

**Architecture:** Pages over what exists: the decision reads and core's answer, withdraw and ruling rules (Part 1), Part 2's git reads (`/tickets/{ticket}/commits`, `/projects/{key}/repo/status`, references and pins) and Part 3's runs. The Trace panel loads as a fragment after the ticket page, so git never slows the page; its reads are cached on main's head.

**Tech Stack:** Go 1.27, templ, htmx, `internal/gitread`.

**Spec:** `docs/superpowers/specs/2026-11-25-agent-workflow-design.md`, Part 5 (Trace panel sketch), as trimmed on VAQ-139 (owner, 2026-11-26). Built after Part 4 (VAQ-138) and VAQ-155 (its repo/status cost fix: one `for-each-ref`, `rev-list` only for tracked branches, one deadline per request).

## Rulings (controller, 2026-11-26; the owner can overrule)

- **P5-R1: answers in the web are direct.** The queue posts as the owner with no agent, so answers are recorded `direct` (Decision 7). The page shows to anyone who can open Vaquero (local mode: the owner), and core refuses any other actor as it does today.
- **P5-R2: the header count** is open questions plus open approvals across non-archived projects (rulings aren't waiting on anyone), from one indexed count read on every page through the Shell, shown as a number badge on a "Decisions" nav link only when above zero.
- **P5-R3: the Trace panel is a fragment**, `GET /t/{ticket}/trace`, loaded by `hx-trigger="load"` and refreshed on `vq:change`; git failures show the line's own message ("Git read failed: {first line}"), never fail the page. Cached per (project, main head, last fetch time).
- **P5-R4: hints become links whichever lands first.** Where Factory (VAQ-154), Architecture (VAQ-118) or Requirements (VAQ-2) are built before this part, this part replaces their `vaquero decision show ID` text with a link to `/decisions/{id}`; where they're built after, their plans link directly (each plan's builder checks whether `/decisions/{id}` exists). `TestNoDecisionShowHints` greps the templates.

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`, and `make e2e` (every task changes pages).
- Commit messages `{Area}: {summary} (VAQ-139)`, ending with the `Co-Authored-By` line.
- Every write goes through core's existing calls (`AnswerDecision`, `WithdrawDecision`, `RecordRuling` overrule path); no new writes.
- Workflow off: `TestWorkflowOffUnchanged` holds; the Trace panel shows only its git lines and only when the project has a repo path; with neither, no panel and no fragment request.
- e2e at 375, 768 and 1280 px, light and dark (the spec's Part 5 line).
- Copy (exact): "No decisions waiting on you.", "Approving covers", "Answered directly by you", "Relayed by {agent}: \"{words}\"", "unknown until fetched".

## Review Focus

1. **Answering Approve on a plan approval whose plan or requirement changed since it was asked:** core's refusal ("The plan or requirement for VAQ-12 changed since this was asked; ask again.") shows on the card, the card stays open, nothing is written. Test in Task 1.
2. **Two tabs answering the same decision:** the second gets "This decision was answered already." with the recorded answer, not a server error. Test in Task 1.
3. **A repo whose remote was never fetched, or a repo path that has gone missing:** the Repo and Merged lines say "unknown until fetched" or the git error; the rest of the panel shows. Test in Task 2.
4. **A ticket in no run:** the Branch line is left out (not "none"); a ticket in a run whose branch was deleted after merging shows the merged state from the run. Test in Task 2.
5. **An answer note with Markdown and a 5,000-character note:** rendered through `internal/markdown`, cut as core cuts it, never raw HTML. Test in Task 1.

---

### Task 1: The decision queue

**Files:**
- Create: `internal/web/handlers/decisions.go`, `internal/web/templates/decisions.templ`
- Modify: `internal/web/router.go` (`GET /decisions`, `GET /decisions/{id}`, `POST /decisions/{id}/answer`, `POST /decisions/{id}/withdraw`), `internal/web/templates/layout.templ` (nav link and badge, P5-R2), `internal/db/queries/decisions.sql` (`CountOpenDecisions`), `internal/web/handlers/render.go` or the Shell data path (the count), `internal/web/icons/ui.json`, `internal/help/articles/agent-workflow.md` ("The decision queue")
- Test: `internal/web/handlers/decisions_test.go`, `internal/e2e/decisions_test.go`

**Interfaces:**
- Consumes: `d.R.ListDecisions`, the API's `decisionAsked` (move it to a shared helper in `api_decisions.go` if it isn't already) for "Approving covers", `core.AnswerDecision`, `core.WithdrawDecision`.
- The page: filters `project`, `run`, `kind`, `topic`, `state` (default open) in the query string with the board's chips and menus; open questions and approvals first, oldest first; each card shows the question (Markdown), the options as buttons with their trade-offs, the recommendation marked "Recommended", why it matters, the tickets it names (linked), for a plan approval "Approving covers" with each ticket's plan revision and children, a note box, and Withdraw; rulings (answered by their maker) listed after, each with "Overrule" opening the same options; answered decisions folded with "Answered directly by you" or "Relayed by {agent}: \"{words}\"". `/decisions/{id}` is one card. Live (`data-live`).
- [ ] **Step 1: Write the failing tests**: handlers `TestQueueListsOpenFirst`; `TestQueueFilters`; `TestAnswerDirect` (recorded `direct`, no `RelayedBy`); `TestAnswerRefusedStaysOpen` (Review Focus 1); `TestAnswerTwice` (Review Focus 2); `TestOverruleRuling`; `TestWithdraw`; `TestHeaderCount` (two open, one ruling → "2"; none → no badge); `TestNoteRendering` (Review Focus 5); e2e answering by button and by keyboard, widths and themes.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Web: the decision queue (VAQ-139)`.

### Task 2: The Trace panel and the Repo line

**Files:**
- Create: `internal/web/handlers/trace.go`, `internal/web/templates/trace.templ`
- Modify: `internal/web/router.go` (`GET /t/{ticket}/trace`, `POST /p/{key}/fetch` if not already on the web), `internal/web/templates/tickets.templ` (the panel's slot at the top of the ticket page, P5-R3), `internal/web/templates/board.templ` (`projectHeader`: the Repo line under the title when the project has a repo path), `internal/help/articles/repositories.md` ("The Trace panel")
- Test: `internal/web/handlers/trace_test.go`, `internal/e2e/trace_test.go`

**Interfaces:**
- Consumes: the handlers behind `/tickets/{ticket}/commits` and `/projects/{key}/repo/status` (call their shared helpers, not HTTP), references and their pins (Part 2), the ticket's plan and its current approval (readiness), Part 3's `runOf` for the run and branch.
- `type TraceLine struct{ Stage /* Spec, Plan, Branch, Commits, Files, Merged, Released, Repo */, Text, Href, Note string; Unknown bool }`; `func traceOf(ctx context.Context, d Deps, p gen.Project, t gen.Ticket) ([]TraceLine, error)`:
  - **Spec:** each `spec` or `requirement` reference, path and anchor, with "(unchanged since approval)" or "(changed since approval)" from the pins; workflow on only.
  - **Plan:** "rev {n} · approved by you, {Nov 26}" or "rev {n} · not approved" or left out with no plan; workflow on only.
  - **Branch:** the open or last run's branch, "pushed" or "{n} commits not pushed" from repo status; left out with no run (Review Focus 4).
  - **Commits:** "{n} mention {KEY}" with the diffstat; **Files:** the first five changed files, linked to the file view, "· …" past five.
  - **Merged:** "main ✓ · origin/main ✓" (or ✗) from the commits' ancestry and the remote-tracking branch as of the last fetch; "unknown until fetched" before one (Review Focus 3).
  - **Released:** the first tag containing the merge, "· tag on origin ✓" when the remote has it.
  - **Repo:** "main {n} ahead of origin · as of last fetch, {2 h ago}", with Fetch now.
  - Workflow off: only Commits, Files, Merged, Released, Repo; no repo path: none.
- [ ] **Step 1: Write the failing tests** with gitread's test repo helpers: each line's text for a fixture repo and ticket; workflow off shows git lines only; no repo path → the ticket page has no panel slot; a git error on one line leaves the others (Review Focus 3); a ticket in no run has no Branch line, and a merged run with a deleted branch shows Merged from the run (Review Focus 4); the fragment is cached (a second request runs no git with main unchanged); e2e: the panel loads after the page, Fetch now updates the Repo line, widths and themes.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS.
- [ ] **Step 5:** Gate, commit `Web: the Trace panel and the Repo line (VAQ-139)`.

### Task 3: Links to the queue

**Files:**
- Modify: whichever of `internal/web/templates/factory.templ`, `architecture.templ`, `requirements.templ` exist (P5-R4), the CLI's `decision show` output (a line "Answer in the web: http://{addr}/decisions/{id}" when the server is local), `internal/help/articles/agent-workflow.md`
- Test: `internal/web/templates/hints_test.go` (`TestNoDecisionShowHints`), the touched pages' tests

- [ ] **Step 1: Write the failing test** `TestNoDecisionShowHints`: no `.templ` file contains "vaquero decision show"; each page that lists a decision links `/decisions/{id}`.
- [ ] **Step 2:** → FAIL (where hints exist) or PASS trivially (where none were built yet; the test then guards later work).
- [ ] **Step 3:** replace the hints. **Step 4:** → PASS; `make e2e`.
- [ ] **Step 5:** `make ci`; CLAUDE.md package map (the queue, the Trace panel, the routes, the header count; P5 rulings under the agent workflow invariants); commit `Web: link decisions to the queue (VAQ-139)`; VAQ-107's Agent workflow milestone is then complete.
