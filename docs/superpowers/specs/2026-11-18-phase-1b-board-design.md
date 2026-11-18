# Phase 1b design: the board

Date: 2026-11-18. Extends `2026-11-18-vaquero-design.md` (Phase 1, second half). Status: approved in conversation (layout chosen from mockups).

## Intent

Phase 1a made vaquero work through plain forms. Phase 1b makes it pleasant to use every day: a board you can drag cards across, a filtered list, search, live updates when anything changes, and keyboard shortcuts. It also shows, at a glance, what is ready for someone (or, from Phase 2, an agent) to pick up.

## Decided by the user

- **Layout B with C's milestone panel**, from the mockups in `.superpowers/brainstorm/`:
  - full columns for the active categories (todo, in progress, in review, done), one per status in board order;
  - backlog and cancelled statuses folded into slim rails that open on click;
  - filter chips in the header;
  - a milestone panel on the right: progress per milestone (done of total, cancelled excluded), plus a "No milestone" count, each clickable to filter the board.
- "Looks great for now, we can always tweak it later."

## Builder's calls (proposed in conversation, not objected to)

- **Done column** shows tickets that reached a done status in the last 7 days, with an "N older" link to the list view filtered to done. This needs a new `status_changed_at` column, set by core on create and on every status change.
- **Filters** live in the URL query, so a board view can be bookmarked; the list view takes the same filters. Chips: Mine (assigned to the owner), Agents (assigned to an agent actor), tag, priority, milestone (including "none"), assignee.
- **Ready** (the rust edge): a todo-category ticket, unassigned, with no open blocker (no "blocks" link from a ticket that isn't done or cancelled) and no open sub-tickets. This is the ready queue from the main spec, minus claims, which arrive in Phase 2 and will join the same definition.
- **Drag** uses SortableJS 1.15.6, vendored (starfish ships the same file under the same CSP). Dropping a card posts to a core operation, `MoveTicket`, which sets status and position together and writes one event. Without JavaScript, the ticket page's status field still works.
- **Live updates**: a server-sent event stream announces the newest event sequence number; the board, list and activity pages refresh their content when it advances, so changes from another tab, the CLI or an agent show up within about a second. A refresh waits while a drag is in progress.
- **Search**: SQLite FTS5 over ticket titles, descriptions and comments (prefix matching, accents ignored), at `/search`, with a search box in the header. Typed text is turned into quoted prefix terms, so no input can be a syntax error.
- **Keyboard shortcuts**: `c` new ticket, `/` search, `g b` board, `g l` list, `j`/`k` move between cards or rows, `enter` open, `?` the shortcut list, `esc` close it. Ignored while typing in a field.
- The plain grouped page from Phase 1a becomes the list view at `/p/{key}/list`; `/p/{key}` is the board.

## Constraints carried forward

All Phase 1a invariants: only core writes the tracker, one event per change, no inline script or style, theme tokens only, generated files committed, the gate before every commit.

## Not in Phase 1b

Agent claims and their countdowns on cards (Phase 2), saved views, swimlanes, bulk edit, customisable shortcuts.
