# Run T: the sidebar shell, the new look and the inbox — implementation plan (VAQ-171, VAQ-172)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every page in the tight, sharp look under a left sidebar, and one inbox page where the owner answers every open question and approval.

**Architecture:** The look is theme tokens in `input.css` plus the class recipes in `templates/helpers.go`, so most pages change without touching their templates. The sidebar is part of `Shell`, fed through the request context the way faces are (a lazy loader set by a router-wide middleware, plus a place read from the path). The inbox is a page over existing reads and core's existing answer, withdraw and overrule calls; the one new write is a decision's `recommended` option.

**Tech Stack:** Go 1.27, templ, htmx, Tailwind v4 (`@theme`), app.js, sqlc and goose, chromedp e2e.

**Spec:** `docs/superpowers/specs/2026-11-27-owner-seat-design.md` (sections Look and density, The shell, The inbox). This is the direction reset's run A, owner's-seat half; its factory half (the `plan` and `audit` prompts) shipped in runs R and S. It supersedes `docs/superpowers/plans/2026-11-26-agent-workflow-part-5.md` Task 1 (the decision queue): the inbox replaces `/decisions`.

## Rulings (controller, 2026-11-27; the owner can overrule)

- **T-R1: items that don't exist yet stay out of the sidebar.** Live, Views, Runs and Docs appear when their pages land (runs B to D). Until Live exists, the sidebar keeps an **Activity** item after My work, and the inbox's empty state is "Nothing needs you." with no link.
- **T-R2: the current place comes from the path.** `/p/{key}/…` marks that project and its view; a ticket page and settings mark the project (from the handler) and app.js marks its last view from `localStorage` (`vq:last-view:{KEY}`, written on Focus, Board and List); without JavaScript the project's Focus is marked.
- **T-R3: My work** is `/my`: the owner's open (not done, not cancelled) assigned tickets across non-archived projects, as list rows grouped by project, priority then updated order. No filters this run.
- **T-R4: `recommended`** is a decision's chosen option label, matched to an option in any case and stored as that option's label; "" means none. The CLI's `--recommend LABEL` sets it (it took free text before) and `--recommend-why TEXT` the free-text `recommendation`; the API and MCP take `recommended` and `recommendation`. A ruling's `recommended` is refused ("A ruling records its answer; it has no recommendation.").
- **T-R5: Review** in the inbox is every ticket in an in-review status of a non-archived project with the workflow on, oldest `status_changed_at` first. **Check** is `ticket.claim_lapsed` events of the last 24 hours whose ticket is not done or cancelled; Dismiss hides one in this browser (`localStorage` `vq:inbox-dismissed`, event seqs, pruned past 24 h). **Rulings agents made** are rulings asked by an agent in the last 7 days and not overruled (`answered_by` = `asked_by`).
- **T-R6: the sidebar count** is open questions and approvals in non-archived projects plus the Review count, read in one query per page, only when the Shell renders.

## Global Constraints

- Gate before every commit touching Go, SQL, templ or CSS: `make gen && make fmt && make vet && make lint && make test`, plus `make e2e` (every task changes pages).
- Commits `{Area}: {summary} (VAQ-171)` or `(VAQ-172)`, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- No inline script, no `style=`, theme tokens only (`TestNoRawColoursInTemplates`), Lucide through `ui.json`, no hand-written `<svg>`.
- Only core writes; the inbox adds no write besides `recommended` on ask.
- `TestWorkflowOffUnchanged` holds (no change to ready, ticket or board JSON).
- e2e at 375, 768 and 1280 px, light and dark; no page wider than 375 px at 375 px.
- Exact copy: "Nothing needs you.", "Recommended", "Answered directly by you", "Relayed by {agent}: \"{words}\"", "Covers {KEY} (plan rev {n}) and {n} sub-tickets as asked" ("1 sub-ticket"; "(no plan)" when the revision is 0; the clause after "and" left out with none), "Answered today ({n})", "Show plan", "Add a note", "Withdraw", "Overrule", "Dismiss".
- Sizes from the spec: text 12/13/15/18 px; rows 26 px (sidebar), 28 px (tickets); chips and small buttons 24 px; top bar 38 px; spacing on 4 px; radius 4 px (6 px dialogs and tiles).

## Review Focus

1. **Twenty or more projects, and long names:** the sidebar scrolls on its own, the current project is scrolled into view, names cut with an ellipsis, archived projects never listed. Test in Task 2.
2. **An inbox answer that core refuses** (the plan changed since asked, the project archived meanwhile, already answered in another tab): the card stays, shows core's sentence under its buttons, and the page doesn't error; the second tab's card then reads "This decision is already answered." Test in Task 4.
3. **A `recommended` label that isn't an option, or differs only in case:** 422 naming `recommended` ("Recommend one of the options.") for a miss; for a case difference the option's own spelling is stored. Test in Task 3.
4. **Phone width:** the drawer opens from the menu button, closes on navigation, Escape and the backdrop, traps nothing, and the page beneath doesn't scroll sideways. Test in Task 2.
5. **Keys inside the inbox's note box:** typing `1` or `j` in a note types it and answers nothing. Test in Task 4.

---

### Task 1: The look: tokens, palette and the class recipes

**Files:**
- Modify: `internal/web/static/css/input.css` (`@theme` and the dark block), `internal/web/templates/helpers.go` (class recipes), `internal/web/templates/components.templ` (field height), CLAUDE.md's ui-theming line if a token name changes (it doesn't), `.claude/rules/ui-theming.md` (the size tokens)
- Test: `internal/web/templates/theme_test.go` (new), existing e2e

**Interfaces:**
- Produces: Tailwind tokens `--text-xs: 12px`, `--text-sm: 13px`, `--text-base: 15px`, `--text-lg: 18px` (each with its line height: 16, 18, 22, 24 px); `--spacing-row: 28px`, `--spacing-nav: 26px`, `--spacing-chip: 24px`, `--spacing-bar: 38px` (so `h-row`, `h-nav`, `h-chip`, `h-bar` exist); `--radius-sm: 4px`, `--radius-md: 6px`. Recipes in helpers.go: `rowClass` (a 28 px ticket row: flex, gap-2, `border-b border-line`, truncating title), `chipClass` (24 px), and existing recipes (`btnPrimary`, `btnSecondary`, `fieldClass`, `panelClass`, `h1Class`, `h2Class`, `h2IconClass`, `labelClass`) retuned to the scale.
- Palette starting values (light): ink `oklch(0.2 0.01 60)`, paper `oklch(0.99 0.002 85)`, paper-alt `oklch(0.965 0.003 85)`, line `oklch(0.91 0.004 85)`, muted `oklch(0.52 0.01 70)`; dark: ink `oklch(0.93 0.005 80)`, paper `oklch(0.17 0.004 60)`, paper-alt `oklch(0.21 0.005 60)`, line `oklch(0.29 0.006 60)`, muted `oklch(0.66 0.01 70)`. Accent, danger, warn and the tile pairs unchanged.

- [ ] **Step 1: Write the failing test** `TestThemeTokens` (reads input.css): each size token above is present with its value; the light and dark `paper`, `paper-alt` and `line` have chroma ≤ 0.006; `muted` on `paper` and `ink` on `paper-alt` reach 4.5:1 contrast in both schemes (convert oklch → sRGB in the test; a small helper). `TestNoAdHocSizes`: no `.templ` file uses `text-[10px]`, `text-[11px]` or `text-[13px]` (use the tokens).
- [ ] **Step 2:** `go test ./internal/web/templates -run 'TestThemeTokens|TestNoAdHocSizes'` → FAIL.
- [ ] **Step 3:** set the tokens and palette; retune the recipes; replace the ad hoc `text-[10px]` sizes in templates with `text-xs`. Focus lanes and the list become rows (`rowClass`): priority glyph, key (the number alone inside its project), title cut with an ellipsis, trailing meta (claim or assignee face, `When`); a board card keeps its title line plus one meta line (claim or agent, priority, tags). The `cardItem` markup stays shared, `data-card` kept, so drags and `j`/`k` work unchanged.
- [ ] **Step 4:** tests → PASS; `make e2e` → PASS (fix selectors that relied on old classes only).
- [ ] **Step 5:** Gate; commit `Web: the tight look: type, size and palette tokens, rows for tickets (VAQ-171)`.

### Task 2: The sidebar shell and My work

**Files:**
- Create: `internal/web/templates/sidebar.templ`, `internal/web/templates/nav.go` (context types), `internal/web/handlers/nav.go` (middleware), `internal/web/handlers/mywork.go`, `internal/web/templates/mywork.templ`
- Modify: `internal/web/templates/layout.templ` (`Shell`: sidebar plus a main column with the 38 px top bar slot; the old header goes), `internal/web/router.go` (`handlers.Nav` router-wide after `Faces`; `GET /my`), `internal/web/templates/board.templ` (`projectHeader` becomes the top bar: project name, view tabs, New ticket), `internal/web/templates/tickets.templ` (`TicketPage` sets the place's project), `internal/web/static/js/app.js` (drawer, `⌘K`/`Ctrl+K`, `g i`, last view), `internal/db/queries/projects.sql` (`ListNavProjects`), `internal/db/queries/tickets.sql` or `board.sql` (`ListMyWork`), `internal/web/icons/ui.json` (inbox, my-work, menu), help `keyboard-shortcuts.md` and `getting-started.md`
- Test: `internal/web/handlers/nav_test.go`, `internal/web/handlers/mywork_test.go`, `internal/e2e/shell_test.go`

**Interfaces:**
- Produces (templates/nav.go):
  - `type NavProject struct{ Key, Name string; Icon ProjectIconView }`
  - `type Nav struct{ Projects []NavProject; InboxCount int }`
  - `type Place struct{ Project, View string }` (View: `focus`, `board`, `list`, `settings`, `ticket`, `inbox`, `my`, `activity`, `search`, `help`, `profile`, or "")
  - `func WithNav(ctx context.Context, place Place, load func() (Nav, error)) context.Context` (the loader runs at most once, on first use, as `WithFaces`); `func NavOf(ctx) Nav` (a failed load logs once and renders an empty sidebar list, never an error page); `func PlaceOf(ctx) Place`; `func SetPlaceProject(ctx, key string)` (the place is a pointer in the context, so a handler can name the ticket's project).
  - `handlers.Nav(d Deps) func(http.Handler) http.Handler`: `Place` from the path (`/p/{key}` → focus, `/p/{key}/board|list|settings` → that view, upper-cased key; `/inbox`, `/my`, `/activity`, `/search`, `/help…`, `/profile`); the loader reads `ListNavProjects` (non-archived, by name) and `inboxCount(ctx, d)` (Task 4 fills it; this task returns 0).
- Sidebar markup: `<nav id="vq-sidebar" aria-label="Main" data-sidebar>`: the logo and "Vaquero" linking `/`; the search form (`/search`, input `#vq-search`, placeholder "Search", a `⌘K` hint); Inbox (count badge when > 0), My work, Activity (T-R1); "Projects" group: one row per project (icon `sm`, name), the current one with `aria-current="page"` and its views (Focus, Board, List) under it, the current view marked; "All projects" linking `/`; the foot: avatar to `/profile`, Help, Admin, the keyboard-shortcuts button. Current item: `bg-paper`, a 2 px accent bar (`shadow-[inset_2px_0_0_var(--color-accent)]` via a recipe), `font-semibold`. Rows `h-nav`. The sidebar is `w-52`, full height, scrolls on its own (`overflow-y-auto`), `bg-paper-alt`, `border-r border-line`.
- Below `md`: the sidebar is hidden; the top bar shows a menu button (`aria-label="Menu"`, `aria-controls="vq-sidebar"`, `aria-expanded`) that app.js uses to open it as a drawer over a backdrop (`data-open` on the sidebar); it closes on a link click, Escape and the backdrop. Without JavaScript the menu button is a link to `/`.
- Keys (app.js, ignored in fields as today): `⌘K`/`Ctrl+K` focuses the search box (also inside fields); `g i` goes to `/inbox`; `/` still focuses search.

- [ ] **Step 1: Write the failing tests**:
  - `TestNavPlaceFromPath`: table of paths → `Place` (`/p/vaq/board` → `{VAQ, board}`; `/t/VAQ-1` → `{"", ticket}` before the handler, `{VAQ, ticket}` after `TicketPage` renders; `/inbox` → `{"", inbox}`).
  - `TestSidebarListsProjects`: two projects and an archived one → two rows, archived absent; the current project has `aria-current="page"` and its three views.
  - `TestSidebarOneQuery`: rendering a page reads `ListNavProjects` once (a counting reader, as `TestProjectsStatsOneQuery`).
  - `TestSidebarLoadFailure`: a loader error renders the page with an empty project list and logs once.
  - `TestMyWork`: the owner's open assigned tickets across two projects, grouped, done and cancelled and other assignees left out, archived projects left out; none → "Nothing assigned to you."
  - e2e `TestShell`: at 1280 px the sidebar is visible and the current view marked, light and dark; 20 projects with a 60-character name: the sidebar scrolls, the current project is in view, no text overflows (Review Focus 1); at 375 px the menu opens the drawer, a link closes it, Escape closes it, the backdrop closes it, and the page is no wider than 375 px (Review Focus 4); `⌘K` focuses search; `g i` opens `/inbox` (a 404 until Task 4; assert the URL).
- [ ] **Step 2:** → FAIL.
- [ ] **Step 3:** implement as above. `Shell(title)` keeps its signature. `AdminShell` sits inside it unchanged. The footer moves to the bottom of the main column. `main` drops `max-w-7xl`'s centring in favour of `px-6 py-6` within the column (Focus and the board use the width).
- [ ] **Step 4:** → PASS; `make e2e` (update tests that clicked the old header: Projects, Activity, Profile, Help, Admin links now live in the sidebar).
- [ ] **Step 5:** Gate; commit `Web: the left sidebar shell and My work (VAQ-171)`.

### Task 3: A decision's recommended option

**Files:**
- Create: `internal/db/migrations/0015_recommended.sql` (`ALTER TABLE decisions ADD COLUMN recommended TEXT NOT NULL DEFAULT '' CHECK (length(recommended) <= 200)`; plus `CREATE INDEX events_claim_lapsed ON events (at) WHERE kind = 'ticket.claim_lapsed'` for Task 4)
- Modify: `internal/core/decisions.go` (`DecisionInput.Recommended`, checked against `Options` as T-R4; `RecordRuling` refuses it), `internal/db/queries/decisions.sql` (insert), `internal/api/types.go` (`Decision.Recommended string \`json:"recommended"\``), `internal/web/handlers/api_decisions.go` (request and answer), `internal/mcpserver` (`ask_decision` schema: `recommended` "the label of the option you recommend"), `cmd/vaquero/cli_decisions.go` (`--recommend LABEL`, `--recommend-why TEXT`; `decision show` prints "Recommended: LABEL"), `internal/workflow/prompts/plan.md` (ask with `recommended` set to the option's label and `recommendation` saying why), `internal/core/coreseed` (one decision with `recommended`), help `api.md`, `cli.md`, `mcp.md`
- Test: `internal/core/decisions_test.go`, `internal/web/handlers/api_decisions_test.go` (`TestDecisionAnswerGolden` updated by hand), `cmd/vaquero/cli_decisions_test.go`, `make fixture`-free: the 0.1.9 fixture migrates (`TestFixturesMigrateToHead` with `recommended` = '' on old rows)

**Interfaces:**
- Produces: `gen.Decision.Recommended string`; `api.Decision.Recommended`; MCP and CLI as above.

- [ ] **Step 1: Write the failing tests**: `TestAskRecommended` (options A, B; recommended "b" → stored "B"; "C" → `*Invalid{"recommended": "Recommend one of the options."}`; "" stored ""); `TestRulingRefusesRecommended`; API round-trip shows `recommended`; CLI `decision ask … --option "A: x" --option "B: y" --recommend B --recommend-why "cheaper"` then `decision show` prints `Recommended: B` and the why; MCP `ask_decision` with `recommended` stores it; `TestWorkflowTextsNameOnlyWhatExists` still passes with the prompt's new wording.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS (`go test ./...`).
- [ ] **Step 5:** Gate; commit `Decisions: a recommended option, set by agents when they ask (VAQ-172)`.

### Task 4: The inbox

**Files:**
- Create: `internal/web/handlers/inbox.go`, `internal/web/templates/inbox.templ`, `internal/help/articles/inbox.md`
- Modify: `internal/db/queries/decisions.sql` (`ListInboxDecisions`: open questions and approvals in non-archived projects, `asked_at` ascending, optional project and kind; `ListAgentRulings` since an instant; `ListAnsweredSince`; `CountInbox`), `internal/db/queries/board.sql` or `tickets.sql` (`ListInboxReview`, `ListRecentLapses` using `events_claim_lapsed`), `internal/web/handlers/api_decisions.go` (`decisionsAsked` reused, unchanged), `internal/web/handlers/nav.go` (`inboxCount` from `CountInbox`, T-R6), `internal/web/router.go` (`GET /inbox`, `GET /decisions/{id}` → 302 `/inbox#decision-{id}`, `POST /inbox/{id}/answer`, `POST /inbox/{id}/withdraw`), `internal/web/static/js/app.js` (inbox keys, Dismiss), `cmd/vaquero/cli_decisions.go` (`decision show` ends with "Answer in the web: {server URL}/decisions/{id}" for an open question or approval), help `keyboard-shortcuts.md`, `agent-workflow.md` (answering in the inbox)
- Test: `internal/web/handlers/inbox_test.go`, `internal/e2e/inbox_test.go`

**Interfaces:**
- Consumes: `decisionsAsked(ctx, d, ids)` (api_decisions.go), `core.AnswerDecision`, `core.WithdrawDecision`, `gen.Decision.Recommended` (Task 3), `templates.Nav.InboxCount` (Task 2), `markdown.Render`.
- Produces: `templates.DecisionCard(c DecisionCardView)` (also used by Task 5) with `type DecisionCardView struct{ ID, Kind, Subject, ProjectKey, AskedBy, Age string; Question template.HTML; Options []CardOption; Covers, PlanKey string; Tickets []TicketRef; Error, Answered, Back string }` and `type CardOption struct{ Label, Tradeoff string; Recommended bool }`; `func decisionCard(ctx, d, dec gen.Decision, asked []api.DecisionAsked, back string) (templates.DecisionCardView, error)`.
- The page (`/inbox`, `?project=KEY&kind=approval|question`, chips All, each project with open items, Approvals, Questions): groups as T-R5, each with its count, empty groups left out; Decide's cards as the spec: kind · project · asker · age; the question (Markdown); option buttons, the recommended first with a "Recommended" mark and each trade-off under its label; tickets linked; an approval's covers line and Show plan (`<details>` loading `/t/{KEY}/plan` fragment with `hx-trigger="toggle once"`, or the plan body inline: builder's choice within the ticket page's plan rendering); Add a note (`<details>` with a `name="note"` box) and Withdraw (`data-confirm`). Review and Check as rows; Rulings agents made folded (`<details>` per ruling, opening to its card with Overrule, whose buttons post the same answer route). "Answered today (n)" (UTC day) folded at the bottom with "Answered directly by you" or "Relayed by {agent}: \"{words}\"". `#inbox` is `data-live` with `hx-trigger="vq:change"`.
- Answer: each option button posts `option` (and the card's note) with htmx to `/inbox/{id}/answer`, `hx-target` the card, `hx-swap="outerHTML"`; success answers an empty card (the item leaves) and `HX-Trigger: vq:change`; a core refusal answers the card again with `Error` set to core's sentence (Review Focus 2); without htmx, a redirect to `/inbox` with a flash. Withdraw the same way.
- Keys (app.js, only on `[data-inbox]`, ignored in fields: Review Focus 5): `j`/`k` move between `[data-card]` (the cards carry it, so the existing handler moves them); `1`–`9` click the focused card's nth `[data-option]`; `n` opens and focuses its note; `o` follows its `[data-ticket-link]`. After a card leaves, focus goes to the next card.

- [ ] **Step 1: Write the failing tests**:
  - `TestInboxGroups`: one each of question, plan approval, review ticket (workflow on), review ticket (workflow off: absent), lapse 2 h ago, lapse 30 h ago (absent), agent ruling 2 days ago, agent ruling 9 days ago (absent), overruled ruling (absent), archived project's question (absent) → Decide 2 oldest first, Review 1, Check 1, Rulings 1.
  - `TestInboxCardRecommended`: recommended option first with "Recommended"; none → no mark, the asked order.
  - `TestInboxCovers`: a plan approval of an epic with plan rev 2 and 3 sub-tickets → "Covers VAQ-1 (plan rev 2) and 3 sub-tickets as asked"; one → "1 sub-ticket"; none → "Covers VAQ-1 (plan rev 2) as asked".
  - `TestInboxAnswer`: posting Approve answers through core as the owner, `direct`, the response is empty with `HX-Trigger`; `TestInboxAnswerRefused`: a plan changed since asked → the card with "The plan or requirement for VAQ-1 changed since this was asked; ask again." and the decision still open; answered twice → "This decision is already answered." (Review Focus 2).
  - `TestInboxFilters`: `?project=` and `?kind=` narrow; unknown kind → all.
  - `TestInboxEmpty`: "Nothing needs you."
  - `TestInboxCount`: two open decisions and one review ticket → the sidebar badge "3"; none → no badge; one query (`CountInbox`).
  - `TestDecisionLinkRedirects`: `/decisions/{id}` → 302 `/inbox#decision-{id}`.
  - CLI: `decision show` of an open question ends with the web line.
  - e2e `TestInbox`: answer by click; answer by `j`, `1`; typing "1j" in a note answers nothing (Review Focus 5); refusal shows on the card; two tabs, the second refused; Dismiss hides a lapse and it stays hidden on reload; 375 and 1280 px, light and dark.
- [ ] **Step 2:** → FAIL. **Step 3:** implement. **Step 4:** → PASS; `make e2e`.
- [ ] **Step 5:** Gate; commit `Web: the inbox, what needs the owner answered in place (VAQ-172)`.

### Task 5: The ticket page's own decisions, and the wrap-up

**Files:**
- Modify: `internal/web/handlers/tickets.go` and `approvals.go` (the page lists every open decision naming the ticket, as `DecisionCard`s with `Back` the ticket, above Comments; the readiness block keeps Ready or its reason and Ask / Ask again, and loses its own answer form), `internal/web/templates/tickets.templ`, `CLAUDE.md` (package map: `nav.go`, the sidebar, `/my`, `/inbox`, `/decisions/{id}`, `CountInbox`, the `recommended` field, `events_claim_lapsed`, migration 0015; the T rulings under the agent workflow invariants), `internal/help/articles/getting-started.md`
- Test: `internal/web/handlers/approvals_test.go` (updated), `internal/e2e/inbox_test.go` (the ticket page's card)

- [ ] **Step 1: Write the failing tests**: `TestTicketPageDecisionCards`: a ticket named by an open question and an open plan approval shows two cards with the inbox's controls; answering one from the ticket page (`Back` = `/t/KEY`) refreshes the page part and leaves the readiness block current (VAQ-210's live rule: the cards and readiness are one `data-live` part); a ticket with none shows no card section. e2e: answer on the ticket page, at 375 and 1280 px.
- [ ] **Step 2:** → FAIL. **Step 3:** implement, reusing Task 4's `decisionCard`. **Step 4:** → PASS; `make ci` (coverage floors).
- [ ] **Step 5:** commit `Web: the ticket page answers its own decisions as the inbox does (VAQ-172)`; move VAQ-171 and VAQ-172 to Done when the run merges.
