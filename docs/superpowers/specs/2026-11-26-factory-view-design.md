# Factory view design (VAQ-154)

Owner brainstorm, 2026-11-26. Built after VAQ-153 (Activity, the record), which it links into and whose indexes and project tab it builds on (controller's call; no fallbacks for an unfiltered Activity). Related: the agent workflow design (`2026-11-25-agent-workflow-design.md`; runs are its Part 3).

## Intent

Coding agents can work around the clock. The Factory view is the factory floor seen from above: the moving parts, their state, and above all **whether the agents are starved of work**: waiting for ready tickets because work isn't triaged, planned, approved or unblocked. The owner is the usual bottleneck in a 24/7 factory (decisions, approvals, triage), and this page shows that bottleneck and what to do about it.

Activity (VAQ-153) is the record: what happened, exactly. Factory is the floor: what is going on and how supply is holding up. Factory summarises; anything that means "these events" links into Activity with filters.

## Goals

- Answer "are the agents waiting for work?" at a glance: the ready queue now, how long it lasts at the current pace, the last 24 hours of it, and how often agents asked for work and found none.
- Say why the other todo tickets aren't ready, grouped by reason, and what waits on the owner, so each group points at a job.
- Show the floor: per project, how much work sits at each stage, who is working on what, and what's stuck.
- How much of the day the line was running (at least one claim held), from claims, and each agent's busy hours.
- A global page (`/factory`) and a per-project one (`/p/{key}/factory`).

## Non-goals

- Starting, stopping or scheduling agents. Vaquero still never runs an agent.
- Alerts or a header count. Page only for now (owner: "start with a page and we can evolve from there").
- Cost, tokens, or model spend.
- Runs (Part 3) are not built here; the page leaves a place for them.

## Anti-goals

- Numbers that sound precise but are guessed. Where history doesn't exist yet, the page says so ("History starts Nov 26.") rather than inventing it.
- A second copy of the readiness rules. Readiness is asked of `internal/readiness` and the board's ready rule, as Focus and `ready` ask them.
- Making the page slow or making the sweep heavy: a sample is a handful of batched reads.

## Approaches

1. **A snapshot every 5 minutes from the sweep, plus a counter of empty ready answers.** Exact from the day it ships, cheap, outside the tracker tables. **Chosen** (owner, 2026-11-26).
2. Rebuild past readiness by replaying events. No new table and history from the first event, but a second readiness implementation that drifts, and slower as history grows. Rejected.
3. No history: only the queue now. Can't answer "were the agents waiting overnight?". Rejected.

## Stations

A ticket is at exactly one station, by its status category and the existing rules:

| Station | Tickets |
|---|---|
| Intake | backlog category, except the triage statuses |
| Triage | statuses named Needs decision or Needs design (the names `/p/{key}/triage-statuses` adds) |
| Ready | todo and ready (the board's rule, plus readiness with the workflow on) |
| Not ready | todo and not ready |
| Working | in progress |
| Review | in review |
| Shipped | reached done today (UTC), and this week, from `status_changed_at` |

Cancelled tickets are on no station.

## The global page (`/factory`)

Linked from the main nav after Activity. Live through `/events/stream` like Focus.

**Supply band** (top), across every non-archived project:
- **Ready queue:** the count now and the runway, "about 3 h of work at this week's pace". Pace is tickets reaching done per hour the line was running, over the last 7 days. With no done tickets in that window: "No pace yet."
- **The last 24 hours:** the ready count per hour (the hour's lowest sample, so an hour that touched empty shows empty), hours at zero marked. Under it: "Empty for 5 h of 24 · 2 of them while an agent was working" (while a claim was held in that hour).
- **Asked and found nothing:** how many ready requests came back empty in the last 24 hours, and the last one's time. Each ask counts once on the global page; a project's page counts the asks scoped to it plus the unscoped ones (an empty unscoped answer means that project had nothing ready too).
- **Why the other todo tickets aren't ready:** counts by reason, the readiness `Reasons` with the workflow on, and the board's conditions (blocked, open sub-tickets, claimed, assigned) for every project, each linking to the list view filtered when a filter exists for it. Then "N decisions waiting on you", a disclosure listing each open question or approval (its kind, its question, the tickets it names, linked) on the Factory page itself; there is no web decisions page yet, and the Part 5 decision queue replaces the list with a link when it exists (controller's call).
- **Counting reasons:** a ticket with several reasons counts under each, so the reason counts can sum past the not-ready total; the band says "A ticket can have more than one reason." (controller's call: each count is a job to do.)
- **Links:** the Ready count links to the project's Focus (its Next lane), and the reasons to Focus's Waiting lane, which already lists each ticket with its reasons; on the global page, to the project with the most. No new list filters (controller's call).
- Before 24 hours of samples exist, the strip shows what it has and says "History starts {day}."

**Line running** (beside the band): hours of the last 24 with at least one live agent claim (the owner's own claims don't count: the line is the agents), as a share, and the share over the 7 days before for comparison. From claim events: `ticket.claimed` starts a claim; it ends at `ticket.released`, `ticket.claim_lapsed`, the `ticket.updated` that moved it to done or cancelled or changed its assignee (the `tickets_claim_ends` and `tickets_claim_reassigned` triggers end it without a claim event), or a later claim of the same ticket (controller's calls). Below the project lines, a **Workers** table: each agent active in the last 24 hours, with harness and model from `events.meta` when recorded, busy hours, tickets finished, and what it holds now.

**Project lines** (layout C, owner's pick): one row per non-archived project, ordered by most work waiting (ready plus working plus review), then key:
- a bar split by station (intake, triage, ready, not ready, working, review, shipped this week), with the counts in words under it;
- the agents working on it now (name, ticket, lease left);
- its alerts: decisions waiting on the owner, tickets stuck (in review or in progress for more than 24 h), claims lapsed today;
- its ready count and runway.
Each row links to the project's Factory page. Archived projects are left out.

**Runs:** when Part 3 adds runs, each project line shows its running runs (name, state, batch) and the band counts tickets in runs. Until then nothing is shown for them.

## The project page (`/p/{key}/factory`)

A tab in the project header after Activity (VAQ-153 adds that tab) (shortcut `g y`, since `g f` is Focus). Layout A: the stations left to right as tiles, each with its count and its oldest ticket's age, the working agents at the Working station, alerts above. Then the same supply band, the line running figure and the Workers table, all for this project. Each tile links to the list view filtered to that station where a filter exists (ready, not ready by category, in progress, in review).

## Data

**`factory_samples`** (new table, one migration): `at`, `project_id`, `ready`, `not_ready`, `reasons` (JSON object, reason → count), `intake`, `triage`, `working`, `review`, `claims` (live claims), `open_decisions`. Primary key `(project_id, at)`.
- Written by `internal/sweep` every 5 minutes (`SampleEvery`), one row per non-archived project, in one transaction, through its own writer like the `mcp_calls` cleanup, never through core: samples are derived observations, not tracker changes, and add no event.
- Reads for a sample reuse `board.LoadProjectStats`-style batched queries and one `readiness.CheckAll` per workflow project: a fixed number of queries whatever the number of tickets.
- Pruned after 90 days by the sweep.
- Not exported or imported, like `mcp_calls` and previews. The data-and-backups article lists it with them.

**Empty ready answers:** `GET /api/v1/ready` (which `vaquero ready` and MCP `ready_tickets` call) counts, in memory, each answer with no tickets, keyed by the project asked or none for an unscoped ask, with the last one's time. The sweep writes them to their own table, **`ready_misses`** (`at`, `project_id` nullable: null for an unscoped ask, `count`, `last_at`), one row per key with a count since the last sample, and resets the counter; so `factory_samples` has no `ready_misses` column (controller's call: the global count is a plain sum, never multiplied by projects). A restart loses at most 5 minutes of counts. Handlers still write nothing. Pruned and left out of exports with the samples.

**Line running and workers** come from claim events, read through new indexed queries (the `events (kind, seq)` index VAQ-153 adds serves them).

**Pace:** done transitions (`ticket.updated` with a status change into a done status) over 7 days, divided by running hours over the same 7 days.

## Constraints

- Reads through `Deps.R`; the samples writer lives in `internal/sweep`, beside the existing cleanups.
- No `style=` and no `<svg>` in templates (only `icon.templ` and `logo.templ` draw SVG). Bars and the hour strip are built from fixed Tailwind classes (for example a 24-column grid with one of a few height steps, and station bars as a grid of 100 or 20 cells, or `<progress>` with `progress-vq`). Theme tokens only, with colours meaning the same thing everywhere and never colour alone: each part carries its count in words.
- Workflow-off projects: no readiness reasons, only the board's ready rule and conditions. `TestWorkflowOffUnchanged` still holds for the pages it covers.
- Live through the stream; `make e2e` at 375 px and desk widths, both themes.
- A help article, "Factory", in section `using`; `/factory`, the tab and `g y` in the articles the tests check.
- Times in UTC, through `internal/timefmt`.

## Left to the builder

- The sample's exact columns beyond those named, and the JSON shape of `reasons`.
- The exact Tailwind construction of the bars and strip within the constraints.
- Which icon the nav link and tab use.
- How the Workers table orders agents (busy hours, then name, is a sensible default).
