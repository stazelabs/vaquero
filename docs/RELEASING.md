# Releasing Vaquero

Releases are cut by hand from GitHub Actions; nothing runs on push. The workflow (`.github/workflows/release.yml`) checks the tag and the upgrade fixture, runs the full gate, creates and pushes the tag, then runs goreleaser: archives for macOS and Linux (amd64, arm64), `checksums.txt` and the GitHub release. Last it writes the Homebrew formula, `Formula/vaquero.rb` in `stazelabs/homebrew-vaquero`, with `scripts/publish-formula.sh`: `scripts/formula` renders `packaging/homebrew/vaquero.rb.tmpl` (with the `service` block for `brew services`) from the version and `dist/checksums.txt`, and `gh api` puts it in the tap with `HOMEBREW_TAP_TOKEN`. The formula workflow (`.github/workflows/formula.yml`) runs the same script for a release that is already out. goreleaser's own `brews` is deprecated, and its successor, casks, has no service block, so the formula is kept here. Change the formula by editing the template and its golden file, `scripts/formula/testdata/vaquero.rb.golden`.

Every action in `.github/workflows` is pinned to a full commit SHA, with its tag in a comment; `TestWorkflowActionsArePinnedBySHA` refuses a tag or branch. To move an action to a newer release, resolve the tag's commit (`gh api repos/OWNER/REPO/git/ref/tags/TAG`, then `gh api repos/OWNER/REPO/git/tags/SHA --jq .object.sha` when the ref's `object.type` is `tag`) and update both the SHA and the comment.

## Once

1. Create the public repository `stazelabs/homebrew-vaquero` (empty; the release workflow writes `Formula/vaquero.rb`).
2. Make a fine-grained token with contents read and write on that repository only, and add it to this repository's Actions secrets as `HOMEBREW_TAP_TOKEN`.
3. Make this repository public when the first release is ready (release downloads and the formula need it).

## Each release

1. On `main`, with the gate green, write the upgrade fixture for the version and commit it:

       make fixture VERSION=0.1.0
       git add testdata/upgrade && git commit -m "Upgrades: fixture for 0.1.0"
       git push

2. In Actions, run "release" with the tag `v0.1.0`.
   The workflow runs only from `main`, and checks the fixture's schema matches the newest migration. If a migration landed after the fixture was made, delete the unreleased fixture's two files and run `make fixture` again.
3. Check the release page has four archives and `checksums.txt`, that the run's "Publish the Homebrew formula" step says `Formula/vaquero.rb updated`, and that `brew tap stazelabs/vaquero` then `brew install vaquero` (or `brew upgrade vaquero`) installs it and `vaquero version` prints `0.1.0`.

## When a release fails

The tag is pushed before goreleaser runs. If goreleaser fails, fix the cause and run the workflow again with the same tag: it reuses a tag that points at the same commit. If a partial GitHub release was created, delete it on the releases page first (keep the tag). To give up on a version, delete both the release and the tag, and release the next number.

If only "Publish the Homebrew formula" fails (an expired `HOMEBREW_TAP_TOKEN` is the usual cause), the release itself is out; don't run the release workflow again, which would fail in goreleaser on the existing release. Renew the token, then in Actions run "formula" from `main` with the same tag, like `v0.1.0`. It downloads the release's `checksums.txt`, renders the formula from the template as it was at the tag, and writes it to the tap; its "Publish the Homebrew formula" step says `Formula/vaquero.rb updated`. A run that finds the tap already holding this release's formula says `The tap already holds the v0.1.0 formula; nothing changed.` and leaves it alone, so running it again is safe.

## Rules

- Never edit or delete a released version's fixture in `testdata/upgrade`; never edit a migration that shipped.
- A schema change that removes something ships in two releases: the addition first, the removal later.
- Check locally before a release with `goreleaser check` and `goreleaser release --snapshot --clean --skip=publish`; `make formula VERSION=X` then prints the formula from the snapshot's `dist/checksums.txt`, with X the version in its archive names (like `0.1.3-SNAPSHOT-abc1234`).
