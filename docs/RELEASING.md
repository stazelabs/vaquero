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
3. Check the release page has four archives and `checksums.txt`, and that `brew install stazelabs/vaquero/vaquero` (or `brew upgrade vaquero`) installs it and `vaquero version` prints `0.1.0`.

## Rules

- Never edit or delete a fixture in `testdata/upgrade`; never edit a migration that shipped.
- A schema change that removes something ships in two releases: the addition first, the removal later.
- Check locally before a release with `goreleaser check` (it warns that `brews` is deprecated; that is known) and `goreleaser release --snapshot --clean --skip=publish`.
