# Structured requirements design (VAQ-2)

Owner brainstorm, 2026-11-26. Builds on the agent workflow design (`2026-11-25-agent-workflow-design.md`, Decision 1: requirements live in tickets, with an "Acceptance criteria" list), references (Part 2), and feeds VAQ-151 (the view from a spec), Factory (VAQ-154) and the new-project intake (VAQ-118's notes). Test management, built on this, is its own ticket.

## Intent

Two problems (owner):
1. **Tickets reach Done while missing parts of what was asked.** Nothing checks a ticket against its acceptance criteria.
2. **Some people want requirements first**, before tickets are broken down, planned and built. Today a requirement exists only as a ticket's description.

So: requirements can be written up front as their own items, every acceptance criterion becomes something Vaquero tracks, with a state and evidence, and the chain from requirement to criteria to implementing tickets to evidence to the owner's acceptance is visible and checked.

## Goals

- A ticket **kind**: `task` (every ticket today) and `requirement`, designed so `test` and `defect` slot in later.
- Acceptance criteria as addressable items, `VAQ-12#AC-3`, each a plain line or a Given/When/Then block (owner: either, per criterion).
- Each criterion's state, `open`, `met` or `waived`, with evidence for `met`.
- Tickets linked to the requirements they implement.
- The owner accepts a requirement as a whole; criteria flow without the owner.
- A warning, never a block, when a ticket moves to review or done with criteria open.
- A Requirements view per project.

## Non-goals

- Test cases, cycles, executions and imported results: the test management ticket.
- The intake interview that asks the questions (a workflow prompt); it writes requirement tickets.
- Requirements as repo files (Decision 1 kept that door open; not now).
- A readiness gate on missing criteria (controller's call: it would unready every existing ticket; triage keeps telling thin from ready).

## Anti-goals

- Two sources of truth for a criterion's state (a checkbox in the text and a row in the database).
- State changes that rewrite the description (they would change the requirement hash and unready approved plans).
- Hard blocks that push agents to mark criteria met without checking.

## Approaches

1. Requirements and criteria as records of their own, outside descriptions. Clean data, but criteria leave the Markdown agents read and the approval hash. Rejected.
2. Requirements as repo files. Versioned with code, but edited often, so git collisions. Rejected.
3. **Requirements as a ticket kind, criteria parsed from the description with stable markers core adds, state and evidence in a small table.** Reuses descriptions, history, versions, search, references and approvals; Markdown stays the one text. **Chosen** (owner, 2026-11-26).

## Ticket kinds

- `tickets.kind`: `task` (default, every existing ticket) or `requirement`. Later kinds (`test`, `defect`) add values and views, nothing else.
- Requirement tickets are left out of the board, Focus, the list view, `ready`, Factory's stations and claims (a claim on one is refused: "Requirements aren't claimed; claim a ticket that implements it."). They have their own view (below), history, comments, references and plans like any ticket.
- **A requirement's status** uses the project's statuses with fixed meaning by category: backlog is draft, todo and in progress are active, done is **accepted**, cancelled is retired. **Only the owner, acting directly, moves a requirement into a done status** ("Only the owner accepts a requirement."); that move is the acceptance.
- Requirements are not part of the ticket tree (controller's call): a requirement has no parent and no sub-tickets, and no task's parent is a requirement ("A requirement isn't part of a ticket tree; link tickets to it with implements."). So readiness, `lean` and the board's open-sub-ticket rule never meet one.
- **Who moves a requirement:** agents and the owner move one between draft and active; only the owner, acting directly, moves one into or out of a done status, or into or out of a cancelled one ("Only the owner accepts or retires a requirement.").
- `vaquero ticket new KEY TITLE --kind requirement`, `kind` on the API and MCP create; a ticket's kind can change only between `task` and `requirement` while it has no `implements` links either way.

## Criteria

- **Where:** in any ticket's description (task or requirement), the list items under a heading named "Acceptance criteria" (any level, any case). Nothing else is parsed.
- **When markers are added** (controller's calls): only by a save that changes the description, never by a status, tag or other field save, so an approved ticket's requirement hash moves only when its text does. No migration rewrites existing tickets: criteria already written under the heading as a task list are taken up on the ticket's next description save. Then `- [x] …` becomes `met` with the evidence note "Checked in the text before criteria were tracked." (by the saver), `- [ ] …` becomes `open`, and the checkbox is dropped from the text as the marker goes in: `- AC-1: …`.
- **Markers:** on such a save, core gives each item without one the ticket's next number and writes it into the text in the same save, before the requirement hash is computed: `- AC-3: Exports include attachments`. A Given/When/Then criterion is the item's continuation lines:

  ```
  - AC-4: Export includes attachments
    Given a ticket with two attachments
    When the owner exports
    Then the archive holds both files
  ```

  Numbers are per ticket (`tickets.next_ac`), never reused. No checkboxes: a `[ ]` or `[x]` in an item is kept as text and ignored (controller's call: state lives in one place).
- **Edits:** an item keeps its number through text edits and moves. A save that drops markers (an agent regenerating the text) first matches each unmarked item to a criterion whose marker vanished in that save, by its text after NFC and whitespace folding; a match keeps its number and state. API, CLI and MCP answers always carry the description with markers, so agents see them (controller's call). Otherwise, an item whose marker is removed is a removed criterion: its row is kept as `removed` with its evidence, and an event says so. A criterion whose text changes after it was `met` goes back to `open` (its evidence was for the old text), recorded in history (controller's call).
- **Table `criteria`:** `ticket_id`, `number`, `text` (as last saved), `form` (`line` or `gwt`), `state` (`open`, `met`, `waived`, `removed`), `updated_at`, `updated_by`. **Table `criterion_evidence`:** `ticket_id`, `number`, `kind` (`test`, `commit`, `review`, `note`), `target` (a test as `path:Name`, a commit, a reference, or text), `by`, `at`. **Checked is worked out on read, never stored** (controller's call), cached by main's head like the other git checks: a test is checked when its file exists on main and contains the name (a plain text search, the same for every language); a commit when it is an ancestor of main; a review when its reference resolves. Without a repo path nothing is checked, and the evidence still counts.
- **Who:**
  - an agent or the owner marks a criterion `met`, with at least one piece of evidence ("Add evidence: a test, a commit, a review or a note.");
  - the owner, or the agent that marked it, reopens it;
  - only the owner waives one, with a note ("Only the owner waives a criterion.").
- **Events:** `criterion.updated` (state change, with evidence summary), `criterion.removed`, each on the ticket; the description save that adds markers is the ordinary `ticket.updated`.
- **The warning:** a move of a task into an in-review or done status while any of its criteria are open answers with `warning` (as a claim does): "2 of 5 acceptance criteria aren't met." The board card shows "3/5" when a ticket has criteria; Factory's project line gains "N done with open criteria".

## Requirements and the tickets that implement them

- A new link kind, `implements`, from a task to a requirement (only a requirement as the target; any project? controller's call: the same project only, as decisions are). A requirement's page lists its implementing tickets with their status and their own criteria counts. Moving a ticket with `implements` links either way to another project is refused ("Remove its implements links first."); deleting a project removes its links as it does other links (controller's calls).
- A requirement's criteria are marked from the work of its tickets: evidence on `VAQ-12#AC-3` names the test or commit, wherever it was made.
- A requirement is **ready to accept** when every criterion is met or waived; it shows "Ready to accept" and the owner's move to done is the acceptance. Moving it with open criteria is allowed for the owner, with the warning.
- Epics stay as they are: an epic is a task with children; a requirement is what is asked, and an epic may implement it.

## The Requirements view (`/p/{key}/requirements`)

A tab after List. Requirements by state (draft, active, accepted, retired), each row `VAQ-12 · title · 4/5 criteria met · 3 tickets · Ready to accept`. A requirement's own page is the ticket page, with its criteria rendered with their state, evidence and checks beside each (the Markdown stays as written), its implementing tickets, and Accept (the move to done) for the owner. A task's page renders its criteria the same way.

## API, CLI and MCP

- `GET /api/v1/tickets/{ticket}/criteria`; `POST /api/v1/tickets/{ticket}/criteria/{n}` (`{state, evidence: [{kind, target}], note?}`); tickets carry `kind` and `criteria: {met, total}`; `?kind=` on ticket lists (default `task`, so today's lists are unchanged).
- `vaquero criteria TICKET`; `vaquero criterion met TICKET AC-3 --test NAME | --commit SHA | --review REF | --note TEXT` (repeatable); `vaquero criterion reopen|waive TICKET AC-3 [--note TEXT]`; `vaquero requirements KEY`.
- MCP: `update_criterion` (one API request), and `kind` on `create_ticket` and `list_tickets`; `bulk_update` items gain a `criteria` action.

## Constraints

- Only core writes `criteria`, `criterion_evidence`, `kind` and the markers.
- `TestWorkflowOffUnchanged` holds: ticket lists default to `task`, `kind` and `criteria` are left out of answers for a task with no criteria (so the goldens' JSON and board are byte for byte the same), and no golden has an "Acceptance criteria" heading.
- Factory (VAQ-154) isn't built yet: whichever of the two lands second leaves requirements out of the stations and adds "N done with open criteria" (controller's call). Markers are added in every project (requirements don't depend on the workflow), but only where that heading exists.
- The help gains a "Requirements" article; `markdown` explains the markers.
- Export and import carry the new tables and columns; `coreseed` seeds a requirement with criteria (one Given/When/Then), an implementing ticket and evidence.

## Left to the builder

- The parser's exact handling of nested lists under the heading (top-level items only are criteria is a sensible default).
- Column names, indexes, payload fields beyond those named.
- The rendering of criteria states on the ticket page.
