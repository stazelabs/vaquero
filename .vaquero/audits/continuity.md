# Audit pack: continuity

Vaquero's own continuity pack, read by the audit workflow prompt (`vaquero workflow audit --project VAQ --kind continuity`). The web UI is server-rendered templ with htmx and one script, `app.js`; its rules are short and mostly written down, and several are held by tests. A finding is drift the tests don't catch.

## Standards

Read in full:

- [CLAUDE.md](../../CLAUDE.md): "Hard invariants" (the CSP, theme tokens, icons, copy, the footer, `/admin` inside `AdminShell`), the package map entries for `internal/web/templates`, `internal/web/static` and `internal/web/handlers`.
- [.claude/rules/ui-theming.md](../../.claude/rules/ui-theming.md), [.claude/rules/copy-voice.md](../../.claude/rules/copy-voice.md), and the Feedback and htmx sections of [.claude/rules/handlers.md](../../.claude/rules/handlers.md).
- The shared component definitions: `internal/web/templates/components.templ` (`TextInput`, `TextArea`, `Select`, `FieldError`, `emptyState`, `emptyStateLink`, `firstSteps`, `connectAgent`), `icon.templ` (`Icon`, `IconLg`, `IconClass`), `avatar.templ`, `projecticon.templ`, `layout.templ` (`Shell`, the toast, the shortcuts dialog, the confirm dialog, the footer), `admin.templ` (`AdminShell`), `filters.templ` (`FilterBar`), `board.templ` (`cardItem`), `help.templ` (`helpLink`, `titleWithHelp`), and the class recipes in `helpers.go` (`h1Class`, `h2Class`, `btnPrimary`, `btnSecondary`, `btnDanger`, `btnLink`, `panelClass`, `inputClass`, `fieldClass`, `labelClass`, `proseClass`) and `labels.go` (`CategoryLabel`, `PriorityLabel`, `CategoryIcon`, `PriorityIcon`).

## Dimensions

A deviation breaks a written rule, quoted here. A discontinuity breaks no rule but leaves one surface unlike its peers; its method is below.

| # | Kind | Dimension | Checks against |
|---|---|---|---|
| 1 | deviation | Theming and tokens | ui-theming.md: "Use only the tokens `ink`, `paper`, `paper-alt`, `line`, `muted`, `accent`, `danger`, `warn`, with opacity modifiers"; "`warn` and `danger` mean something and never decorate"; "Never `bg-white`, `text-black`, or raw hues (`red-500`, `#fff`, `oklch(...)`) in templates"; the `tile-N` pairs "are for `ProjectIcon` and `Avatar` monograms only"; icons "Draw them only with `Icon(name)` … never hand-write an `<svg>`". `TestNoRawColoursInTemplates` catches only six-digit hex and `oklch(`: Tailwind palette names (`gray-`, `red-500`, `bg-white`, `text-black`) and three-digit hex are the gap to grep |
| 2 | deviation | Copy and voice | copy-voice.md: "Sentence case everywhere: titles, buttons, tabs, headers, menu items"; "No exclamation points, no emoji, no 'Oops', 'let's', 'you're all set'"; "Buttons name the action"; "Real counts: '1 ticket', '2 tickets', never 'ticket(s)'"; "The product is 'Vaquero' in prose and `vaquero` as a command"; "Never name competing products in docs or UI" (the one exception, in CLAUDE.md's words: "an importer, which has to say what it imports": the `vaquero import linear` lines in the cli, api and data-and-backups articles, and the importer's own usage and output in `cmd/vaquero/main.go` and `cli_linear.go`); "Never name a model provider in app copy"; "Errors state the problem and what to do". Covers templ strings, toasts, core's refusal messages (`internal/core`), CLI output (`cmd/vaquero`) and the help articles |
| 3 | deviation | Accessibility and interaction | ui-theming.md: "A link or button with only a glyph carries `aria-label`" (`TestIconOnlyButtonsLabelled` holds the templates it scans); CLAUDE.md: "CSP allows no inline script and no `style=` attribute. All behaviour is in `internal/web/static/js/app.js`"; every control's focus visible (the `focus-visible:ring` in `btnBase`, `inputClass`); labels tied to inputs (`TextInput` sets them); no native `confirm()`, `alert()` or `prompt()` (the confirm dialog, `#vq-confirm`, is the one way to ask); the keyboard shortcuts leave keys typed into fields alone, and Enter on a focused link or button follows it |
| 4 | deviation | Role gating | Local mode has no roles in the browser, so the rule here is the owner against agents: CLAUDE.md "The gate is the owner's (N-R6)" and "The repo is the owner's (P2-R1)". The browser is always the owner and the UI has no agent-only path, so the one failure to look for is copy: a control that only works for the owner (the workflow switch, the tier map, the repo settings, the approval answer form) whose label or note doesn't say it is the owner's, against the core rule it rests on. With the workflow off, no Tier, Model or readiness control shows (`TestNoTierUIWithWorkflowOff`) |
| 5 | discontinuity | Partial adoption of shared components | Methods, 5 |
| 6 | deviation | Narrow viewport (a suspicion from source) | CLAUDE.md, `internal/e2e`: "no page wider than 375 px at 375 px". Methods, 6 |
| 7 | discontinuity | Peer check across surfaces | Methods, 7 |

## Methods

### 5. Partial adoption of shared components

Count call sites both ways in `internal/web/templates/*.templ`:

- `TextInput` and `TextArea` against a raw `<input` (not `type="hidden"`, `file`, `checkbox` or `radio`) or `<textarea`. The header search box, the search page's box and the filter menus' tag search are raw `type="search"` boxes: count them, and file them only if their styling or labelling differs from `TextInput`'s.
- `Select` against a raw `<select` outside `components.templ`.
- `FieldError` against a hand-written error line under a field.
- `emptyState` and `emptyStateLink` against a bare "No … yet." paragraph, on a page (or a page's live part) with nothing in it. Inside a section of a page that has other content, the bare muted paragraph is the convention: don't count it.
- The confirm dialog, `data-confirm="question"` on a form, against a destructive POST form without it: count every form whose action ends `/delete`, `/revoke` or `/archive`, or that moves or merges, both ways. The project delete form, which confirms by the typed key, is the counted exception. Ask again on a ticket page withdraws an open approval only to ask the same one again at once, and loses nothing: it isn't counted.
- Toasts through `redirectWithFlash` in `internal/web/handlers` against a handler that renders a success message into the page or redirects with none.
- The button recipes (`btnPrimary`, `btnSecondary`, `btnDanger`, `btnLink`) against a `<button` or `<a` carrying its own `rounded-md px-` classes.
- `h1Class` and `h2Class` against a hand-styled `<h1` or `<h2`.
- `ProjectIcon` and `Avatar` against a hand-drawn letter tile or a raw `<img` of an icon or avatar.
- `CategoryIcon`, `PriorityIcon` and `EventIcon`, and `CategoryLabel` and `PriorityLabel`, against a glyph or word chosen again by hand for a category, priority or event.
- `titleWithHelp` and `helpLink` against a hand-made link to `/help/…` beside a page title.

### 6. Narrow viewport

Candidate rows:

```
grep -n 'justify-between' internal/web/templates/*.templ
grep -nE 'whitespace-nowrap|min-w-\[|w-[0-9]{2,}\b|grid-cols-[3-9]' internal/web/templates/*.templ
```

For each `justify-between` hit, in order:

1. Does the row have an action group (a sibling holding controls)? None: not a finding.
2. Does the group hold two or more controls (button, link, menu, form)? One control leaves the label room.
3. Does it already wrap or stack below `sm` (`flex-wrap`, `flex-col … sm:flex-row`, `max-sm:` classes)? Then it complies.

A finding fails 3 while passing 1 and 2. For the width grep, a fixed width wider than about 340 px or a non-wrapping row of several items without `overflow-x-auto` around it is the suspect. Weigh pages `TestPhoneWidthNoSidewaysScroll` (`internal/e2e/widths_test.go`) doesn't visit first: `/profile`, `/admin/backups`, `/admin/mcp`, the file view `/p/{key}/files/…`, a ticket with a plan, references and attachments, and the error pages. The board's rails overlay below `md` on purpose. Every row here is a suspicion for the qa audit to confirm.

### 7. Peer check across surfaces

Read each surface's template and its handler. The peer groups, confirmed in `internal/web/router.go`:

| Group | Surfaces |
|---|---|
| Project views (`projectHeader` tabs) | Focus (`/p/{key}/`), Board (`/p/{key}/board`), List (`/p/{key}/list`): the filter bar, chips and menus, the help link beside the title (`helpLink`), the live parts (`data-live`), empty and no-tag notices, card markup (`cardItem`) |
| Top-level pages (`Shell`) | Projects (`/`), Activity (`/activity`), Search (`/search`), Help (`/help`, `/help/{slug}`), Profile (`/profile`), Licences (`/about/licenses`) |
| Forms | New project (`/projects/new`), ticket new and edit (`/p/{key}/new`, `/t/{ticket}/edit`), the profile form, the quick settings on the projects page: field components, labels, error display, the action button's wording and place |
| Project settings sections (`/p/{key}/settings`) | Name and description, statuses, milestones, tags, icon, project links, agent workflow, repository, key change, archive, delete: section headings, add forms, row actions, confirmations |
| Ticket page sections (`/t/{ticket}`) | Description, sub-tickets, links, references, attachments, plan, readiness, comments, history: section headings (`h2Class` with glyph), empty states, row delete actions and their confirmations |
| Admin pages (`AdminShell`) | Home (`/admin`), Backups (`/admin/backups`), API tokens (`/admin/tokens`), MCP activity (`/admin/mcp`) |
| Error pages | `ErrorPage`, `NotFoundPage`, `TooLargePage` |

## Search surface

`internal/web/templates/*.templ` and the `.go` helpers beside them (never `*_templ.go`), `internal/web/handlers/*.go` (toasts, redirects, refusal text), `internal/web/static/js/app.js`, `internal/web/static/css/input.css`, `internal/web/router.go`, `internal/core/*.go` for the wording of refusal messages, `cmd/vaquero/*.go` for CLI output, `internal/help/articles/*.md` for copy. Never `internal/web/static/css/tailwind.css`, `internal/web/icons/ui_gen.go` or `internal/web/static/vendor/`.

## Known non-findings

- **The project delete form confirms by typing the key**, not through the confirm dialog: a stronger confirmation, by design.
- **The brand mark in `logo.templ`** is inline SVG in theme tokens; it isn't an icon (`TestNoAdHocSVG` allows it, with `icon.templ`).
- **The three `<script>` tags in `layout.templ`** load htmx, Sortable and `app.js` from `/static/` with the nonce: what the CSP allows.
- **The `tile-0` … `tile-7` pairs** appear on project monograms and avatars only, by rule.
- **`warn` on a claim note and `danger` on destructive buttons and errors** carry meaning.
- **The Admin tokens page shows a new token once**, on the POST answer, with `data-replace-url`; no redirect, by design.
- **No theme toggle**: the theme follows the system.
- **Below `sm` the top bar shows glyphs only**, its labels kept as screen-reader text, by rule.
- **The board's rails overlay the columns below `md`**, with a close button, by design.
- **Tier, Model and readiness are absent with the workflow off**: the workflow-off rule.
- **The importer's lines name the product it imports** (`vaquero import linear`, in the cli, api and data-and-backups articles, and the importer's own usage and output in `cmd/vaquero`), the one exception to the competing-products rule.
- **Harness names and model ids where the product connects to or configures a harness** (the connect-agent line, `claude mcp add vaquero -- vaquero mcp --agent NAME`; the MCP, agent-workflow and installing articles; the default tier map's `claude` and `codex` model ids) are not a model provider named in copy.
- **Settings sections differ in content**; only their chrome (headings, add forms, row actions) is compared.
- **Admin pages are not peers of project pages**: they live in `AdminShell` with their own nav.
- **Generated files** (`*_templ.go`, `tailwind.css`, `ui_gen.go`) are never evidence.
- **Dated specs and plans** under `docs/superpowers/` are history, not audited for copy.
