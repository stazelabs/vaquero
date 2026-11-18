# Footer, logo, project key change and project delete — Design and Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every page shows the copyright, the running version and links to vaquero's licence and the open source notices; a cowboy hat is the logo; a project's key can be changed; a project can be deleted.

**Spec:** this document (a small change, decided in conversation on 2026-11-18), extending `docs/superpowers/specs/2026-11-18-vaquero-design.md`.

## Decided by the user

- Footer copyright: "© 2026 Purple Bongo, Inc. · A Stazelabs project" (the second part links to https://stazelabs.com, as in healthcheck-email). NOTICE changes to match. Version and licence links follow the sibling footers' row-with-dots pattern.
- Logo: a cowboy hat.
- Changing a project key **drops the old key**: old ticket URLs, typed keys, branch names and commit references using it stop resolving, and the old key is free for another project. Ticket numbers don't change.
- Deleting a project **removes everything** (its tickets, comments, links, milestones, statuses and history) after the person types the project's key; a database snapshot is taken first.

## Builder's calls

- **Footer** (every page): `© 2026 Purple Bongo, Inc. · A Stazelabs project · Vaquero {version} ({commit}) · Licence · Open source notices`. "Licence" and "Open source notices" go to `/about/licenses`, one page with two sections: vaquero's Apache 2.0 text (embedded from the repo's `LICENSE` and `NOTICE`), then every third-party component compiled or bundled into the binary, each with its licence text in a `<details>`.
- **Notices are generated**, not hand-kept: `go run ./scripts/notices` writes `internal/legal/third_party.json` from the modules actually linked into `cmd/vaquero` (`go list -deps`), the Go standard library, and a small hand-kept list of bundled browser files (htmx 2.0.4, 0BSD; SortableJS 1.15.6, MIT; Tailwind CSS 4.3.5, MIT) whose licence texts live in `internal/legal/vendor/`. `make gen` runs it, so CI's "generated files are fresh" check catches a dependency change without a notices update.
- **Logo**: an original, simple cowboy-hat SVG (crown, pinch, curled brim, band) in the accent colour. It replaces the lasso favicon and sits before the "Vaquero" wordmark in the header.
- **Key change**: a "Key" form on the settings page with the warning "Ticket links, branch names and commit messages that use VAQ will stop working." `core.ChangeProjectKey` validates like creation, refuses a key another project holds, and records `project.updated` with `changes.key`. The response goes to `/p/{new}/settings`.
- **Delete**: a "Delete project" section at the bottom of settings with "Type VAQ to delete this project and everything in it." The handler snapshots the database to `{data dir}/backups/vaquero-{UTC stamp}.db` (ported `db.Snapshot` from startcase; keep the newest 7), then calls `core.DeleteProject`, which in one transaction deletes the project's ticket tags, comments, links touching its tickets (from any project), events about it, tickets, milestones, statuses and the project, then records one `project.deleted` event (no project or ticket id; payload `{key, name}`). Tags stay. Toast: "Project VAQ deleted. A backup was saved first."
- Migration `0004_hard_delete.sql` adds `AFTER DELETE` triggers on `tickets` and `comments` that remove their rows from the search indexes (backlog item: FTS rowids and hard deletes).

## Review Focus

1. **Someone deletes a project whose tickets are linked from another project's tickets.** The other project's tickets lose those links and keep working; nothing dangles. (Task 3.)
2. **Someone types the wrong key, or a lowercase one, to confirm a delete.** Wrong is refused with nothing changed; case doesn't matter. (Task 3.)
3. **After a delete, search no longer finds the deleted tickets or comments.** (Task 3.)
4. **Someone changes VAQ to a key that was another project's old key, or to one in use.** In use is refused; a freed key is fine. (Task 2.)
5. **The snapshot can't be written (disk full, unwritable directory).** The delete doesn't happen and the person is told why. (Task 3.)

---

### Task 1: Footer, licence page, notices generator, logo

**Files:** `legal.go` (package `vaquero` at the module root: `//go:embed LICENSE NOTICE`; `func License() string`, `func Notice() string`), `scripts/notices/main.go`, `internal/legal/legal.go` (`type Component struct{ Name, Version, URL, License string }`, `func ThirdParty() []Component` from the embedded JSON), `internal/legal/third_party.json` (generated), `internal/legal/vendor/{htmx,sortablejs,tailwindcss}.txt`, `internal/web/templates/layout.templ` (footer, logo), `internal/web/templates/about.templ`, `internal/web/handlers/about.go`, `internal/web/static/favicon.svg`, `internal/web/static/logo.svg`, `NOTICE`, `Makefile` (`notices` target, part of `gen`), tests.

- [ ] **Tests first:** `TestFooterOnEveryPage` (`/`, `/activity`, `/search`, a 404: contains "Purple Bongo, Inc.", `href="https://stazelabs.com"`, `version.Version`, `href="/about/licenses"`), `TestLicensesPage` (contains "Apache License", "Purple Bongo", "htmx", "SortableJS", "Tailwind", "github.com/go-chi/chi", "Go standard library", and a licence text such as "Permission is hereby granted"), `TestThirdPartyCoversEveryLinkedModule` (every module from `go list -deps ./cmd/vaquero` except this one appears in `ThirdParty()`, each with a non-empty licence), `TestLogoInHeader` (`static.URL("logo.svg")` is referenced and served).
- [ ] **Implement**, run `make gen && go test ./...`, gate, commit — `"Web: footer, licence page, open source notices, cowboy hat"`

### Task 2: Change a project's key

**Files:** `internal/core/projects.go` (`func (c *Core) ChangeProjectKey(ctx context.Context, by, projectID, newKey string) (gen.Project, error)`), `internal/db/queries/projects.sql` (`SetProjectKey`), `internal/web/handlers/projects.go` (`ProjectKeyChange`, `POST /p/{key}/key`), `internal/web/templates/projects.templ`, `internal/web/templates/activity.go` (wording "changed the key of VAQ to VQ"), tests.

- [ ] **Tests first:** core `TestChangeProjectKey` (VAQ → VQ: `ResolveTicket(VQ-1)` finds the ticket, `VAQ-1` is `ErrNotFound`; event payload `changes.key` from VAQ to VQ), `TestChangeKeyRefusals` (bad format, taken key, unchanged key → Invalid, nothing written), `TestFreedKeyReusable` (after VAQ → VQ, a new project may take VAQ); an `ops` entry; handler `TestKeyChangeFlow` (POST lowercase `vq` → 303 to `/p/VQ/settings` with "Key changed to VQ." and the warning text on the settings page; `/t/VAQ-1` now "No ticket VAQ-1.").
- [ ] **Implement**, gate, commit — `"Core: change a project's key"`

### Task 3: Delete a project

**Files:** `internal/db/backup.go` (port `Snapshot` and `PruneBackups` from startcase; prefix `vaquero-`), `internal/db/migrations/0004_hard_delete.sql`, `internal/db/queries/projects.sql` (the delete statements), `internal/core/projects.go` (`func (c *Core) DeleteProject(ctx context.Context, by, projectID, confirm string) error`), `EvProjectDeleted`, `internal/web/handlers/projects.go` (`ProjectDelete`, `POST /p/{key}/delete`; `Deps.BackupDir`), `projects.templ`, `activity.go`, `cmd/vaquero/serve.go`, `webtest`, tests.

- [ ] **Tests first:** core `TestDeleteProjectRemovesEverything` (counts of every tracker table for that project are 0; the other project's rows unchanged except links into the deleted tickets; one `project.deleted` event remains), `TestDeleteConfirmation` (wrong → `Invalid{"confirm": "Type VAQ to confirm."}`, nothing changed; lowercase accepted), `TestDeleteDropsSearchRows` (search for a deleted ticket's word and comment word finds nothing), an `ops` entry; db `TestSnapshot` (ported); handler `TestDeleteFlow` (snapshot file exists in the backup dir, redirect `/` with the toast, `/p/VAQ` → "No project VAQ."), `TestDeleteWhenSnapshotFails` (an unwritable backup dir: 303 back to settings with "Couldn't save a backup, so nothing was deleted." and the project intact).
- [ ] **Implement**, gate, commit — `"Core: delete a project, with a backup first"`

### Task 4: Docs and check

- [ ] CLAUDE.md package map (root `vaquero` package, `internal/legal`, `scripts/notices`, new core operations, backups), the invariant on project keys ("changeable; the old key stops resolving"), `docs/BACKLOG.md` (tick the FTS hard-delete item), floors for `internal/legal`.
- [ ] `make ci`; browser look at the footer, logo (light and dark) and the licences page; commit — `"Docs: footer, keys and delete"`
