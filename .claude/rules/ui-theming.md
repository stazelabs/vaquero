---
paths:
  - "internal/web/templates/**"
  - "internal/web/static/css/**"
  - "internal/web/static/js/**"
---

# UI theming

CLAUDE.md "Hard invariants" is the authority. Tokens live in internal/web/static/css/input.css (`@theme`, with a dark-mode override block). The palette is a placeholder until the brand is designed.

## Colors

- Use only the tokens `ink`, `paper`, `paper-alt`, `line`, `muted`, `accent`, `danger`, `warn`, with opacity modifiers: `text-ink/70`, `bg-accent/15`. `scrim` is for dialog and drawer backdrops only (`backdrop:bg-scrim/40`): it stays near-black in both themes, where `ink` would turn light in dark mode. The monogram tile pairs `tile-0` … `tile-7` (with `tile-N-ink` for the lettering) are for `ProjectIcon` and `Avatar` monograms only.
- `warn` and `danger` mean something and never decorate.
- Never `bg-white`, `text-black`, or raw hues (`red-500`, `#fff`, `oklch(...)`) in templates; `TestNoRawColoursInTemplates` holds it. Dark mode comes free because every token is redefined for dark in input.css.
- System fonts only (`font-sans`, `font-mono`); nothing is downloaded.

## Icons

- Glyphs are Lucide (ISC), listed by purpose in `internal/web/icons/ui.json` (our name → Lucide name) and generated into `ui_gen.go` by `make icons` (part of `make gen`) from the pinned `lucide-static` package. Add a line to ui.json before using a new glyph; `TestEveryIconNameKnown` fails on an unknown name.
- Draw them only with `Icon(name)` (16px), `IconLg(name)` (20px) or `IconClass(name, classes)`; never hand-write an `<svg>` in a template (`TestNoAdHocSVG`). The one exception is the brand mark, `logo.templ`: the cowboy hat from `static/logo.svg`, coloured with theme tokens; it isn't an icon.
- A glyph sits beside the text it belongs to and draws in `currentColor`; tint it with a token class on a wrapper. A link or button with only a glyph carries `aria-label` (`TestIconOnlyButtonsLabelled`).
- Shared meanings: `CategoryIcon`, `PriorityIcon` and `EventIcon` give a status category, priority or event kind its glyph; reuse them rather than choosing again. Empty pages use `emptyState(icon, text)`.
- Below `md` the sidebar is a drawer behind the top bar's menu button; everywhere else it is always shown.

## Sizes

- Use the tokens, never ad hoc sizes (`TestNoAdHocSizes`): type `text-xs` 12 px, `text-sm` 13 px, `text-base` 15 px, `text-lg` 18 px; heights `h-nav` 26 px (sidebar rows), `h-row` 28 px (ticket rows, `rowClass`), `h-chip` 24 px (chips and small buttons, `chipClass`), `h-bar` 38 px (top bar); radius `rounded-sm` 4 px, `rounded-md` 6 px for dialogs and tiles. Surfaces are separated by `line` hairlines, not shadows. Tickets are listed as rows, not cards, except on the board.

## Scripts and styles

- CSP forbids inline scripts and `style=` attributes. JS lives in internal/web/static/js/app.js; use classes, not style attributes.
- Inline `<script>` blocks, where one exists, must carry the nonce from `templ.GetNonce`.

## After changing a template

Run `make gen` (generated *_templ.go files and tailwind.css are committed, never edited by hand). Check the page in light and dark mode.
