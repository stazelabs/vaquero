# The owner's seat: shell, look and density, ticket page, inbox, live view (VAQ-170)

Owner design session, 2026-11-27, with browser mockups on real VAQ data (`.superpowers/brainstorm/`, not committed). Direction: `docs/superpowers/specs/2026-11-26-direction-reset.md`, decision 10. This document gates VAQ-171 to VAQ-176 and decides what VAQ-6 keeps.

## Intent

The owner's notes on VAQ-170: the UI is slow to operate, agent chatter buries the signal, the ticket page reads like a record form, and the look should be tighter and sharper. The web becomes the place the owner runs the factory from: a day's work (approve, triage, edit, check progress) needs no CLI (VAQ-169).

## Goals

- One shell across every page, so the current place is always marked and switching project is one click.
- More on screen at a desk without feeling cramped: rows, not cards, wherever tickets are listed.
- The ticket page is a document you edit where you read it.
- What needs the owner is in one place and answered there.
- One screen shows the factory working, readable from across a room.

## Non-goals

- The command palette, quick create, saved views' editor and bulk select (VAQ-177 to VAQ-179): they live inside this shell but are designed with their own tickets. The sidebar leaves room for them (the ⌘K box, the Views group).
- Settings in one pattern (VAQ-180).
- A brand palette: the hues stay the placeholder ones.

## Anti-goals

- A page that exists only to show what was recorded (direction reset).
- Agent progress mixed into people's comments.
- A rule or a count the owner can't predict from what the page shows.
- Any stored metric samples for the live view.

## Approaches (owner's picks, 2026-11-27)

- **Look:** A, today's warm look tidied (14 px, cards), or **B, tight and sharp** (13 px, rows, near-neutral paper). **B chosen.**
- **Ticket page:** A, document plus a properties column, or **B, full-width document with a field bar under the title, and tabs**. **B chosen**, with labels: inside the chip, **a small label above each chip**, or a label and value grid. **Labels above chosen** ("easier to read").
- **Editor (VAQ-6):** **CodeMirror 6 vendored**, image in place while typing, or a plain text box that shows a pasted image as a thumbnail under the box until saved. **CodeMirror chosen:** the owner's test is that pasting "just works" in the box itself.
- **Inbox:** A, list and detail side by side, or **B, one column of answerable cards**. **B chosen** ("easier to read"); keys give it A's speed (controller's call).
- **Live view:** **A, flow counts on top, then working now, stream and waiting on you**, or B, a tile per agent as the headline. **A chosen.**

## Look and density (VAQ-171)

- **Palette:** near-neutral paper, page and alternate surface (the tokens keep their names: `paper`, `paper-alt`, `line`, `ink`, `muted`), with chroma at most about 0.004. The accent (rust) marks only the current place, counts, the focus ring, the status chip and the main button. `warn` and `danger` keep their meanings. Dark mode is the same at lower lightness. Values are set in `input.css`; the tile pairs are unchanged.
- **Tokens (controller's call), added to `@theme` so templates never set sizes ad hoc:**
  - Type: `text-xs` 12 px (labels, meta), `text-sm` 13 px (body), `text-base` 15 px (section titles, a card's question), `text-lg` 18 px (page titles). Ticket keys, counts and code in `font-mono`.
  - Heights: 26 px sidebar rows, 28 px ticket rows, 24 px chips and small buttons, 38 px top bar.
  - Spacing on a 4 px step; radius 4 px (6 px for an agent tile or dialog).
  - Rules are 1 px `line` hairlines; surfaces are separated by rules, not shadows.
- **Rows, not cards:** Focus lanes, the list, the inbox's folded items and the live stream list tickets as one 28 px line: priority mark, key (number only inside its project), title cut with an ellipsis, a trailing meta (agent, age). Board cards keep a second line only for the agent or claim. A run's tickets group under one line ("Run S · claude · 7 tickets") in Focus's In flight once runs exist (VAQ-137, run C); until then In flight groups by status as today (controller's call: no interim rule for finding runs).

## The shell (VAQ-171)

```
┌──────────────┬───────────────────────────────────────────┐
│ ▣ Vaquero    │ Vaquero        Focus  Board  List   New c │
│ [Search… ⌘K] ├───────────────────────────────────────────┤
│ Inbox      1 │                                           │
│ My work      │                                           │
│ Live         │                                           │
│ VIEWS        │                                           │
│  Audit finds │                                           │
│ PROJECTS     │                                           │
│ VQ Vaquero   │                                           │
│   ▸Focus     │                                           │
│    Board     │                                           │
│    List      │                                           │
│    Runs      │                                           │
│    Docs      │                                           │
│ OT Other     │                                           │
│ ── You · Help · Admin                                    │
└──────────────┴───────────────────────────────────────────┘
```

- The left sidebar replaces the top bar on every page except `/admin` (which keeps `AdminShell`, reached from the foot).
- **Order (direction reset; Live is the controller's call):** the search box (`⌘K` opens it; it is today's search until VAQ-177's palette), Inbox with its count, My work, Live, the Views group, then Projects, then the foot: the owner's avatar linking `/profile`, Help, Admin.
- **Projects:** each non-archived project is one row (icon and name). The current project expands to Focus, Board, List, Runs and Docs. Runs and Docs show only once their pages exist (VAQ-137, VAQ-175 and the knowledge work); until then the project expands to Focus, Board and List. Clicking a project opens its Focus. Archived projects are reached from the projects page, linked as "All projects" under the list.
- **Current place:** the current item has the `paper` background, a 2 px accent bar on its left and weight 600. A ticket page and project settings mark the project's last view (Focus by default) and keep the project expanded.
- **The top bar** of the main area is 38 px: the project name (or the page name), the project's view tabs where they apply, and the main action (New ticket, `c`).
- **Views** and **My work** are VAQ-178's. Until it lands the Views group is absent and My work is the owner's open assigned tickets across projects (controller's call: a filtered list, no new data).
- **Phone width (below `md`):** the sidebar is a drawer behind a menu button in the top bar; it closes on navigation, on Escape and on the backdrop; the top bar keeps the menu button, the page name and New.
- The keyboard shortcuts keep working; `g i` goes to the Inbox and `g v` to Live (controller's call).

## The ticket page (VAQ-173, VAQ-174, VAQ-175, VAQ-182)

```
Vaquero / VAQ-214 / VAQ-210                                        …
Refresh the ticket page's readiness when its live plan changes
STATUS        PRIORITY  ASSIGNEE       PARENT    MILESTONE         TAGS      TIER
[In progress▾] [High]   [● claude 12m] [VAQ-214] [2 Seat and loop] [audit-qa] [standard]
┌ Plan ─────────┬ Branch ───────────┬ Merged ──┬ Released ┐
│ rev 1 approved│ run-s · 2 commits │ not yet  │ —        │
└───────────────┴───────────────────┴──────────┴──────────┘
 Description | Plan · rev 1 | References 1 | Links 1
 (text, edited in place) … sub-tickets … attachments
 [open decision cards, as in the inbox]
 Comments 2 | Work log 6 | History 9
 ● claude, 2m: "e2e green, opening for review"        Work log →
 comments…
 [Add a comment…]
```

- **No edit page.** `/t/{ticket}/edit` and the Edit button go once every field is editable in place; `/t/{ticket}/edit` then redirects to the ticket. The new-ticket form stays (VAQ-177 replaces it with quick create).
- **Title:** click (or `e` then Shift+Tab, left to the builder) edits it in place, as VAQ-6 specifies.
- **The field bar** sits under the title, full width: each field is a small uppercase label (`text-xs`, `muted`) above a 24 px chip holding the value. Order: Status, Priority, Assignee, Parent, Milestone, Tags, Tier (Tier and the model only with the workflow on). Each chip opens a menu (status, priority, milestone, assignee) or a type-ahead (parent, tags); choosing saves at once with the ticket's version. An unset field is a dashed chip "+ Milestone". Only the status chip is drawn in the accent. The bar wraps at narrow widths, labels kept.
- **Keys on the ticket page (VAQ-173, plus `p` and `m`):** `s` status, `p` priority, `a` assignee, `m` milestone, `l` tags, `e` the description. Each opens the same menu or editor the click does.
- **A stale save** (the ticket changed since the page loaded) is refused as today; the chip shows the error toast "This ticket changed since you opened it." and the field bar refreshes to the current values (the change is shown, not overwritten).
- **The "…" menu** right of the title: Copy link, Move to project (the existing confirm flow).
- **The lifecycle strip (VAQ-174)** under the field bar: one row of cells, each a small label above a short value, each linked to its detail: Plan ("rev 1 · approved", "rev 2 · not approved", or the readiness reason), Branch or run ("run-s · 2 commits"), Merged ("main ✓ · origin ✓", "not yet", or "unknown until fetched"), Released (the first tag). Readiness and its action (Ask, Ask again) sit in the Plan cell; with the workflow on and no plan, the cell reads "No plan" with the readiness reason. Commits, Merged and Released come from the ticket's commits (`Mentions`, as the Commits section reads today), so they need no run; the Branch-or-run cell joins in run C with VAQ-137 and is left out until then (controller's call: VAQ-174 builds the other cells in run B). Done stages are drawn in the `ok` colour, the current one on `paper-alt`, later ones `muted`. Git-backed cells load after the page (as the Commits section does today). With the workflow off only the git cells show; with no repo path and the workflow off there is no strip.
- **Document tabs:** Description (the default; the text, then sub-tickets as a short list, then attachments), Plan with its revision (read, Show changes, and edited in place with the stale-revision refusal, VAQ-175), References with the count (listed, added with kind, path or URL, anchor and label, removed behind the confirm dialog, VAQ-175), Links with the count. A tab with nothing is shown with no count and its empty state ("No plan yet." with Write a plan). The tab is in the address (`#plan`), so a link opens it.
- **Open decisions** on this ticket show between the document and the conversation, as the inbox's cards, with the same controls.
- **Conversation tabs:** Comments (the default, people and questions), Work log (agents' progress, VAQ-182: entries are their own record written by their own API route, MCP tool and CLI command, which the `work` prompt uses for progress; an agent's `add_comment` stays a comment, for questions and discussion; existing comments are not reclassified; VAQ-182's own design settles the schema; controller's call), History (today's events, Show changes kept). Each with its count. Above the comment list, the newest work log entry as one line, linking the Work log tab. The comment box is always at the bottom of Comments, the CodeMirror editor.
- Everything that is live today stays live (`data-live`); the field bar, the strip, the tabs' counts and the readiness cell follow `vq:change` too (VAQ-210's rule).

## The editor (VAQ-6)

VAQ-6's spec stands as written (`docs/superpowers/specs/2026-11-26-editor-design.md`): CodeMirror 6 vendored, attachment images in place while typing, uploads on the new-ticket form through pending attachments, ticket-key suggestions, click to edit, the CSP nonce on style elements. Its look takes this document's tokens. Its plan (`docs/superpowers/plans/2026-11-26-editor.md`) is re-cut against this ticket page: the title and description click-to-edit live on the new page, and the edit page's removal is VAQ-173's.

## The inbox (VAQ-172)

```
Inbox   [All] [VAQ] [Approvals] [Questions]
DECIDE · 3
┌──────────────────────────────────────────────────────┐
│ Plan approval · VAQ · claude · 2h                    │
│ Approve run S's plan: fix the seven High and …       │
│ Covers VAQ-214 (plan rev 1) and 7 sub-tickets as asked · Show plan │
│ [Approve] [Request changes]      Add a note · Withdraw│
└──────────────────────────────────────────────────────┘
REVIEW · 1     CHECK · 1     RULINGS AGENTS MADE · 2 (folded)
Answered today (3)
```

- `/inbox`, across non-archived projects, replacing VAQ-139's planned `/decisions` page; `/decisions/{id}` opens the inbox at that card (the CLI's hint links it).
- **One column of cards**, oldest first within each group:
  - **Decide:** open questions and approvals. A card shows its kind, project, asker and age; the question (Markdown); the options as buttons, the recommended one first and marked "Recommended" (controller's call: a decision gains a `recommended` field naming one of its options' labels, through core, the API, MCP's `ask_decision`, the CLI and export, with the `plan` prompt asking agents to set it; the free-text `recommendation` stays as the why; a decision without it shows no mark; built with VAQ-172), each option's trade-off under it; the tickets it names, linked. An approval says what approving covers in one line ("Covers VAQ-214 (plan rev 1) and 7 sub-tickets as asked") and Show plan opens the plan under the card. Add a note opens a note box; Withdraw (for questions and approvals the owner may withdraw) behind the confirm dialog.
  - **Review:** tickets in an in-review status in workflow-on projects, as rows (key, title, who moved it, age), linking the ticket. (Controller's call: "waiting for the owner" is every in-review ticket in a workflow-on project; with the workflow off a project's review tickets aren't listed.)
  - **Check:** claims that lapsed in the last 24 hours, as rows ("claude's claim on VAQ-203 lapsed 3m ago"), each with Dismiss, which hides it for this browser only (localStorage; no event, controller's call). Nothing to requeue: a lapse already returns the ticket.
  - **Rulings agents made:** folded to one line each (question and the ruling), opening to the card with Overrule, which offers the options. Rulings from the last 7 days.
- Answering is one click (plus the optional note), through core's existing calls, recorded as the owner's direct answer. An answered card leaves the list; a refusal (stale since asked, answered already) shows on the card, which stays (VAQ-139's Review Focus 1 and 2 carry over).
- **Keys:** `j`/`k` move between cards, `1`–`9` choose the focused card's option, `n` opens its note, `o` opens its ticket; after an answer focus moves to the next card.
- **Filters** in the address: `project`, `kind` (approvals, questions). Chips along the top.
- **Answered today (n)** at the bottom opens today's answered decisions, each with "Answered directly by you" or "Relayed by {agent}: \"{words}\"".
- **The sidebar count** is Decide plus Review, shown when above zero.
- **Empty:** "Nothing needs you." with a link to Live.
- Live: the list follows `vq:change`.

## The live view (VAQ-176)

```
Live   [All projects] [VAQ]                         Wall mode  w
  12        8          0               3              19
 Ready   In flight   Waiting on you  Finished today  This week
WORKING NOW            STREAM                        WAITING ON YOU
● claude · run S       now VAQ-210 claude · 6 log entries   Nothing waits on you.
 VAQ-210 … 12m         4m  VAQ-208 moved to In review       Inbox →
 last: … 40s ago       9m  VAQ-204 · 3 log entries · 2 commits
● claude · lapsed …
```

- `/live` (all projects) and `/p/{key}/live` (one project; a project tab, controller's call: it also shows in the project's expanded sidebar entry, after Docs).
- **Flow counts** across the top, large mono numerals: Ready (the board's ready rule plus readiness), In flight (in progress and in review), Waiting on you (the inbox count), Finished today, This week (done; "today" and "week" in UTC, as Focus's day labels; the week starts Monday). All read from current tickets and events; nothing stored.
- **Working now:** each live claim grouped by agent (name, run once VAQ-137 exists, the ticket, how long it has been claimed, the last action: the agent's newest event or work log entry, with its age). Flagged in `warn`: a claim lapsed in the last hour ("claim on VAQ-203 lapsed 3m ago") and a claim with no event for 30 minutes ("quiet for 34m", controller's call).
- **Stream:** recent events newest first, folded to one line per ticket per agent run of consecutive events ("claude · 6 log entries · 2 commits"); people's changes are one line each. Reorders stay out, as today.
- **Waiting on you:** the inbox count and the first five titles, linking the inbox.
- **Wall mode** (`w`, or `?wall=1`): no sidebar or controls, type scaled for three metres (counts about 64 px, rows 22 px), the four counts and Working now first, the stream below when there is room. Light and dark follow the system.
- **Activity becomes the stream's history:** `/activity` is the same stream unfolded and paged, filters in the address. VAQ-153's spec (`docs/superpowers/specs/2026-11-26-activity-page-design.md`) is an input re-cut under VAQ-176; VAQ-154's Factory view is replaced by this page.

## Constraints

- CLAUDE.md's invariants hold: no inline script or `style=`, theme tokens only, Lucide icons, `TestWorkflowOffUnchanged`, core-only writes, one event per change.
- Every page passes e2e at 375, 768 and 1280 px, light and dark.
- Help: `keyboard-shortcuts` lists every new key; a help article for the inbox and the live view; `getting-started` describes the sidebar.
- Copy (exact): "Nothing needs you.", "Nothing waits on you.", "Recommended", "Answered directly by you", "Relayed by {agent}: \"{words}\"", "Covers {KEY} (plan rev {n}) and {n} sub-tickets as asked", "unknown until fetched".

## Order of building

The direction reset's runs: A (VAQ-171 shell and look, VAQ-172 inbox), B (VAQ-173 ticket page, VAQ-174 strip, VAQ-175 plans and references, VAQ-6 editor, VAQ-182 work log), C (VAQ-176 live view, Activity). The look and the tokens land first, with the shell, so every later page is built in them.

## Left to the builder

- Exact oklch values for the near-neutral palette in light and dark, within the chroma and contrast the mockups showed (body text at least 4.5:1 on `paper`).
- Icon choices for sidebar items and stages, from Lucide through `ui.json`.
- How the type-ahead menus are built (a shared component for parent and tags).
- The drawer's animation, if any.
- Where the stream's folding window ends (a gap of time or another actor's event).
