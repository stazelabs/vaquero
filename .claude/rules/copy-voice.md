---
paths:
  - "internal/web/templates/**"
  - "internal/web/handlers/**"
  - "cmd/vaquero/**"
  - "README.md"
  - "docs/**"
---

# Voice and copy

Applies to templ strings, toasts, error messages, CLI output, the README and docs. CLAUDE.md "Copy" is the authority.

## Tone

- Sentence case everywhere: titles, buttons, tabs, headers, menu items.
- No exclamation points, no emoji, no "Oops", "let's", "you're all set". Few em-dashes.
- Calm and direct. Errors state the problem and what to do, not an apology.
- Empty states describe: "No tickets yet." If there is a next step, name it.

## Vocabulary

- The product is "Vaquero" in prose and `vaquero` as a command.
- Core nouns stay plain: project, ticket, status, tag, comment, milestone. Agents must be able to guess them. The cowboy theme lives in the name, the tagline ("Vaquero herds the chaos.") and the visual identity, not in the nouns.
- Never name competing products in docs or UI.
- Never name a model provider in app copy.

## Help articles

- Documentation lives in `internal/help/articles/*.md` and ships in the binary (`/help`, `/help/{slug}.md`, `vaquero docs`). Front matter: `title`, `section` (getting-started, using, command-line, api-and-agents), `summary` (one sentence), `order`; every article has `##` headings.
- A new CLI command, keyboard shortcut or `/api/v1` route needs its line in the `cli`, `keyboard-shortcuts` or `api` article: tests fail naming what is missing. Links between articles use `/help/slug` (and `#anchor`); a link to a missing article or heading fails a test.
- Same voice as the UI. Write for someone using Vaquero, not someone reading the code; say what happens, then how.

## Specifics

- Buttons name the action: "Create ticket", not "Submit".
- Real counts: "1 ticket", "2 tickets", never "ticket(s)".
- Copy says what a surface does. It avoids vendor-speak ("powerful", "seamless").
