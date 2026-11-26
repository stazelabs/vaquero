# Focus view, projects page and icons, attachment thumbnails: design

Tickets: VAQ-111 (long boards and lists), VAQ-102 (projects page), VAQ-103 (project icons), VAQ-110 (attachment previews). Decided with the owner on 2026-11-25, with mockups.

## Intent

The owner uses a project mostly to see **what's next** and **what just finished**. Today the board shows everything at once: a wall of about 30 filter chips, and columns 16 cards tall. The list is one long page. The projects page is a bare row per project. Attachments are plain file rows, even when they are screenshots.

The aim is to make the everyday questions answerable at a glance, without scrolling, and to give projects an identity you can recognise.

## Goals

- A project opens on a **Focus** view: next (ready), in flight, and just finished.
- The list stops scrolling forever. The tag wall becomes a menu.
- The projects page shows each project's description, health and icon, in cards or a list, with quick settings.
- Projects have icons: a distinct default for every project, which can be changed to a glyph or an image.
- Image attachments show as a gallery of previews, made by the server so pages stay light.

## Non-goals

- New ways to filter or sort beyond those named here.
- Board changes beyond the tag menu. VAQ-142 (the squeezed Done column) is a separate fix.
- Image editing, annotation, or previews of non-image files (PDF and others).
- Previews on board cards (option C was declined).

## Anti-goals

- The board stops being reachable, or its URLs and filters break.
- A project's look depends on setup. Every new project must look distinct with no action.
- Pages get heavy: a ticket with many screenshots downloads every original.
- Any change breaks the CSP (no inline script, no `style=`), the theme tokens, or the e2e suite.

## Constraints

- CLAUDE.md invariants:
  - only core writes, with one event per change;
  - the API is additive;
  - CSP and theme tokens only;
  - no new Go dependency (decoding uses the standard library's `image/png`, `image/jpeg` and `image/gif`);
  - icons are Lucide, through `internal/web/icons`. The monogram is lettering, not an icon.
- At most one migration per run.
- The `blobs` table is written only through `internal/blobs`.
- `make e2e` must cover the new pages at 375, 768 and 1280 px, in light and dark.

## Decisions made with the owner

1. **VAQ-111: the Focus view (option A)** is the project's landing page. It has three lanes:
   - **Next**: ready tickets, top 5.
   - **In flight**: in progress plus in review.
   - **Just finished**: done in the last 7 days, newest first.

   The board and the list stay as tabs. Taken from option B: the tag chips become a searchable **Tags ▾** menu.
2. **List view:** each status group collapses (remembered). Done and Cancelled start collapsed and show 50 at a time, with "Show 50 more". *This was the controller's proposal; the owner approved it.*
3. **VAQ-102: the projects page.**
   - Cards by default, with a list view. The choice is remembered.
   - Each project shows: icon, name, key, description, counts (ready, in review, open), the open milestone's progress, when it last changed, and its project links (VAQ-119).
   - "⋯" opens quick settings in place (name, description, icon), with "All settings →".
4. **VAQ-103: icons (option 3).**
   - The default is a **monogram tile**: two letters, on a colour chosen from the key.
   - Settings can switch to a **Lucide glyph** from a curated set, or to an **uploaded image**.
5. **VAQ-110: a gallery (option A).** Images show as a grid of 96 px previews with their names. Other files are listed as rows below. Clicking a preview opens the full image.
6. **Thumbnails are made by the server (option 2),** not by the browser scaling the original.

## Approaches considered

- **VAQ-111:**
  - (A) **Focus view. Chosen.**
  - (B) The same board with capped columns: the middle of the queue is still there once expanded. Only its Tags menu was kept.
  - (C) Strips above a folded board: the board is still the long page whenever it's open.
- **VAQ-110:**
  - (A) **Gallery. Chosen.**
  - (B) Rows with a small preview: compact, but less readable for screenshots.
  - (C) A plus previews on board cards: cards get taller.
- **Thumbnails:**
  1. Browser scaling: no new code, but heavy once hosted.
  2. **Server-made at upload. Chosen.**
  3. Browser now, server later.
- **Icons:**
  1. Monogram only.
  2. Glyph only: every project looks the same until changed.
  3. **Monogram by default, then glyph or image. Chosen.**

## Design

### Focus view (VAQ-111)

**Route.** The Focus view is the project's landing page, `/p/{key}`. The full board moves to its own page route (the builder names it, for example `/p/{key}/board`; today that path is the board fragment, so the fragment moves too). Old links to `/p/{key}` with board filters in the query keep working: Focus takes the same filters. `g b` opens the board, `g l` the list, and a new `g f` opens Focus, documented in keyboard-shortcuts. The tabs read **Focus · Board · List · Settings**.

**Next.**
- Ready tickets: today's rule; when the agent workflow is on (part 1), its readiness rule.
- Order: priority (Urgent, High, Medium, Low, none), then board position.
- Shows the top 5, then "N more ready", which opens the rest in place.
- Then "M waiting": todo tickets that would be next but aren't ready (an open blocker, an open sub-ticket, a claim, or with the workflow on, a missing approval). It opens a list with each ticket's reason.

**In flight.**
- In progress, then in review. Once runs exist (agent workflow part 3), grouped by run with a header ("run-k · 16 tickets ▸").
- Shows up to 8 per group, then "N more".
- Each card shows its claim (holder and lease), as on the board.

**Just finished.**
- Done in the last 7 days, newest first by status change, each with its date ("today", "yesterday", "Mon").
- Shows the top 8, then "N more this week". "Older: in List" links to the list with Done expanded.

**Behaviour.**
- The lanes are live (`data-live`, `vq:change`), like the board.
- Card markup is shared with the board.
- Empty lanes say what they mean: "Nothing ready. Triage the backlog or check Needs decision." "Nothing in flight." "Nothing finished this week."
- On a phone (below md) the lanes stack vertically in this order: Next, In flight, Just finished.

### Filters: Tags menu (all three views)

- **Quick chips** stay: Mine, Agents.
- **Priority ▾** and **Milestone ▾** become menus.
- **Tags ▾** is a menu with a search field that filters the tag list as you type (app.js, CSP-safe). Choosing a tag applies the same `?tag=` filter as today. Active filters show as removable chips next to the menus.

### List view

- Each status group has a disclosure, remembered per project in the browser (localStorage).
- Done and Cancelled start collapsed. When opened, they show 50 tickets at a time. "Show 50 more" loads the next page in place (htmx, `?offset=`), live-safe.
- Active groups (backlog, todo, in progress, in review) are not paged.

### Projects page (VAQ-102)

- **Views.** Cards (default) or List, switched by a control in the header and remembered (a cookie, `vq_projects_view`, so the server renders the right one).
- **Card and row content:**
  - the icon;
  - the name and key;
  - the description (cards: first 2 lines; list: 1 line);
  - counts: ready, in review, open;
  - the open milestone: the earliest due, else the first open, with its done/total bar, or "No open milestone";
  - last change: the newest event's time, relative;
  - project links ("depends on X", "used by Y", from VAQ-119), shown only once VAQ-119 exists.
- Archived projects keep today's separate section.
- **Quick settings.** "⋯" opens a panel in place (a `<details>`) with name, description and icon. It posts to the existing settings handlers and returns to the projects page with a toast. "All settings →" opens the full page.

### Project icons (VAQ-103)

- **Stored on the project.** `icon_kind` (`monogram`, `glyph` or `image`; default `monogram`) and `icon_glyph` (a name from the curated set); an image lives in `project_icons` (see the calls below). Changes go through core, as `project.updated` events. This is one migration.
- **Monogram.**
  - Two letters: the first letters of the first two words of the name, else the first two letters of the key. Upper case.
  - The colour is one of 8 tile colour pairs chosen by a stable hash of the key.
  - The pairs are new theme tokens in input.css, defined for light and dark, so templates still use tokens only.
  - It is rendered as lettering in a tile, not SVG.
- **Glyph.** A curated set of about 40 Lucide glyphs (folder, rocket, smartphone, globe, server, database, book, wrench, and so on), added to `internal/web/icons/ui.json` and drawn through `IconClass`. Settings shows them as a picker grid.
- **Image.**
  - PNG, JPEG or WebP up to 256 KB. Other types, SVG included, are refused with a message.
  - It is stored in `project_icons` (see the calls below), and served from its own route with its ETag (`private, no-cache`, 304s), like attachments.
  - It is shown square-cropped (`object-cover`) at the tile size.
  - Export and import carry it.
- **Where icons show:** the projects page, the project header, the breadcrumb on ticket pages, and the search results' project label. The project's JSON in the API gains `icon` (`{kind, glyph?, url?}`), additively.

### Attachment gallery and thumbnails (VAQ-110)

**Gallery.**
- On the ticket page, the attachments section shows images (PNG, JPEG, GIF, WebP) first, as a grid of 96 px previews (192 px images for sharp screens), each with its name, ellipsised.
- Other files follow as today's rows.
- Clicking a preview opens the full image. The builder chooses: the existing inline attachment route in a new tab, or a lightbox `<dialog>` in app.js closed by Escape, which then needs an e2e test.
- Delete stays where it is, behind the confirm dialog.

**Thumbnails, made by the server.**
- **Size and format:** the image scaled to fit 192 × 192, aspect kept, never enlarged. Encoded as PNG when the source has transparency, else JPEG at quality 85.
- **Decoding:** standard library only (`image/png`, `image/jpeg`, `image/gif`; a GIF's first frame). WebP has no standard decoder, so a WebP preview uses the original, scaled by the browser.
- **The resizer:** a small area-averaging downscaler in Go, in its own package (for example `internal/thumbs`), tested for exact output dimensions and the aspect ratio.
- **Safety:**
  - read the size with `image.DecodeConfig` first, and refuse to decode anything over 40 megapixels (it falls back to the original);
  - bound the decode time;
  - never trust the claimed content type: decode by sniffing.
- **When made:**
  - At upload, best effort: a failure never fails the upload.
  - On the first request for a preview that doesn't exist yet. This is also the backfill for existing images, so no separate job is needed.
  - Generation is idempotent: two requests at once write the same bytes.
- **Storage:**
  - Through the blob store, under a key derived from the attachment's.
  - The builder checks `blobs.ValidKey` and **the sweep**. The orphan sweep deletes objects under `attachments/{instance}/` that have no attachment row, so the thumbnail key must either be recognised by the sweep (kept while its attachment exists, deleted with it) or live under its own prefix that the sweep also cleans.
  - Deleting an attachment deletes its thumbnail.
- **Export and import:** thumbnails are derived data. They are not exported, and they are remade on demand after an import.
- **Served** from a route next to the attachment's (for example `/attachments/{id}/thumb`) with the same ETag, caching and CSP headers as the inline attachment route.

## Calls made after the builder check (controller, 2026-11-25; the owner can overrule)

1. **Order:** this ships after agent workflow part 1 (run L), as its own run. That follows the owner's "factory first". It takes the next migration number (0010). The Next lane, the "ready" counts and "M waiting" use part 1's readiness rule when a project's workflow is on, and today's rule when it is off. The Next lane's empty state names "Needs decision" only when the project has that status. Otherwise it reads "Nothing ready. Triage the backlog." Run K (in progress, no migrations) doesn't include this.
2. **Icon images don't go in the blob store.** They are small (256 KB at most), so they live in the database, in a `project_icons` table (project, type, `data` as base64 TEXT, etag, updated). As a result:
   - the blob store's attachment-only rules (key prefix, orphan sweep, backend lock) stay as they are;
   - the export archive carries icons as an ordinary table in export.json, with no new entry kind and no `FormatVersion` bump;
   - an instance with only icons locks nothing.

   This replaces "stored through the blob store under a project key" above.
3. **Setting icons:** through the web settings, the API and the CLI, matching statuses and milestones (VAQ-125, VAQ-127):
   - `PATCH /api/v1/projects/{key}` takes `icon: {kind, glyph?}`;
   - `PUT /api/v1/projects/{key}/icon` takes a multipart image upload (added to `BodyLimit`'s upload routes, with a 256 KB limit for this route);
   - `vaquero project icon KEY --monogram | --glyph NAME | --image FILE`.

   No MCP tool. Help article lines as the tests require.
4. **The monogram colour hashes the project's id, not its key,** so changing a key never recolours a project.

## How this maps to tickets

- VAQ-111: Focus view, Tags menu and List paging.
- VAQ-102: projects page.
- VAQ-103: icons, including the migration.
- VAQ-110: gallery and thumbnails.

They can ship in one run, with one migration (for the icon fields) and no other schema change, or across two runs. The builder decides the batches.

## Left to the builder

- Route names for the board page and fragment, the thumbnail route, and the icon route.
- The 8 tile colour pairs (theme tokens, checked for contrast in both themes) and the curated glyph list.
- Lightbox or new tab for the full image.
- The exact layouts at each width, checked with e2e and headless screenshots at 375, 768 and 1280 px, light and dark. The mockups (this session) are a guide, not a spec.
- Whether thumbnails are made synchronously at upload or right after it.
