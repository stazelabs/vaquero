---
paths:
  - "internal/web/templates/**"
  - "internal/web/static/css/**"
  - "internal/web/static/js/**"
---

# UI theming

CLAUDE.md "Hard invariants" is the authority. Tokens live in internal/web/static/css/input.css (`@theme`, with a dark-mode override block). The palette is a placeholder until the brand is designed.

## Colors

- Use only the tokens `ink`, `paper`, `paper-alt`, `line`, `muted`, `accent`, `danger`, `warn`, with opacity modifiers: `text-ink/70`, `bg-accent/15`.
- `warn` and `danger` mean something and never decorate.
- Never `bg-white`, `text-black`, or raw hues (`red-500`, `#fff`, `oklch(...)`) in templates; `TestNoRawColoursInTemplates` holds it. Dark mode comes free because every token is redefined for dark in input.css.
- System fonts only (`font-sans`, `font-mono`); nothing is downloaded.

## Scripts and styles

- CSP forbids inline scripts and `style=` attributes. JS lives in internal/web/static/js/app.js; use classes, not style attributes.
- Inline `<script>` blocks, where one exists, must carry the nonce from `templ.GetNonce`.

## After changing a template

Run `make gen` (generated *_templ.go files and tailwind.css are committed, never edited by hand). Check the page in light and dark mode.
