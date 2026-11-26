# Audit records and the factory service log (VAQ-105)

Owner brainstorm, 2026-11-26. Builds on the agent workflow design (`2026-11-25-agent-workflow-design.md`: releases, runs, the `audit` prompt) and the Factory view (`2026-11-26-factory-view-design.md`, VAQ-154).

## Intent

VAQ-105 asked for a place to record and show project-wide events, with the project's own releases and "when a skill like continuity, hygiene or docs last ran" as examples. The workflow design already records releases (an approval naming the version tag) and runs (Part 3). What nothing records is that an **audit ran**: a clean audit leaves no trace, so "when did hygiene last run?" and "is it due?" have no answer.

This design adds the audit record, a due-ness setting per audit kind, and a **service log** panel on the project's Factory page: like a machine's service record on a factory floor, the last time each kind of upkeep happened and whether it is due, beside the last release and the run in progress (owner: "the tie in to the factory page is exactly what I was thinking").

## Goals

- Record each audit: kind, when, who (with provenance), the commit it looked at, the tickets it filed (possibly none).
- Say when each audit kind is due: every N merged runs or D days, whichever first, per project.
- Show it on the project's Factory page, and in Activity as a kind group.
- Record from the CLI, the API and MCP; the `audit` prompt (Part 4) records at the end of every audit.

## Non-goals

- Running audits. Vaquero never runs an agent; triage proposes a due audit as a decision, as the workflow design says.
- A page of its own. The panel lives on Factory; the record lives in Activity.
- Vaquero's own upgrades on the owner's machine (owner: the examples meant the project's releases).

## Approaches

1. **An `audits` table and one core operation that appends `audit.recorded`**, with each project's cadence stored as a setting like the tier map. **Chosen** (owner, 2026-11-26).
2. The event alone, its payload the record. Less code, but the only fact in Vaquero stored only in the event log, and due-ness read out of payloads. Rejected.
3. An answered decision per audit. No new storage, but it blurs "only the owner answers" and clutters the decision queue. Rejected.

## The record

- **Table `audits`:** `id` (ULID), `project_id`, `kind` (`qa`, `hygiene`, `continuity`, `docs`: the workflow design's four, `core.AuditKinds`), `at`, `actor_id`, `commit` (a full or abbreviated SHA as given, at most 64 characters, "" when not given), `note` (optional, at most 500 characters, NFC), and `audit_tickets (audit_id, ticket_id)` for the tickets it filed. Exported and imported like the other workflow tables; `DeleteProject` removes them; `coreseed` seeds one.
- **Core:** `RecordAudit(ctx, actor, project, AuditInput{Kind, Commit, Note, Tickets []TicketKey})`: any actor (owner or agent); tickets must be in the project; one `audit.recorded` event on the project, payload `{audit_id, kind, commit, tickets, ticket_ids}`, with the context's origin as `meta` like every event. Each filed ticket's history shows the line through `payload.ticket_ids`: built after run P, whose `event_tickets` table (P-R1, filled by `emit` from `payload.ticket_ids`) makes the line come free with no new lookup (controller's call).
- **Filed tickets are history:** `audit_tickets` keeps the tickets as recorded, even after one moves to another project; "3 tickets" counts the record. Rows go with their audit (`DeleteProject` removes the project's audits and their rows) and with a deleted ticket (foreign key cascade); `MoveTicketToProject` changes nothing (controller's call). No edit or delete: a wrong record is followed by a right one (controller's call: it is a log).
- **Cadence:** `projects.audit_policy`, JSON `{kind: {"runs": N, "days": D}}`, "" for the defaults; either part 0 turns it off; both 0 turns the kind off. Defaults (controller's call): every 5 merged runs or 14 days for hygiene, continuity and docs, every 3 merged runs or 7 days for qa. Only the owner, acting directly, changes it (the same rule as the tier map and repo settings: "Only the owner changes the audit schedule."). Settings page: a small table in the workflow section, shown only with the workflow on.
- **Due only with the workflow on** (controller's call): with it off, records are kept and shown but nothing is ever due, the API answers `due: false` with no reason, and no Factory alert counts it, so a project that never audits isn't flagged for good.
- **Due:** a kind is due when it has never run, or `days` have passed since its last record, or `runs` merged runs have landed since (merged runs count once Part 3 exists; until then only days apply, and the panel says nothing about runs). Computed on read (`core.AuditDue`), never stored.
- **Since:** with a repo path (Part 2) and a recorded commit, the number of commits on main since it, through `internal/gitread` (`rev-list --count COMMIT..main`, bounded by gitread's timeout), on the service log panel, in `GET …/audits` (`commits_since`) and `vaquero audits`; never on the global Factory page. Cached in memory by (main's head, commit), so a live refresh runs git only when main moves. A commit git can't resolve uniquely, or that isn't an ancestor of main, shows "commit not on main" and no count. `RecordAudit` doesn't check the commit against the repo: a project may have no repo path, and recording works offline (controller's calls).

## Surfaces

- **CLI:** `vaquero audit record KEY KIND [--commit SHA] [--note TEXT|-] [--ticket TICKET]...` and `vaquero audits KEY` (each kind: last run, by whom, commit, tickets filed, due or when due).
- **API:** `POST /api/v1/projects/{key}/audits`, `GET /api/v1/projects/{key}/audits` (the latest per kind with `due` and `due_reason`, plus `?kind=` for one kind's history, newest first, paged by `limit`). The project answer carries `audit_policy` (effective).
- **MCP:** `record_audit`, one API request like every tool; the tool list and its documentation grow by exactly this one.
- **Activity (VAQ-153):** a "Releases, runs and audits" kind group holding `audit.recorded` now, and the run and release kinds when Part 3 adds them. Filter groups live in one table, and the test that every kind is in a group makes the new kind land there.
- **Factory project page (VAQ-154):** a **Service log** panel after the stations:

  ```
  Service log
  QA          2 d ago · claude · a1b2c3d · 0 tickets       due in 5 d
  Hygiene     12 d ago · claude · 9f8e7d6 · 3 tickets      due · 40 commits since
  Continuity  never                                        due
  Docs        3 d ago · you · e5f6a7b · 1 ticket           due in 11 d
  Release     v0.1.6 · Nov 24                              (when Part 3 records releases)
  Run         run-o · running · batch 2 of 3               (when Part 3 adds runs)
  ```

  Each line links to Activity filtered to the project and the "Releases, runs and audits" group (no per-audit-kind filter is added to Activity; controller's call); a due kind is marked with words and an icon, not colour alone. The global Factory page's project lines gain "2 audits due" among their alerts. Workflow-off projects show the panel only when they have audit records (controller's call: audits are useful without the workflow, but an empty panel isn't).
- **Help:** the `agent-workflow` article gains an "Audits" section; the cli, api and mcp articles their lines.

## Constraints

- Only `internal/core` writes `audits` and `audit_tickets`; one event per record.
- The migration is the next free number when built; `testdata/upgrade` fixtures untouched; `make fixture` before the next release picks the tables up.
- Theme tokens only, no `style=`; e2e at 375 px and desk widths for the panel (with VAQ-154's tests).

## Order

1. The record, cadence, CLI, API, MCP and Activity group: after run P (for `event_tickets`) and VAQ-153 (for the kind group).
2. The service log panel: with or after VAQ-154.
3. Release and run lines, and merged-run counts: when Part 3 lands. The `audit` prompt's record step: Part 4.

## Left to the builder

- Column and index names; the JSON field names in API answers beyond those above.
- The settings table's exact layout.
- The service log's icons.
