# Icons and in-app help — Design and Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the UI visual focus with icons, and put vaquero's documentation inside the app (and in the CLI), starting with everything shipped so far.

**Spec:** this document (requested 2026-11-19: "let's not be afraid of incorporating icons into the UI — spiffy it up a bit, bring some visual focus; let's start incorporating documentation into the app as well — include CLI info, etc."), extending `docs/superpowers/specs/2026-11-18-vaquero-design.md`.

## Design (builder's calls)

**Icons.** Lucide (ISC), the set healthcheck-email uses, through the same pipeline: `internal/web/icons/ui.json` names each glyph vaquero uses (our name → Lucide name); `go run ./internal/web/icons/gen` (part of `make gen`) reads `node_modules/lucide-static` (pinned exactly in `package.json`) and writes `ui_gen.go`; templates draw them with `Icon(name)` (16 px), `IconLg(name)` (20 px) and `IconClass(name, class)`. Glyphs are `aria-hidden`, draw in `currentColor`, sit beside the text they belong to, and are never a link's only label (an icon-only button carries `aria-label`). No hand-drawn `<svg>` in templates. Lucide's licence joins the open source notices.

Where icons go:

| Place | Glyphs |
|---|---|
| Top bar | Projects `folder-kanban`, Activity `activity`, Admin `shield`, Help `circle-help`, search box `search`, shortcuts `keyboard` |
| Project header | Board `square-kanban`, List `list`, Settings `settings`, New ticket `plus` |
| Board column and rail headings, status badges, list sections | backlog `circle-dashed`, todo `circle`, in progress `circle-dot`, in review `eye`, done `circle-check`, cancelled `circle-x` |
| Cards and list rows | priority: urgent `octagon-alert`, high `signal-high`, medium `signal-medium`, low `signal-low`; ready `zap`; assignee `user` (people) or `bot` (agents) |
| Ticket page | section headings: description `align-left`, sub-tickets `list-tree`, links `link`, comments `message-square`, history `history`; side panel field labels; Edit `pencil` |
| Activity lines | by event kind: created `plus`, updated `pencil`, moved `arrow-right`, comment `message-square`, link `link`, project `folder`, token `key-round`, deleted `trash-2` |
| Empty states | a large muted glyph above the sentence (`inbox`, `search-x`, `folder-plus`) |
| Toasts | success `circle-check`, error `circle-alert` |
| Admin side list | Overview `gauge`, API tokens `key-round` (2b adds MCP activity `bot`) |
| Help | each section's glyph on the index; `book-open` for articles |

**In-app help.** An `internal/help` package embeds markdown articles (`internal/help/articles/*.md`) with front matter (`title`, `section`, `summary`, `order`), rendered with goldmark (tables, heading anchors, raw HTML escaped; articles are ours, so no sanitiser is needed beyond that). Sections, in order: Getting started, Using vaquero, Command line, API and agents.

- `/help`: the index, each section with its articles and summaries.
- `/help/{slug}`: an article, with a "Contents" list from its headings and links to the others in its section.
- `/help/{slug}.md`: the same article as `text/markdown`, for agents and scripts (the page links to it).
- The top bar's Help link; contextual "Help" links where a page has one: board → the board article, ticket edit conflict banner → editing and conflicts, admin tokens → API tokens, project settings → projects.
- `vaquero docs [TOPIC]` prints an article as markdown without needing a server (`vaquero docs` lists them). The CLI guide becomes the `cli` article; `docs/CLI.md` is removed and README points to `vaquero docs cli` and `/help/cli`.

First articles: `welcome` (what vaquero is, local mode, where data lives), `projects-and-tickets` (keys, statuses and categories, priorities, tags, sub-tickets, links and the loop rule, milestones, archiving, changing a key, deleting), `the-board` (columns and rails, the ready edge, filters, drag, live updates, the milestone panel), `editing-and-conflicts` (versions, the banner, exit 3), `search`, `keyboard-shortcuts`, `cli` (from docs/CLI.md), `api` (endpoints, errors, versions), `api-tokens`, `data-and-backups` (the data directory, the lock, backups before deletes, restoring by hand).

Tests keep docs honest: every article has the front matter and a known section; every `/help/…` link in an article resolves; the shortcuts article names every shortcut in `templates.shortcuts`; the CLI article names every command in `cliCommands`; the API article names every `/api/v1` route registered in the router.

## Global Constraints

CLAUDE.md invariants apply. Theme tokens only; icons in `currentColor`. Generated files committed (`ui_gen.go` joins them). `lucide-static` is pinned to an exact version. Copy rules apply to articles. Gate steps separately, by exit code; `make ci` ends each task. Commits `{Area}: {summary}` with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A template asks for an icon not in `ui.json`.** The build fails (the generated set has no such entry and a test walks every `Icon("…")` call), never a blank space at run time.
2. **An article links to a help page that doesn't exist.** A test fails.
3. **A new CLI command, shortcut or API route ships without documentation.** A test fails naming it.
4. **`/help/nope` or `/help/../etc`.** A themed 404, no file access outside the embedded set.
5. **Dark mode and narrow screens.** Icons follow the text colour in both themes; the top bar still fits at phone width (labels may hide, icons stay with `aria-label`s).

---

### Task 1: Icon pipeline

**Files:** `package.json` (`lucide-static` exact pin), `internal/web/icons/ui.json`, `ui.go`, `ui_gen.go` (generated), `gen/main.go` (ported from healthcheck-email, UI glyphs only), `ui_test.go`, `internal/web/templates/icon.templ` (`Icon`, `IconLg`, `IconClass`), `Makefile` (`icons` target in `gen`), `scripts/notices` + `internal/legal/vendor/lucide.txt`.

- [ ] **Tests first:** `TestEveryIconNameKnown` (walk `internal/web/templates/*.templ` for `Icon("x")`, `IconLg("x")`, `IconClass("x"` and `iconFor` tables; each name is in the generated set), `TestIconMarkup` (aria-hidden, `currentColor`, the size classes), `TestNoAdHocSVG` (no `<svg` in any `.templ` but `icon.templ`), Lucide in `legal.ThirdParty()`.
- [ ] Implement, gate, commit — `"Web: Lucide icons"`

### Task 2: Icons through the UI

**Files:** `layout.templ`, `board.templ`, `projects.templ`, `tickets.templ`, `activity.templ` (+ `activity.go` `EventIcon(kind)`), `admin.templ`, `search.templ`, `labels.go` (`CategoryIcon`, `PriorityIcon`), `input.css` if needed.

- [ ] **Tests first:** `TestTopBarIcons` (each nav link carries its glyph and keeps its text), `TestBoardIcons` (column headings carry category glyphs; an urgent card carries `octagon-alert`; an agent assignee carries `bot`), `TestEveryEventKindHasIcon` (over `core.EventKinds()`), `TestIconOnlyButtonsLabelled` (every `<button>` or `<a>` whose only child is an icon has `aria-label`).
- [ ] Implement per the table; check in Chrome in light and dark at desktop and phone width; gate; commit — `"Web: icons through the UI"`

### Task 3: Help package, pages and `vaquero docs`

**Files:** `internal/help/help.go` (+test), `internal/help/articles/*.md` (the ten above), `internal/web/handlers/help.go` (+test), `internal/web/templates/help.templ`, router, layout (Help link), `cmd/vaquero/docs.go` (+test), contextual links in `board.templ`, `tickets.templ`, `tokens.templ`/`admin.templ`, `projects.templ`; remove `docs/CLI.md`; README.

```go
package help
type Article struct {
	Slug, Title, Section, Summary string
	Order                         int
	HTML                          template.HTML
	Headings                      []Heading // h2 only, for Contents
	Markdown                      string    // as written, with title and summary on top
}
type Heading struct{ ID, Text string }
type Section struct {
	Key, Title, Icon string
	Articles         []Article
}
func Sections() []Section
func Get(slug string) (Article, bool)
```

- [ ] **Tests first:** `TestArticlesComplete` (front matter, known section, unique order within a section), `TestHelpLinksResolve`, `TestShortcutsDocumented`, `TestCLICommandsDocumented`, `TestAPIRoutesDocumented` (walk the router with `chi.Walk` for `/api/v1/` routes; each appears in `api.md`), handlers `TestHelpIndex`, `TestHelpArticle` (contents list, section links), `TestHelpMarkdown` (`/help/cli.md` is `text/markdown`), `TestHelpUnknown` (Review Focus 4), `TestContextualHelpLinks`, CLI `TestDocsCommand` (`vaquero docs` lists, `vaquero docs cli` prints the article, unknown topic exits 2).
- [ ] Implement, gate, commit — `"Help: in-app docs, markdown for agents, vaquero docs"`

### Task 4: Docs and check

- [ ] CLAUDE.md (icons rule, `internal/web/icons`, `internal/help`, help routes, `vaquero docs`; "documentation lives in internal/help/articles; a new command, shortcut or route needs its article line"), `.claude/rules/ui-theming.md` (the icon rules), `.claude/rules/copy-voice.md` (articles), BACKLOG.
- [ ] `make ci`; Chrome: top bar, board, ticket page, activity, admin, help index and an article, light and dark, desktop and phone width. Commit — `"Docs: icons and help"`
