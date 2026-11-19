# Releasing Vaquero

Releases are cut by hand from GitHub Actions; nothing runs on push. The workflow (`.github/workflows/release.yml`) checks the tag and the upgrade fixture, runs the full gate, creates and pushes the tag, then runs goreleaser: archives for macOS and Linux (amd64, arm64), `checksums.txt`, the GitHub release, and the Homebrew formula in `stazelabs/homebrew-vaquero`.

## Once

1. Create the public repository `stazelabs/homebrew-vaquero` (empty; goreleaser writes `Formula/vaquero.rb`).
2. Make a fine-grained token with contents read and write on that repository only, and add it to this repository's Actions secrets as `HOMEBREW_TAP_TOKEN`.
3. Make this repository public when the first release is ready (release downloads and the formula need it).

## Each release

1. On `main`, with the gate green, write the upgrade fixture for the version and commit it:

       make fixture VERSION=0.1.0
       git add testdata/upgrade && git commit -m "Upgrades: fixture for 0.1.0"
       git push

2. In Actions, run "release" with the tag `v0.1.0`.
   The workflow runs only from `main`, and checks the fixture's schema matches the newest migration. If a migration landed after the fixture was made, delete the unreleased fixture's two files and run `make fixture` again.
3. Check the release page has four archives and `checksums.txt`, and that `brew install stazelabs/vaquero/vaquero` (or `brew upgrade vaquero`) installs it and `vaquero version` prints `0.1.0`.

## When a release fails

The tag is pushed before goreleaser runs. If goreleaser fails (an expired `HOMEBREW_TAP_TOKEN` is the usual cause), fix the cause and run the workflow again with the same tag: it reuses a tag that points at the same commit. If a partial GitHub release was created, delete it on the releases page first (keep the tag). To give up on a version, delete both the release and the tag, and release the next number.

## Rules

- Never edit or delete a released version's fixture in `testdata/upgrade`; never edit a migration that shipped.
- A schema change that removes something ships in two releases: the addition first, the removal later.
- Check locally before a release with `goreleaser check` (it warns that `brews` is deprecated; that is known) and `goreleaser release --snapshot --clean --skip=publish`.
