# Phase 3a: Releases and upgrades — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Vaquero can be released and upgraded without losing data. A release is built from a manual workflow and published to GitHub and a Homebrew tap, and upgrading is `brew upgrade vaquero`. The next command after an upgrade restarts a local server still running the old binary. The server snapshots the database before migrating it. Every released database is proven to migrate to head. A backup can be restored.

**Architecture:** Vaquero makes no outbound calls: Homebrew installs and upgrades it, and checks each archive against the formula's sha256. goreleaser builds the archives and `checksums.txt`, and writes the formula with a `service` block, so `brew services start vaquero` keeps Vaquero running at login. The CLI compares the local server's `Vaquero-Version` with its own. When the server is older, the CLI stops it and starts its own binary. `cmd/vaquero` gains `stop`, `backup list` and `backup restore`, plus a snapshot before pending migrations.

**Tech Stack:** As Phase 2b. goreleaser v2 (2.20.2 locally) is a release tool only. No new Go dependencies.

**Spec:** `docs/superpowers/specs/2026-11-18-vaquero-design.md`, section "Upgrades", as amended on 2026-11-20: upgrades go through Homebrew, with no `vaquero upgrade` and no update notice. Phase 3b (export and import, the Linear importer, the skills adapter, moving the backlog in) gets its own plan.

**Patterns to follow:** `/Users/cstaszak/Stazelabs/projects/oza/.goreleaser.yaml` (archives, checksums, Homebrew formula), read-only. Do not copy its `on: push: tags` trigger.

## Global Constraints

- CLAUDE.md invariants apply unchanged, including "no telemetry, no outbound network calls".
- Never add `push` or `pull_request` triggers: the release workflow is `workflow_dispatch` only.
- Supported platforms: darwin and linux, amd64 and arm64. Homebrew covers both; on Linux without Homebrew, people install from the release archive.
- Archive name: `vaquero_{version}_{os}_{arch}.tar.gz`, holding `vaquero`, `LICENSE`, `NOTICE` and `README.md`, alongside `checksums.txt`. The tap is `stazelabs/homebrew-vaquero`, so the install command is `brew install stazelabs/vaquero/vaquero`.
- A dev build is version `0.0.0-dev`. It never restarts a server for being newer, and never shows the upgrade-needed message.
- Snapshots before a migration go to `{data dir}/backups`, with the same names and keep count (`backupKeep`, 7) as the snapshots taken before a project delete.
- `stop` and `backup` work without a server, using the data directory from `VQ_DATA_DIR` or the default.
- Copy follows `.claude/rules/copy-voice.md`. New commands get rows in the `cli` help article (tests enforce this).
- Gate steps run separately and are judged by exit code; `make ci` ends each task. Commits are `{Area}: {summary}` with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **`brew upgrade vaquero` runs while the old server is up.** The next CLI command, or `vaquero mcp` on start, restarts the server on the new binary. It says so once on stderr, and the command then succeeds. Task 4 holds this with `TestStaleLocalServerRestarted`.
2. **The server is newer than the CLI** (an old binary earlier on PATH, or a dev build). Nothing restarts, so two binaries never take turns restarting each other. Task 4 holds this with `TestNewerServerNotRestarted` and `TestDevCLINeverRestarts`.
3. **Two commands notice the stale server at once** (two agents' bridges, say). One restarts it, and the other waits for the new server rather than failing or restarting it again. Task 4 holds this with `TestConcurrentStaleRestart`.
4. **The snapshot before a migration fails** (low disk, unwritable backups directory). The server doesn't migrate, says why and where, and leaves the database untouched at its old version. Task 1 holds this with `TestServeRefusesMigrationWithoutSnapshot`.
5. **Vaquero runs under `brew services`.** `vaquero stop` doesn't fight launchd or systemd: it names `brew services stop vaquero`. A stale-server restart only stops the server and waits for the service manager to bring the new one up. Tasks 2 and 4 hold this with `TestStopUnderBrewServices` and `TestStaleRestartUnderBrewServices`.

---

### Task 1: Snapshot before migrating

**Files:** `internal/db/migrate.go` (+test), `cmd/vaquero/main.go` (`openDB`), `cmd/vaquero/serve.go`, `cmd/vaquero/serve_test.go`

**Interfaces:**
- Produces: `db.Pending(db *sql.DB) (applied, newest int64, err error)`; `openDB(ctx context.Context, path, backupDir string, now time.Time, logger *slog.Logger) (w, r *sql.DB, err error)`.

`openDB` opens the writer, then calls `Pending`. When `0 < applied < newest`, it takes a `db.Snapshot` into `backupDir` before migrating. It then logs `"snapshot before migrating" "from" applied "to" newest "path" p` and prunes to `backupKeep`. A fresh database (applied 0) and an up-to-date one get no snapshot. If the snapshot fails, `openDB` returns `the database needs migrating from version %d to %d, but the snapshot before it failed: %v; free some space in %s and start vaquero again`. The migration doesn't run.

- [ ] **Tests first:**
  - `TestPendingReportsVersions`: a database migrated to 3 reports `(3, Newest())`.
  - `TestServeSnapshotsBeforeMigrating`: stage a database with `MigrateTo(5)`, run `serve`, and find exactly one new file in `backups/`. That file opens and reads version 5.
  - `TestServeNoSnapshotWhenCurrent`: covers both a fresh database and a current one.
  - `TestServeRefusesMigrationWithoutSnapshot`: make `backups` a file, not a directory. `serve` then returns the message above, and the database is still at version 5.
- [ ] Watch them fail, implement, gate, and commit: `"Upgrades: snapshot the database before migrating it"`

### Task 2: `vaquero stop` and `vaquero backup`

**Files:** `internal/lockfile/lockfile.go` (+test), `cmd/vaquero/local_cmds.go` (new, +`local_cmds_test.go`), `internal/help/articles/cli.md`, `internal/help/articles/data-and-backups.md`

**Interfaces:**
- Produces:
  - `lockfile.Holder(dir string) (pid int, running bool, err error)`, implemented as Acquire then release.
  - `stopLocal(ctx context.Context, dataDir string, wait time.Duration) (stopped bool, err error)`: sends SIGTERM to the holder, then waits until the lock is free. A `signal func(pid int) error` hook can be swapped in tests.
  - The constant `stopWait = 40 * time.Second`, which covers the 30 s drain.
  - `brewService(home, goos string) bool`: true when `~/Library/LaunchAgents/homebrew.mxcl.vaquero.plist` (darwin) or `~/.config/systemd/user/homebrew.vaquero.service` (linux) exists. Its `home` comes from `os.UserHomeDir`, as a hook in tests.
- Commands:
  - `vaquero stop` prints "Vaquero stopped." or "Vaquero isn't running.". Under brew services it refuses with "Vaquero runs under brew services; run brew services stop vaquero." and exits 1.
  - `vaquero backup list` prints one line per backup, newest first: name, size, time. It prints "No backups yet." when there are none.
  - `vaquero backup restore NAME`:
    1. If a server holds the lock, it refuses with "Stop vaquero first: vaquero stop". Under brew services, the message names `brew services stop vaquero` instead.
    2. It snapshots the current database, so the restore can be undone.
    3. It removes `vaquero.db-wal` and `vaquero.db-shm`.
    4. It copies the backup into place (a temporary file, then a rename) and prints "Restored NAME. The database before it is saved as NEW."

- [ ] **Tests first:**
  - `TestHolderReportsRunning`
  - `TestStopNotRunning`
  - `TestStopEndsBackgroundServer`: start `serve` in a goroutine with a cancelable context; the signal hook cancels it.
  - `TestStopUnderBrewServices`
  - `TestBackupListEmptyAndSorted`
  - `TestRestoreRefusedWhileRunning`
  - `TestRestoreReplacesAndKeepsPrevious`: the restored database has the backup's tickets; the previous database is among the backups; no `-wal` or `-shm` file is left.
  - `TestRestoreUnknownName`: exit 1, "No backup called NAME.".
- [ ] Watch them fail, implement, and add the doc rows. Tick the backlog lines "Restoring a pre-delete backup is manual", "remove -wal and -shm first" and "`vaquero stop`". Gate, then commit: `"CLI: vaquero stop, backup list and backup restore"`

### Task 3: Upgrade fixtures

**Files:** `scripts/fixture/main.go` (new), `testdata/upgrade/README.md`, `internal/db/upgrade_test.go` (new), `Makefile` (target `fixture`)

**Interfaces:**
- `go run ./scripts/fixture -version X` writes `testdata/upgrade/vX.sqlite` and `vX.json`. The JSON holds `{"version": "X", "schema": N, "counts": {table: rows}}`.
- The seed goes through `internal/core` on a fresh database migrated to head. It adds:
  - two projects, one archived;
  - a custom status;
  - a milestone;
  - eight tickets, including a parent with a sub-ticket, one done and one cancelled;
  - tags, comments (one edited, one deleted) and links of all three kinds;
  - an agent holding a live claim;
  - a revoked token;
  - one `mcp_calls` row;
  - a ticket whose title contains the word `lasso`.
- The WAL is checkpointed before writing, so the file stands alone.

- [ ] **Test first:** `TestFixturesMigrateToHead`. For each `testdata/upgrade/*.sqlite` it copies the file to a temporary directory, runs `Open` then `Migrate`, and checks:
  - `PRAGMA integrity_check` is `ok`, and `PRAGMA foreign_key_check` returns no rows;
  - every table's row count equals the JSON's, or exceeds it for tables a later migration seeds (an allowlist in the test, empty for now);
  - `SearchTickets` for `lasso` finds one ticket;
  - `Version` equals `Newest()`.

  With no fixtures present the test fails with "no upgrade fixtures; run make fixture VERSION=…".
- [ ] Watch it fail, then run `make fixture VERSION=0.1.0`. The first fixture is made at head and stands for the first release. Run the test again and watch it pass.
- [ ] Add `testdata/upgrade/README.md`: before each release, run `make fixture VERSION=<release>` and commit it; never edit or delete a fixture. Gate, then commit: `"Upgrades: fixture databases that must migrate to head"`

### Task 4: Restart a stale local server

**Files:** `internal/client/client.go` (+test), `cmd/vaquero/cli.go` (`connect`), `cmd/vaquero/mcp.go`, `cmd/vaquero/cli_local_test.go`

**Interfaces:**
- Consumes: Task 2's `stopLocal` and `brewService`; `client.EnsureLocal`.
- Produces:
  - `client.Older(a, b string) bool`, exported from `older`.
  - `client.IsDev(v string) bool`, true for an empty string or any version with a pre-release part.
  - `(c *Client) ServerVersion() string`: the last answer's `Vaquero-Version`.

For the local profile, `connect` calls `GET /api/v1/me` once. When the CLI isn't a dev build and `Older(serverVersion, ownVersion)` is true, it:
1. prints `vaquero: restarting the local server to run X (it was running Y)` to stderr;
2. calls `stopLocal`;
3. then waits up to `startWait` for the service manager under brew services, or calls `EnsureLocal` otherwise;
4. reads the token again and carries on with the command.

A server that is newer or equal, or a CLI that is a dev build, is left alone. Two commands that race both stop; `stopLocal` on a free lock is a no-op, and EnsureLocal already copes with losing the race to start. `vaquero mcp` gets this through `connect`. The client also skips `UpgradeNeeded` when its own version is a dev build.

- [ ] **Tests first:**
  - `TestStaleLocalServerRestarted`: the server's version is stamped 0.1.0 and the CLI's 0.2.0. Afterwards the lock holder is new, stderr has the line, and the command's output is normal.
  - `TestNewerServerNotRestarted`, `TestDevCLINeverRestarts`
  - `TestConcurrentStaleRestart`: two commands at once, both succeed, and one server is left.
  - `TestStaleRestartUnderBrewServices`: the brew-services hook is true, and the test's fake "service manager" restarts serve after the stop.
  - `TestDevBuildNeverNeedsUpgrade`
- [ ] Watch them fail, implement, gate, and commit: `"CLI: restart a local server older than the CLI"`

### Task 5: Release pipeline

**Files:** `.goreleaser.yaml`, `.github/workflows/release.yml`, `Makefile`, `.gitignore` (`dist/`)

- Makefile: `VERSION` is the exact `v*` tag on HEAD without its "v" (`git describe --tags --exact-match`), otherwise `0.0.0-dev`. This closes the backlog line "Make `VERSION` match its comment".
- `.goreleaser.yaml`:
  - one build of `./cmd/vaquero` with `CGO_ENABLED=0`, for linux and darwin on amd64 and arm64;
  - ldflags stamping `internal/version.{Version,Commit,BuildDate}`;
  - the archive name and contents from Global Constraints, and `checksums.txt`;
  - releases with `draft: false` and `prerelease: auto`;
  - a Homebrew formula in `stazelabs/homebrew-vaquero`, using the token `HOMEBREW_TAP_TOKEN`. The formula's test is `vaquero version`. Its service is `run [opt_bin/"vaquero", "serve"]` with `keep_alive true` and `log_path var/"log/vaquero.log"`. If `goreleaser check` flags `brews` as deprecated, use the form it suggests, as long as that form keeps the service block.
- `release.yml`:
  - `on: workflow_dispatch` with a required `tag` input matching `^v\d+\.\d+\.\d+$`;
  - `permissions: contents: write`;
  - it fails unless `testdata/upgrade/${tag}.sqlite` exists (the tag `v0.1.0` needs `v0.1.0.sqlite`);
  - it runs the gate's steps, then creates and pushes the tag with the workflow token, then runs `goreleaser release --clean` with `GITHUB_TOKEN` and `HOMEBREW_TAP_TOKEN`.

- [ ] Verify: `goreleaser check` reports no errors, and `goreleaser release --snapshot --clean --skip=publish` builds four archives whose names match. The generated formula in `dist/` has the service block, and the darwin/arm64 binary's `version` prints the snapshot version.
- [ ] Gate, then commit: `"Release: goreleaser builds and a Homebrew formula from a manual workflow"`

### Task 6: Docs and the hand check

**Files:**
- `internal/help/articles/installing-and-upgrading.md` (new; section getting-started, order 2; move `data-and-backups` to 3)
- `data-and-backups.md`, `README.md`, `CLAUDE.md`
- the spec's "Upgrades" section
- `docs/BACKLOG.md`, `docs/RELEASING.md` (new)

- [ ] **The article:**
  - installing with `brew install stazelabs/vaquero/vaquero`, or from a release archive on Linux, checking it with `sha256sum -c checksums.txt`;
  - `brew services start vaquero` to keep it running at login;
  - upgrading with `brew upgrade vaquero`; the next command restarts the server;
  - what happens to data on an upgrade: the snapshot before migrating, and the refusal to open a database from a newer Vaquero;
  - restoring a backup.
- [ ] **CLAUDE.md:**
  - the new commands;
  - the stale-server restart;
  - fixtures: never edit or delete one.
- [ ] **The spec:** replace the `vaquero upgrade` and update-notice lines with the Homebrew path, dated 2026-11-20 as the user's decision.
- [ ] **`docs/RELEASING.md`, the user's one-time and per-release steps:**
  - one time: create `stazelabs/homebrew-vaquero` and add a `HOMEBREW_TAP_TOKEN` secret that can write to it.
  - each release: `make fixture VERSION=X`, commit, then run the release workflow with tag `vX`.
- [ ] **Hand check** (scratch data directory and HOME, port 7879, with Homebrew untouched):
  1. Build 0.1.0 and 0.2.0 snapshots.
  2. Run 0.1.0 as a background server with some tickets.
  3. Copy 0.2.0 over the binary, as `brew upgrade` would, and run `vaquero tickets`. It should print the restart line, the server should answer as 0.2.0, and the tickets should be intact.
  4. Boot a staged older database and see the snapshot appear.
  5. Run `backup list` and `backup restore`.
- [ ] Tick the Phase 3a backlog lines. Move the container image to Phase 4. Record "update notice: none; Homebrew's `brew outdated` covers it". Run `make ci`, then commit: `"Docs: installing and upgrading"`

## Left to the user (not tasks)

These have outside effects, so the user does them, using `docs/RELEASING.md`:
- creating the tap repository and its token;
- making the repository public;
- running the first release workflow.

The first public release follows Phase 3b.
