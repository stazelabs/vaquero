# Phase 3a: Releases and upgrades — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Vaquero can be released and upgraded without losing data. A signed release is built from a manual workflow and published to GitHub and a Homebrew tap. `vaquero upgrade` installs it and restarts the local server. The server snapshots the database before migrating it. Every released database is proven to migrate to head. A backup can be restored. `vaquero service install` keeps Vaquero running at login.

**Architecture:** The new `internal/release` package owns semver comparison, the releases feed, signature checks and replacing the binary. The new `internal/service` package writes and drives a launchd agent or a systemd user unit through an injected command runner. `cmd/vaquero` gains the `upgrade`, `stop`, `service` and `backup` commands, a snapshot before pending migrations, and the daily update notice. goreleaser builds the archives. Its checksum file is signed with an ed25519 key: the private half is a GitHub secret, and the public half is compiled into the binary.

**Tech Stack:** As Phase 2b, plus goreleaser v2 (2.20.2 locally) as a release tool only. Every new piece of code uses only the standard library (`crypto/ed25519`, `archive/tar`, `compress/gzip`).

**Spec:** `docs/superpowers/specs/2026-11-18-vaquero-design.md`, section "Upgrades". Phase 3b (export and import, the Linear importer, the skills adapter, moving the backlog in) gets its own plan.

**Patterns to follow:** `/Users/cstaszak/Stazelabs/projects/oza/.goreleaser.yaml` (archives, checksums, Homebrew formula), read-only. Do not copy its `on: push: tags` trigger.

## Global Constraints

- CLAUDE.md invariants apply. Never add `push` or `pull_request` triggers: the release workflow is `workflow_dispatch` only.
- **Outbound calls (amends the invariant; needs the user's OK at plan review).** The CLI calls the GitHub releases feed in two cases only: `vaquero upgrade`, and a once-a-day update check that `VAQUERO_UPDATE_CHECK=off` turns off. The server never makes outbound calls. Vaquero sends no telemetry. A request sends only `User-Agent: vaquero/{version}`.
- Supported platforms: darwin and linux, amd64 and arm64. Windows is not built.
- Feed: `https://api.github.com/repos/stazelabs/vaquero/releases/latest`. Archive name: `vaquero_{version}_{os}_{arch}.tar.gz`, holding `vaquero`, `LICENSE`, `NOTICE` and `README.md`. The checksum file is `checksums.txt`, signed as `checksums.txt.sig`: a base64 ed25519 signature.
- A binary is never replaced before its archive's checksum matches a signed `checksums.txt`.
- A dev build is version `0.0.0-dev`. It never shows the update notice or the upgrade-needed message. `vaquero upgrade` treats it as older than any release.
- Snapshots before a migration go to `{data dir}/backups`, with the same names and keep count (`backupKeep`, 7) as the snapshots taken before a project delete.
- The commands that work without a server (`stop`, `backup`, `service`, `upgrade`) use the data directory from `VQ_DATA_DIR`, or the default one.
- Copy follows `.claude/rules/copy-voice.md`. New commands get rows in the `cli` help article (tests enforce this).
- Gate steps run separately and are judged by exit code; `make ci` ends each task. Commits are `{Area}: {summary}` with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A download is cut short, tampered with or signed with the wrong key.** The installed binary is untouched, and the message says the release couldn't be verified. Task 5 holds this as `TestReplaceRefusesBadSignature` and `TestReplaceRefusesTruncatedArchive`.
2. **The binary was installed by Homebrew, or sits in a directory the user can't write.** `vaquero upgrade` refuses before downloading anything and names what to do instead (`brew upgrade vaquero`, or the path it couldn't write). Tasks 5 and 7 hold this with `TestManagedByHomebrew` and `TestUpgradeUnwritableDir`.
3. **The snapshot before a migration fails** (low disk, unwritable backups directory). The server doesn't migrate, says why and where, and leaves the database untouched at its old version. Task 1 holds this with `TestServeRefusesMigrationWithoutSnapshot`.
4. **An upgrade runs while the local server is up, either as a background process or as a service.** The old server stops cleanly, the new binary serves, and the CLI prints the version now running. Task 7 holds this with `TestUpgradeRestartsBackgroundServer` and `TestUpgradeRestartsService`.
5. **The releases feed is offline, slow or rate-limited during the daily check.** The command's output and exit code are unaffected and no message appears. The attempt still counts, so the next try is a day later. Task 8 holds this with `TestUpdateCheckOfflineIsSilent`.

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
  - `stopLocal(ctx context.Context, dataDir string, wait time.Duration) (stopped bool, err error)`: sends SIGTERM to the holder, then waits until the lock is free.
  - The constant `stopWait = 40 * time.Second`, which covers the 30 s drain.
- Commands:
  - `vaquero stop` prints "Vaquero stopped." or "Vaquero isn't running.". When a service is installed (Task 6 adds the check; until then it is never true), it refuses with "Vaquero runs as a service; use vaquero service uninstall to stop it." and exits 1.
  - `vaquero backup list` prints one line per backup, newest first: name, size, time. It prints "No backups yet." when there are none.
  - `vaquero backup restore NAME`:
    1. If a server holds the lock, it refuses with "Stop vaquero first: vaquero stop".
    2. It snapshots the current database, so the restore can be undone.
    3. It removes `vaquero.db-wal` and `vaquero.db-shm`.
    4. It copies the backup into place (a temporary file, then a rename) and prints "Restored NAME. The database before it is saved as NEW."

- [ ] **Tests first:**
  - `TestHolderReportsRunning`
  - `TestStopNotRunning`
  - `TestStopEndsBackgroundServer`: start `serve` through `run` in a goroutine with a signal-aware context, then stop it. To make it testable, `stopLocal` takes a `signal func(pid int) error` hook in the test file.
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

  With no fixtures present the test fails with "no upgrade fixtures; run make fixture VERSION=…", so the directory can't go empty unnoticed.
- [ ] Watch it fail, then run `make fixture VERSION=0.1.0`. The first fixture is made at head and stands for the first release. Run the test again and watch it pass.
- [ ] Add `testdata/upgrade/README.md`: before each release, run `make fixture VERSION=<release>` and commit it; never edit or delete a fixture. Gate, then commit: `"Upgrades: fixture databases that must migrate to head"`

### Task 4: Signed release pipeline

**Files:** `internal/release/version.go`, `internal/release/sign.go` (+tests), `scripts/releasekey/main.go`, `scripts/sign/main.go`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `Makefile`, `internal/client/client.go`, `coverage-floors.txt` (`internal/release 80`)

**Interfaces:**
- Produces:
  - `release.Older(a, b string) bool`: moved from `client.older`; the client calls it. It also adds `release.IsDev(v string) bool`, true for an empty string or any version with a pre-release part.
  - `release.PublicKey string`: a base64 ed25519 public key, as a `var` so a test build can stamp one with `-ldflags -X`. It is empty until the user runs `releasekey`.
  - `release.Verify(pubB64 string, msg, sig []byte) error`: returns `ErrUnsigned` when the key is empty and `ErrBadSignature` otherwise.
  - `release.ChecksumFor(checksums []byte, name string) (sha256hex string, err error)`
- The tools:
  - `go run ./scripts/releasekey` prints the public key to commit into `internal/release/key.go`. It writes the private key, base64 encoded, to a 0600 file named by `-out`, for the `VAQUERO_SIGNING_KEY` secret.
  - `go run ./scripts/sign IN OUT` reads `VAQUERO_SIGNING_KEY` and writes the base64 signature of IN to OUT.
- Makefile: `VERSION` is the exact `v*` tag on HEAD without its "v" (`git describe --tags --exact-match`), otherwise `0.0.0-dev`. This closes the "Make VERSION match its comment" backlog line.
- The client skips `UpgradeNeeded` when its own version is a dev build.
- `.goreleaser.yaml`:
  - one build of `./cmd/vaquero` with `CGO_ENABLED=0`, for linux and darwin on amd64 and arm64;
  - ldflags stamping `internal/version.{Version,Commit,BuildDate}`;
  - the archive name and contents from Global Constraints, and `checksums.txt`;
  - `signs` with `cmd: go`, `args: [run, ./scripts/sign, "${artifact}", "${signature}"]`, `artifacts: checksum`, `signature: "${artifact}.sig"`;
  - a Homebrew formula in `stazelabs/homebrew-vaquero`, using the token `HOMEBREW_TAP_TOKEN` and the test `vaquero version`. If `goreleaser check` flags `brews` as deprecated, use what it suggests;
  - releases as drafts off (`draft: false`), and `prerelease: auto`.
- `release.yml`:
  - `on: workflow_dispatch` with a required `tag` input matching `^v\d+\.\d+\.\d+$`;
  - `permissions: contents: write`;
  - it fails unless `testdata/upgrade/${tag}.sqlite` exists (the tag `v0.1.0` needs `v0.1.0.sqlite`);
  - it runs the gate's steps, then creates and pushes the tag with the workflow token, then runs `goreleaser release --clean` with `GITHUB_TOKEN`, `HOMEBREW_TAP_TOKEN` and `VAQUERO_SIGNING_KEY`.

- [ ] **Tests first:**
  - `TestOlder`, moved from the client with dev and pre-release cases added;
  - `TestSignVerifyRoundTrip`, `TestVerifyWrongKey`, `TestVerifyUnsigned`;
  - `TestChecksumFor`, covering a hit, a miss and a malformed line;
  - client `TestDevBuildNeverNeedsUpgrade`.
- [ ] Watch them fail, then implement.
- [ ] Verify: `goreleaser check` reports no errors, and `goreleaser release --snapshot --clean --skip=publish,sign` builds four archives whose names match. Run the darwin/arm64 binary's `version` and check it prints the snapshot version. `dist/` stays out of git.
- [ ] Gate, then commit: `"Release: signed goreleaser builds from a manual workflow"`

### Task 5: Fetch, verify and replace

**Files:** `internal/release/feed.go`, `internal/release/install.go` (+tests)

**Interfaces:**
- Consumes: Task 4's `Verify`, `ChecksumFor` and `Older`.
- Produces:
  - `const FeedURL`
  - `type Release struct { Version string; Assets map[string]string }`, mapping asset name to download URL.
  - `Latest(ctx, hc *http.Client, feed string) (Release, error)`: reads `tag_name` and `assets[].{name,browser_download_url}`. A 404 is `ErrNoRelease`; any other non-200 is an error naming the status.
  - `ArchiveName(version, goos, goarch string) string`
  - `Fetch(ctx, hc, rel Release, goos, goarch, pub string) (binary []byte, err error)`: downloads `checksums.txt`, its `.sig` and the archive, each capped at 200 MB. It verifies the signature, then the checksum, then extracts `vaquero` from the tar.gz.
  - `Managed(exe string) (by string, ok bool)`: true with `"Homebrew"` when the resolved path contains `/Cellar/` or starts with a Homebrew prefix (`/opt/homebrew/`, `/usr/local/Cellar/`, `/home/linuxbrew/`).
  - `Replace(exe string, bin []byte) error`: resolves symlinks, writes `{exe}.new` in the same directory with the old file's mode, then renames it over. On failure it removes the `.new` file.

- [ ] **Tests first** (against an `httptest` server):
  - `TestLatestParsesFeed`, `TestLatestNoRelease`
  - `TestFetchVerifies`
  - `TestReplaceRefusesBadSignature`: the installed file is unchanged.
  - `TestReplaceRefusesChecksumMismatch`
  - `TestReplaceRefusesTruncatedArchive`
  - `TestFetchNoAssetForPlatform`: "There is no build of Vaquero X for linux/riscv64."
  - `TestManagedByHomebrew`
  - `TestReplaceKeepsModeAndFollowsSymlink`
  - `TestReplaceUnwritableLeavesNothing`
- [ ] Watch them fail, implement, gate, and commit: `"Release: fetch, verify and replace the binary"`

### Task 6: `vaquero service`

**Files:** `internal/service/service.go` (+test), `cmd/vaquero/service.go` (+test), `cmd/vaquero/local_cmds.go` (the stop refusal), `internal/help/articles/cli.md`, `coverage-floors.txt` (`internal/service 80`)

**Interfaces:**
- Produces:
  - `type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)`
  - `type Manager struct { GOOS, Home, Exe, DataDir, Addr string; UID int; Run Runner }`
  - `(m Manager) Unit() (path string, body []byte)`
  - `Installed() bool`
  - `Install(ctx) error`
  - `Uninstall(ctx) error`
  - `Restart(ctx) error`
  - `Status(ctx) (string, error)`
- darwin: `~/Library/LaunchAgents/com.stazelabs.vaquero.plist`.
  - The plist sets `ProgramArguments` to [Exe, serve], `EnvironmentVariables` `VQ_DATA_DIR` and `VQ_ADDR`, and `RunAtLoad` and `KeepAlive` to true. Both log paths are `{DataDir}/vaquero.log`.
  - Install runs `launchctl bootstrap gui/UID path`. Uninstall runs `launchctl bootout gui/UID/com.stazelabs.vaquero`, then deletes the plist. Restart runs `launchctl kickstart -k gui/UID/com.stazelabs.vaquero`.
- linux: `~/.config/systemd/user/vaquero.service`.
  - The unit sets `ExecStart=Exe serve`, the two `Environment=` lines and `Restart=on-failure`.
  - Install runs `systemctl --user daemon-reload`, then `enable --now vaquero.service`. Uninstall runs `disable --now`, removes the unit, then runs `daemon-reload`. Restart runs `restart`.
- Commands:
  - `vaquero service install` stops a background server first (Task 2's `stopLocal`) and prints "Vaquero will now run at login. It is running at URL.". It waits for `/healthz`, and reinstalling over an existing install rewrites the unit.
  - `vaquero service uninstall` prints "Vaquero no longer runs at login.".
  - `vaquero service status` prints "installed, running", "installed, stopped" or "not installed".
  - `vaquero stop` refuses while a service is installed.

- [ ] **Tests first:**
  - `TestLaunchdUnit`, `TestSystemdUnit`: golden bodies with paths and environment variables.
  - `TestInstallRunsManager`: the fake runner records `launchctl bootstrap gui/501 …`.
  - `TestUninstallRemovesUnit`, `TestRestartCommand`
  - `TestUnsupportedOS`: "vaquero service supports macOS and Linux."
  - CLI: `TestServiceStatusNotInstalled`, `TestStopRefusedWhenServiceInstalled`
- [ ] Watch them fail, implement, gate, and commit: `"CLI: vaquero service install, uninstall and status"`. The hand check never runs a real install on the user's machine; tests run everything against a temporary HOME and a fake runner.

### Task 7: `vaquero upgrade`

**Files:** `cmd/vaquero/upgrade.go` (+test), `internal/help/articles/cli.md`

**Interfaces:**
- Consumes: Task 5's `Latest`, `Fetch`, `Managed` and `Replace`; Task 2's `stopLocal`; Task 6's `Manager`; `client.EnsureLocal`.
- Produces: `release.FeedOverride string`, a ldflags variable. When it is `"allow"`, the CLI reads the feed from `VAQUERO_RELEASES_URL`; a real release leaves it empty and ignores the variable. Task 8's check uses the same feed. Test `TestFeedOverrideIgnoredInReleases`.
- Test hooks: `upgradeFeed` (the feed URL), `upgradeExe` (the path to replace) and `serviceRunner`.

`vaquero upgrade [--check]`:
1. Report up to date. When the current version isn't `Older` than the latest, print "Vaquero X is up to date." and exit 0.
2. Check only. `--check` prints "Vaquero Y is available (you have X). Run vaquero upgrade." and exits 0.
3. Refuse when Homebrew manages the binary. Print "Homebrew manages this vaquero; run brew upgrade vaquero." and exit 1.
4. Check the directory is writable before downloading. If it isn't, refuse with "Can't replace PATH: DIR isn't writable. Reinstall Vaquero, or run vaquero upgrade with permission to write there." and exit 1.
5. Fetch and replace the binary.
6. Restart whatever is running:
   - a service, through `Manager.Restart`;
   - a background server, through `stopLocal` and then `EnsureLocal`, using the new exe;
   - nothing, in which case start nothing.
7. Print "Upgraded Vaquero from X to Y." When a server was restarted, also print "The server is running Y." with the version read from the `Vaquero-Version` header on `GET /api/v1/me`.
8. An unsigned build (`PublicKey` empty) refuses with "This build can't verify releases; install the new version from the releases page." and exits 1.

- [ ] **Tests first.** A test feed serves archives signed with a test key, built in the test from a stub binary.
  - `TestUpgradeUpToDate`, `TestUpgradeCheck`
  - `TestUpgradeHomebrewRefused`, `TestUpgradeUnwritableDir`, `TestUpgradeUnsignedBuild`
  - `TestUpgradeReplacesBinary`
  - `TestUpgradeRestartsBackgroundServer`: a real `serve` in a goroutine; afterwards the lock is held again by a new process. EnsureLocal is a hook, because the stub binary isn't a real server.
  - `TestUpgradeRestartsService`: the fake runner sees `kickstart -k`.
- [ ] Watch them fail, implement, gate, and commit: `"CLI: vaquero upgrade"`

### Task 8: The update notice

**Files:** `internal/client/update.go` (+test), `cmd/vaquero/cli.go` (after a command finishes)

**Interfaces:**
- Produces: `type UpdateCheck struct { Path string; Now func() time.Time; Fetch func(ctx context.Context) (string, error) }` and `(u UpdateCheck) Notice(ctx context.Context, current string) string`.
- The cache lives at `{user config dir}/vaquero/update.json`, mode 0600, holding `{"checked_at", "latest"}`. A fetch runs only when `checked_at` is more than 24 hours old, under a 2 s timeout. A failed fetch still records `checked_at`.
- `Notice` returns "" for a dev build, when `VAQUERO_UPDATE_CHECK=off`, and when the latest version isn't newer. Otherwise it returns "Vaquero Y is available (you have X). Run vaquero upgrade.".
- `run` prints a non-empty notice to stderr after the command's output. It does this only for API commands without `--json`: never for `serve`, `mcp`, `upgrade`, `docs`, `version` or `help`.

- [ ] **Tests first:**
  - `TestNoticeWhenNewer`
  - `TestNoticeCachedForADay`: a second call within 24 hours doesn't fetch.
  - `TestUpdateCheckOfflineIsSilent`: the fetch errors, the notice is empty and `checked_at` is recorded.
  - `TestNoticeOff`, `TestNoticeDevBuild`
  - CLI: `TestNoticeNotOnJSONOrMCP`
- [ ] Watch them fail, implement, gate, and commit: `"CLI: a once-a-day update notice"`

### Task 9: Docs and the hand check

**Files:**
- `internal/help/articles/installing-and-upgrading.md` (new; section getting-started, order 2; move `data-and-backups` to 3)
- `data-and-backups.md`, `README.md`, `CLAUDE.md`, `.claude/rules/security.md`
- `docs/BACKLOG.md`, `docs/RELEASING.md` (new)

- [ ] **The article:**
  - installing, from Homebrew or from a release archive;
  - `vaquero service install`;
  - `vaquero upgrade` and what it checks;
  - the update notice and how to turn it off;
  - what happens to data on upgrade (the snapshot, the newer-database refusal);
  - restoring a backup.
- [ ] **CLAUDE.md:**
  - the outbound-calls invariant, worded as in Global Constraints;
  - package map entries for `internal/release` and `internal/service`, and the new commands;
  - fixtures: never edit or delete one.
- [ ] **`docs/RELEASING.md`, the user's one-time and per-release steps:**
  - one time: run `releasekey`, then commit the public key; add the secrets `VAQUERO_SIGNING_KEY` and `HOMEBREW_TAP_TOKEN`; create `stazelabs/homebrew-vaquero`.
  - each release: `make fixture VERSION=X`, commit, then run the release workflow with tag `vX`.
- [ ] **Hand check** (scratch data directory, scratch HOME, scratch port):
  1. Build `0.1.0` and `0.2.0` with `goreleaser release --snapshot` and a test key stamped into both through ldflags.
  2. Serve the 0.2.0 archives and a feed JSON from a local `python3 -m http.server`, pointing the CLI at it through `VAQUERO_RELEASES_URL`. Both builds are stamped with `release.FeedOverride=allow` (Task 7).
  3. Run the 0.1.0 binary as a background server with some tickets, then run `vaquero upgrade`. Check the binary reports 0.2.0, the server answers as 0.2.0, the tickets are intact, and no snapshot was taken (no migration was pending).
  4. Stage an older database and boot it, checking a snapshot appears. Then run `backup list` and `backup restore`.
- [ ] Tick the Phase 3a backlog lines; add a Phase 3a deferred section if needed. Run `make ci`, then commit: `"Docs: installing and upgrading"`

## Left to the user (not tasks)

These have outside effects, so the user does them, using `docs/RELEASING.md`:
- generating the signing key and storing its secret;
- creating the tap repository and its token;
- making the repository public;
- running the first release workflow.

The first public release follows Phase 3b, so the backlog moves into Vaquero before anyone else installs it.
