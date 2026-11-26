# Focus view, projects page and icons, attachment thumbnails: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a project's everyday questions ("what's next", "what just finished") answerable at a glance, and give projects and image attachments a look of their own.

The four features:
- a Focus view as the project landing page;
- filter menus and List paging;
- a projects page with cards, list and quick settings, plus project icons;
- an attachment gallery backed by server-made thumbnails.

**Architecture:**
- One migration, `0010_icons.sql`, adds the icon fields and a `project_icons` table. Icon changes go through core as `project.updated`.
- Thumbnails come from a new `internal/thumbs` package (standard library only). They're stored through the blob store under their own `thumbs/{instance}/` prefix, and the sweep cleans them.
- The Focus view is server-rendered from the board's own query and the ready rule (part 1's rule when the project's workflow is on), and stays live through the existing stream.

**Tech Stack:** As the repo: Go 1.27, templ/htmx, Tailwind, chromedp e2e. No new dependency.

**Spec:** `docs/superpowers/specs/2026-11-25-ui-focus-projects-thumbnails-design.md`, including its "Calls made after the builder check". Tickets: VAQ-111, VAQ-102, VAQ-103, VAQ-110.

**Runs after:** agent workflow part 1 (run L), so `0009_workflow.sql` and `readiness.Check` exist. If run L hasn't merged, stop and report.

## Global Constraints

- CLAUDE.md hard invariants:
  - only core writes, one event per change;
  - the API is additive;
  - CSP: no inline script, no `style=`;
  - theme tokens only;
  - icons are Lucide through `internal/web/icons` (the monogram is lettering, not an icon);
  - `blobs` is written only through `internal/blobs`.
- **Exactly one migration: `0010_icons.sql`.** Released fixtures must still migrate.
- No new dependency. Image decoding uses the standard library only (`image/png`, `image/jpeg`, `image/gif`). WebP has no standard decoder, so its preview uses the original.
- **Copy, exactly as the spec gives it:**
  - "Nothing ready. Triage the backlog." When the project has a "Needs decision" status, add " or check Needs decision".
  - "Nothing in flight."
  - "Nothing finished this week."
  - "N more ready", "M waiting", "N more this week", "Older: in List", "Show 50 more", "All settings →".
- **Limits:**
  - project icon images: PNG, JPEG or WebP, at most 256 KB (SVG refused);
  - thumbnails: fit within 192 × 192, never enlarged; PNG if the source has transparency, else JPEG at quality 85; refused (falling back to the original) over 40 megapixels.
- Every new route, command and shortcut (`g f`) gets its help-article line; the tests enforce this.
- **Gate:** run `make gen`, `make fmt`, `make vet`, `make lint`, `make test`, then `make ci` and `make e2e`, checking each exit code separately.
- **Commits:** `{Area}: {summary} (VAQ-N)` with the Co-Authored-By line, staging only the changed paths.

## Review Focus

1. **Bookmarked board URLs still work.**
   - `/p/{key}?tag=ui&mine=1` opens Focus with those filters applied.
   - `/p/{key}/board` is the full board.
   - Live refresh and drag on the board still work after the route change. Test in Task 3.
2. **Thumbnails survive the orphan sweep.**
   - A thumbnail whose attachment exists is never deleted.
   - A thumbnail whose attachment was deleted is removed, at the latest by the next sweep.
   - Test in Task 2.
3. **A hostile or huge image doesn't hurt the server.**
   - Covers a 50-megapixel PNG, a truncated JPEG, and a file named `.png` that isn't one.
   - No thumbnail is stored, the preview falls back to the original, and memory use stays bounded.
   - Test in Task 2.
4. **A project key change doesn't recolour its monogram,** and changing the icon doesn't change the key. Test in Task 1.
5. **List paging during a live refresh.** With Done opened and "Show 50 more" used twice, a live refresh keeps the 150 shown and the group open. Test in Task 3 (e2e).

---

### Task 1: Project icons (VAQ-103)

**Files:**
- Create: `internal/db/migrations/0010_icons.sql`, `internal/db/queries/icons.sql`, `internal/core/icons.go`, `internal/web/templates/projecticon.templ`, `internal/web/handlers/project_icon.go`.
- Modify:
  - `internal/core/projects.go` (`UpdateProject` accepts icon kind and glyph);
  - `internal/api/types.go` (`Project.Icon *ProjectIcon`, `omitempty`);
  - `internal/web/icons/ui.json` (the curated glyphs), then run `make icons`;
  - `internal/web/static/css/input.css` (8 tile pairs as tokens, light and dark);
  - `internal/web/templates/projects.templ` (an Icon section in settings: Monogram · Glyph grid · Upload image);
  - `internal/web/router.go`;
  - `internal/web/middleware` (`IsUpload` covers `PUT /api/v1/projects/*/icon` and `POST /p/*/icon`, with a 256 KB limit for these);
  - `cmd/vaquero/cli_cmds.go`;
  - `internal/archive` (load order and table list include `project_icons`);
  - help: api, cli, projects-and-tickets.
- Test: `internal/core/icons_test.go`, `internal/web/handlers/project_icon_test.go`, `cmd/vaquero/cli_icon_test.go`, `internal/archive` export round trip.

**Migration:**
- `projects`:
  - `icon_kind TEXT NOT NULL DEFAULT 'monogram' CHECK (icon_kind IN ('monogram','glyph','image'))`;
  - `icon_glyph TEXT NOT NULL DEFAULT ''`.
- `project_icons`:
  - `project_id TEXT PRIMARY KEY REFERENCES projects(id)`;
  - `type TEXT NOT NULL CHECK (type IN ('image/png','image/jpeg','image/webp'))`;
  - `data TEXT NOT NULL` (base64);
  - `etag TEXT NOT NULL`;
  - `updated_at TEXT NOT NULL`.

**Interfaces:**
- Produces:
  - `func core.Monogram(name, key string) string` returns two upper-case letters: the first letters of the first two words of `name`, else the first two letters of `key`.
  - `func core.TileColor(projectID string) int` returns 0–7, from a stable FNV-1a hash of the id.
  - `func (c *Core) SetProjectIcon(ctx, by, projectID string, kind, glyph string, image *IconImage) (gen.Project, error)`, with `type IconImage struct{ Type string; Data []byte }`.
  - `SetProjectIcon` rules:
    - It writes one `project.updated` event with `changes.icon`.
    - It refuses an unknown glyph: an `*Invalid` on `icon` reading "Choose a glyph from the list."
    - It refuses a disallowed type: "Use a PNG, JPEG or WebP image."
    - It refuses an image over 256 KB: "Use an image of 256 KB or less."
    - Type is sniffed with `http.DetectContentType`, never trusted from the upload.
  - `templates.ProjectIcon(p ProjectIconView, size string) templ.Component` is used everywhere icons show. The sizes are `"sm"` (22 px) and `"md"` (30 px).
  - `GET /p/{key}/icon` serves the image (ETag, `private, no-cache`, 304s, nosniff).
  - `api.ProjectIcon{Kind, Glyph string; URL string}` is part of every project answer.
- Routes and CLI:
  - `PATCH /api/v1/projects/{key}` accepts `icon: {kind, glyph?}`;
  - `PUT /api/v1/projects/{key}/icon` takes a multipart `file` part;
  - `vaquero project icon KEY --monogram | --glyph NAME | --image FILE`.

- [ ] **Step 1: Write the failing tests.**
  - `TestMonogram`:
    - ("ABC Web App", "ABW") gives "AW";
    - ("Vaquero", "VAQ") gives "VA";
    - ("", "XY") gives "XY".
  - `TestTileColorStableAcrossRekey`: `ChangeProjectKey` leaves `TileColor` unchanged (Review Focus 4).
  - `TestSetProjectIconGlyphAndImage`: one event each.
  - `TestSetProjectIconRefusals`: an SVG, 300 KB, an unknown glyph, and a `.png` name holding text bytes.
  - `TestProjectIconServedWithETag`: 200, then 304.
  - `TestAPIProjectIconPatchAndUpload`.
  - `TestCLIProjectIcon`.
  - `TestExportImportKeepsProjectIcons`.
  - `TestFixturesMigrateToHead`: run as part of the suite.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.** Use the tile colour classes `bg-tile-0 … bg-tile-7` with matching text tokens. Render the glyph through `IconClass`.
- [ ] **Step 4: Run the tests.** Expect PASS. Run `make gen`.
- [ ] **Step 5: Commit** `Projects: monogram, glyph and image icons (VAQ-103)`

### Task 2: Server-made thumbnails and the gallery (VAQ-110)

**Files:**
- Create: `internal/thumbs/thumbs.go`, `internal/thumbs/thumbs_test.go`.
- Modify:
  - `internal/web/handlers/attachments.go` (make the thumbnail after a successful upload, best effort; delete it with the attachment; new `AttachmentThumb` handler);
  - `internal/web/router.go` (`GET /attachments/{id}/thumb`, registered before the `/*` name route);
  - `internal/sweep/sweep.go` (also list `thumbs/{instance}/` and delete those whose attachment id has no row, older than `OrphanAge`);
  - `internal/web/templates/tickets.templ` (`AttachmentRow` becomes a gallery for images, rows for the rest);
  - `internal/archive` (nothing: `thumbs/` keys are never exported; add a test that proves it);
  - help: attachments.
- Test: `internal/sweep/sweep_test.go`, `internal/web/handlers/attachments_test.go`, `internal/e2e/gallery_test.go`.

**Interfaces:**
- Produces:
  - `const thumbs.Max = 192`, `const thumbs.MaxPixels = 40_000_000`.
  - `func thumbs.Make(r io.Reader) (data []byte, contentType string, err error)`.
    - It reads with `image.DecodeConfig` first and refuses over `MaxPixels`.
    - It decodes PNG, JPEG or GIF (a GIF's first frame).
    - It downscales by area averaging to fit `Max × Max`, keeping the aspect ratio and never enlarging.
    - It encodes PNG if any pixel has alpha < 255, else JPEG at quality 85.
    - It returns `thumbs.ErrUnsupported` for WebP or anything else, and `thumbs.ErrTooLarge` for oversized images.
  - `func thumbKey(instance, attachmentID string) string` returns `"thumbs/" + instance + "/" + strings.ToLower(attachmentID)`.
  - Thumbnail generation happens at upload, best effort: an error only logs.
- `GET /attachments/{id}/thumb`:
  - It serves the stored thumbnail, or makes it on first request: get the original, `Make`, `Put`. Concurrent requests may both make it, which is fine because the bytes are identical.
  - On `ErrUnsupported`, `ErrTooLarge` or a decode error, it redirects (302) to the original's inline route.
  - It sends the same ETag, caching and sandbox CSP headers as the inline route.
- **Gallery:**
  - Images (`api.IsInlineType`) render first as 96 px tiles (`<img src=".../thumb" width="96" height="96" loading="lazy" class="object-cover">`), each with its name under it, ellipsised.
  - The tile links to the inline original with `target="_blank"`: the builder chose a new tab over a lightbox, which keeps app.js unchanged.
  - Other files render below as today's rows.

- [ ] **Step 1: Write the failing tests.**
  - `TestMakeFitsAndKeepsAspect`: 1440×900 gives 192×120; 100×50 gives 100×50 (never enlarged).
  - `TestMakePNGKeepsAlpha`.
  - `TestMakeJPEGForOpaque`.
  - `TestMakeRefusesHugeAndBogus`: a 50-megapixel PNG header gives `ErrTooLarge` without allocating the full image (Review Focus 3); text bytes, a truncated JPEG, and WebP give errors.
  - `TestThumbRouteMakesOnFirstRequestThen304`.
  - `TestThumbFallsBackToOriginal`.
  - `TestDeletingAttachmentDeletesThumb`.
  - `TestSweepKeepsLiveThumbsAndDeletesOrphans` (Review Focus 2).
  - `TestExportHasNoThumbs`.
  - `TestE2EGalleryShowsImagesFirst`: 375 and 1280 px, light and dark.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.** Add one comment above the area-averaging loop explaining it.
- [ ] **Step 4: Run the tests and `make e2e`.** Expect PASS.
- [ ] **Step 5: Commit** `Attachments: server-made thumbnails and an image gallery (VAQ-110)`

### Task 3: Focus view, filter menus and List paging (VAQ-111)

**Files:**
- Create:
  - `internal/board/focus.go`, `internal/board/focus_test.go`;
  - `internal/web/templates/focus.templ`;
  - `internal/web/templates/filters.templ` (shared filter bar: Mine, Agents, Priority ▾, Milestone ▾, Tags ▾ with search, and active-filter chips).
- Modify:
  - `internal/web/router.go`:
    - `/p/{key}` is Focus;
    - `GET /p/{key}/board` is the full board page;
    - the board fragment moves to `GET /p/{key}/board/fragment` (update app.js and every `hx-get` that used `/board`);
    - add `GET /p/{key}/focus/fragment` for live lanes;
    - `/p/{key}/list` gains `?group=STATUS&offset=N` for paging.
  - `internal/web/handlers/board.go`;
  - `internal/web/templates/board.templ`: use `filters.templ`; Done/Cancelled in the list start collapsed and page 50 at a time.
  - `internal/web/static/js/app.js`:
    - `g f`;
    - the tag menu's search filter;
    - list groups remember open/closed per project in localStorage;
    - paging keeps the count shown across a live refresh.
  - Help: the-board (Focus, menus, paging), keyboard-shortcuts (`g f`).
- Test: `internal/board/focus_test.go`, `internal/web/handlers/focus_test.go`, `internal/e2e/focus_test.go`, and update existing tests that GET `/p/{key}` expecting the board.

**Interfaces:**
- Consumes: `board.Load` and `board.LoadList`; `readiness.Check` (part 1) when `project.Workflow`; card markup from `board.templ`.
- Produces:
  - `type Focus struct { Next []Card; NextMore int; Waiting []Waiting; InFlight []FlightGroup; Finished []Card; FinishedMore int }`.
  - `type Waiting struct { Card; Reason string }`.
  - `type FlightGroup struct { Run string; Cards []Card; More int }`. `Run` is empty until part 3 exists; then group by run.
  - `func board.LoadFocus(ctx, q *gen.Queries, p gen.Project, owner gen.Actor, f Filter, now time.Time) (Focus, error)`.
- The lane rules:
  - **Next:** ready, ordered by priority (Urgent 3 … none 0, descending), then position. Shows 5.
  - **Waiting:** todo tickets that are not ready, each with its reason. Without the workflow, the reasons are "Blocked by KEY", "Has open sub-tickets" and "Claimed by NAME". With the workflow on, they are the part 1 reasons.
  - **In flight:** in progress then in review, 8 per group.
  - **Finished:** done with `status_changed_at` within the last 7 days, newest first, shows 8. Dates read "today", "yesterday", then a weekday name for the rest of the week.
- On phones (below md), the lanes stack in the order Next, In flight, Finished.

- [ ] **Step 1: Write the failing tests.**
  - `TestFocusNextOrdersByPriorityThenPosition`.
  - `TestFocusNextShowsFiveAndCountsMore`.
  - `TestFocusWaitingReasons`: blocked, sub-ticket and claimed, plus a not-approved ticket with the workflow on.
  - `TestFocusFinishedSevenDays`: an 8-day-old done ticket is excluded.
  - `TestFocusEmptyStatesCopy`: the exact strings, including the "Needs decision" variant.
  - `TestOldBoardURLOpensFocusWithFilters` (Review Focus 1).
  - `TestBoardPageAndFragmentRoutes`.
  - `TestListDonePagesFifty`.
  - e2e:
    - `TestE2EFocusLanes` at 375, 768 and 1280 px, light and dark;
    - `TestE2ETagMenuSearch`;
    - `TestE2EListPagingSurvivesRefresh` (Review Focus 5);
    - `TestE2EShortcutGF`.
  - Existing drag e2e tests must pass on `/p/{key}/board`.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests and `make e2e`.** Expect PASS.
- [ ] **Step 5: Commit** `Board: a Focus view, filter menus and List paging (VAQ-111)`

### Task 4: Projects page, quick settings and icons everywhere (VAQ-102)

**Files:**
- Modify:
  - `internal/web/handlers/projects.go` (`ProjectList` builds per-project stats; the `vq_projects_view` cookie is set by `?view=cards|list`);
  - `internal/db/queries/projects.sql` (one query for the per-project counts and latest event time; no N+1: one query, or one per stat across all projects);
  - `internal/web/templates/projects.templ`:
    - cards and list;
    - quick-settings `<details>` posting to the existing settings handlers with `?return=/`;
    - `ProjectIcon` in the project header;
  - `internal/web/templates/tickets.templ` (icon in the breadcrumb);
  - `internal/web/templates/search.templ` (icon by the project label);
  - help: projects-and-tickets.
- Test: `internal/web/handlers/projects_test.go`, `internal/e2e/projects_test.go`.

**Interfaces:**
- Consumes:
  - `templates.ProjectIcon` (Task 1);
  - the ready rule (Task 3's `board` helpers or part 1's `readiness`);
  - project links, if VAQ-119 has shipped. If it hasn't, leave that line out.
- Produces `type ProjectStats struct { Ready, InReview, Open int; Milestone *MilestoneProgress; LastChange time.Time }`. `MilestoneProgress` holds `{Name string; Done, Total int}` for the open milestone with the earliest due date, else the first open one.

- [ ] **Step 1: Write the failing tests.**
  - `TestProjectsPageCardsShowStats`.
  - `TestProjectsViewCookieRemembersList`.
  - `TestQuickSettingsSavesAndReturns`: saves the name and description, returns with a toast; a refused name re-renders with its message.
  - `TestProjectsStatsOneQuery`: count queries with a counting DBTX.
  - e2e:
    - `TestE2EProjectsCardsAndList` at 375 and 1280 px, light and dark;
    - `TestE2EIconInHeaderAndBreadcrumb`.
- [ ] **Step 2: Run them and see them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run the tests and `make e2e`.** Expect PASS. Run `make ci`.
- [ ] **Step 5: Commit** `Projects: cards and list with stats, quick settings and icons (VAQ-102)`
