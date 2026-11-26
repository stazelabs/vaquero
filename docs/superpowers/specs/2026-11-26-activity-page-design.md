# Activity page design (VAQ-153)

Owner brainstorm, 2026-11-26. Related: VAQ-154 (Factory view).

## Intent

Activity is Vaquero's record: what happened, exactly, who did it and when. Today `/activity` is the newest 100 events from every project as one flat list, with no filters, no paging and no detail, so it reads as a dump. In the owner's own data 97% of events are one agent's, and a fifth are claims and releases, so the owner's own changes and any one fact are buried.

The page is shaped for **looking up one fact** ("who moved VAQ-88 to Done, and when?"). Reading a stretch of time ("what did the agents do overnight?") belongs to the Factory view (VAQ-154), which summarises and links into Activity filtered through the address. Activity still has to land well from such a link: filters in the query string and an exact time on every row.

## Activity and Factory

- **Activity is the record.** Backward-looking, complete, precise. Nothing is summarised away; filters narrow it.
- **Factory is the floor** (VAQ-154, designed once runs exist): what is going on now, how work is flowing, what needs the owner. Mostly current state, events only for counts.
- They share the events table. The bridge is Activity's address: every Factory number or line that means "these events" is an Activity link with filters.

## Goals

- Find one event quickly by project, who, kind of change, ticket and date.
- Every row says exactly when and who, and opens to the whole change and its provenance.
- Page through all of history, newest or oldest first, with stable pages.
- A per-project Activity tab beside Focus, Board and List.
- The same filters on `GET /api/v1/events`, `vaquero activity` and MCP `recent_activity`.
- Stays fast as history grows: every filter combination uses an index.

## Non-goals

- Charts, counts over time, "now" panels, run summaries: Factory's job.
- Free-text search over event sentences. Search finds tickets; the ticket filter finds their events.
- Editing or deleting events. The record is append-only.
- Changing what events are written.

## Anti-goals

- Hiding or folding events by default to look tidy. One row is one event (the one exception stays: reorders are left out unless asked for, as today).
- Rows that move under the reader while they read.
- A filter that silently scans `events`.

## Approaches

1. **One hand-written query built from the filters** in Go, as `SearchTickets` is: each set filter adds its condition, keyset paging by `seq`, new indexes for who and kind; the ticket filter reuses the ticket history lookups. **Chosen.**
2. Fixed sqlc queries with switchable conditions (`? = '' OR e.actor_id = ?`). SQLite can't use an index through that pattern, so every filtered view scans `events`. Ruled out.
3. A denormalised activity table kept beside `events`. Fast for any combination, but a second copy of the record written on every change, for data an index covers. Too much for a local tool.

## The page

```
Activity                                          (global: /activity)
[Project ▾] [Who ▾] [Kind ▾] [Ticket: VAQ-88 ] [From – To ]   Newest first ▾
 Who: agents ×   Kind: Status changes ×

 Nov 26 14:03  ⬡ claude · opus   VAQ-88  moved VAQ-88 to Done          ▸
 Nov 26 13:58  ⬡ claude · opus   VAQ-88  commented on VAQ-88           ▸
 Nov 26 09:12  ● Owner           VAQ-12  changed priority to Urgent    ▾
   priority   None → Urgent
   via        web · seq 4812 · 2026-11-26 09:12:44 UTC
                                                     ← Newer   Older →
```

**Rows.** A dense table, one event per row, columns: when, who, ticket (or project for a project event), sentence.
- **When:** the exact time, `Nov 26 14:03` (the year shown when it isn't this year), in UTC like every displayed instant; the relative time ("2 h ago") as the title. Formatted through `internal/timefmt`.
- **Who:** the actor's name; an agent carries a small badge with its harness and model from `events.meta` when recorded. The owner and agents are told apart by more than colour.
- **Ticket:** the current key (as ticket history shows it), linked; the title on hover.
- **Sentence:** `EventSentence` / `EventParts` as today, links included.

**Expanded row.** A `<details>` per row (no JavaScript needed to open it). It shows:
- the change: each changed field before → after, from the payload (a description edit says so and links to the ticket's "Show changes", never inlining `body_before`/`body_after`);
- provenance: surface, client, harness, model, run, as recorded (empty ones left out);
- the sequence number and the time to the second, `2026-11-26 09:12:44 UTC`.

**Filters**, in the query string, with the board's filter bar pattern (chips, `details[data-menu]` menus, removable chips per active filter):
- `project=KEY` (global page only; the project tab fixes it). Matches the project the event was recorded in, so a moved ticket's earlier events stay under its old project, as they happened. The ticket column still shows the key now.
- `who=`: `owner`, `agents`, or one agent as `agent:NAME` (the menu lists actors that have events, with a search box as the tag menu has). The prefix keeps an agent named "owner" or "agents" apart from the keywords; agents are unique by name (controller's call).
- `kind=`: one of these groups, several allowed:

  | Group | Kinds |
  |---|---|
  | Tickets | ticket.created, ticket.updated, ticket.moved |
  | Comments | comment.* |
  | Attachments | attachment.* |
  | Links | link.*, project.linked, project.unlinked |
  | Claims | ticket.claimed, ticket.released, ticket.claim_lapsed |
  | Plans and decisions | plan.*, decision.* |
  | Project settings | project.created/updated/archived/restored/deleted, status.*, milestone.*, tag.* |
  | Access | actor.created, token.* |
  | Reorders | ticket.reordered (the only way to see them) |

  The groups live in one table in Go beside `core.EventKinds`; a test fails when a kind is in no group, so a new kind must be placed.
- `ticket=KEY-N` (any case, an old key resolved through `core.ResolveTicket`): exactly that ticket's history, the same events its page shows (its own, link targets, tag events listing it, ancestor moves), through the same indexed lookups as `ListTicketEvents`. The ticket filter wins over the project: on a project's tab, `?ticket=` shows the ticket's whole history, including events recorded in its old project or on tickets elsewhere, with the note "Showing VAQ-88's whole history, from every project." (controller's call: the ticket page and the filter must agree).
- `from=YYYY-MM-DD`, `to=YYYY-MM-DD` (UTC days, inclusive).
- `order=oldest` (default newest first).
- An unknown value in any filter shows an inline notice naming it with a link that clears only that filter, as the board's `noTagNotice` does; it never errors the page.

**Paging.** 100 rows a page, keyset by sequence: `before=SEQ` (the 100 just below SEQ) and `after=SEQ` (the 100 just above SEQ), each shown in the page's order. The query reads one row past the limit to know whether there is more; links "← Newer" and "Older →" appear only then. Pages stay stable while new events arrive.

**Live.** The feed stays `data-live`. On the first page, newest first, a change re-renders it with the filters kept, so new rows appear at the top. On any other page, or oldest first, a change shows a quiet "3 new events · Show" note instead of re-rendering; Show goes to the first page with the same filters. Open `<details>` rows stay open across a refresh (`data-keep`, as Focus does).

**Empty states.** No events at all: "Nothing has happened yet." With filters: "No events match these filters." with a link that clears them all.

**Per-project tab.** `/p/{key}/activity`: the project header gains an Activity tab after List (`viewTab`, with an icon from `ui.json`), keyboard shortcut `g a` (on the global page without a project, `g a` opens `/activity`). It is the same page with `project` fixed and the project menu hidden; the board's ticket filters (tag, priority, milestone) don't carry into it, nor out of it.

**Widths.** At 375 px each row stacks: who and when on one line, the sentence below; the filter bar wraps as the board's does. `make e2e` covers 375 px and desk widths, both themes.

## Data

- One hand-written query in `internal/db` (beside `search.go`), built from a filter struct: optional project id, actor ids or the agent kind, kind list, `from`/`to`, `before`/`after` seq, order, limit. It returns the columns `ListEvents` returns today.
- The ticket filter is its own query: `ListTicketEvents`'s four lookups with the seq window, order and limit applied, plus the other filters on top.
- Dates filter on `at` itself (`at >= from` and `at < to + 1 day`), exactly, through a new `events (at)` index; rows within the range still order by `seq`. Not translated to seq bounds, because `at` comes from the wall clock and an imported archive or a clock stepped back can break the order (controller's call).
- New indexes in one migration: `events (actor_id, seq)`, `events (kind, seq)` and `events (at)`. The existing `events_project (project_id, seq)` serves the project filter. The builder may add composite indexes (such as `(project_id, kind, seq)`) when the benchmark below needs them.
- A test in the style of `TestTicketHistoryUsesIndexes` runs `EXPLAIN QUERY PLAN` for each filter alone and for representative combinations, and fails on a full scan of `events`.
- **Speed bar (controller's call):** a benchmark over a seeded database of 100,000 events, shaped like the owner's (about 97% one agent, a fifth claims), loads the first page of each filter alone and of project + who + kind in under 50 ms each on a laptop. It is a benchmark run at the desk, not a CI gate. "No full scan" alone isn't enough: a filter matching most rows through an `IN` list can sort them all before the limit.
- No total count: counting a filtered set would read it all. The page says only whether there is more.

## API, CLI and MCP

- `GET /api/v1/events` takes the filters `project`, `who`, `kind`, `ticket`, `from`, `to` alongside today's `after`, `newest` and `limit`, plus `before`. Unknown values are a 422 naming the field, as the ticket list's are. Paging keeps today's contract (controller's call):
  - the answer is always oldest first, as now;
  - `after=SEQ`: the events just above SEQ (the polling cursor, unchanged);
  - `newest=N`: the newest N that match, oldest first (unchanged, now filterable);
  - `before=SEQ`: the newest that match below SEQ, oldest first (new, for reading back); `before` with `after` is a 422;
  - `Vaquero-More: true` when more match past the page in the direction read;
  - `Vaquero-Seq` stays the newest sequence overall, read before the list.
  So `order` is the web page's only, and requests without the new parameters answer byte for byte as today.
- `vaquero activity` keeps its defaults (the newest 20, `--after` for polling) and gains `--project`, `--who`, `--kind` (repeatable), `--ticket`, `--from`, `--to`, `--before`; it prints "More: run with --before SEQ" when the answer says there is more.
- MCP `recent_activity` gains the same optional arguments, as one API request like every tool.
- The `cli`, `api` and `keyboard-shortcuts` help articles and the activity section of the help get their lines (tests enforce it).

## Constraints

- Reads through `Deps.R`; nothing new writes the tracker.
- Theme tokens only; no inline script or `style=`; behaviour in `app.js`.
- `body_before`/`body_after` never leave the server through the API (`apiPayload` as today).
- Workflow-off output of other pages is untouched (`TestWorkflowOffUnchanged`).

## Left to the builder

- Exact names of routes' query parameters beyond those above, the Go filter struct and the query file.
- The before → after rendering per event kind (reuse what ticket history and `EventSentence` already know).
- The Activity tab's icon.
- Whether the "N new events" note counts exactly or says "New events".
