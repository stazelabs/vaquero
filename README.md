# Vaquero

Vaquero herds the chaos.

Vaquero is an agent-native, local-first, lightweight ticket tracker. It ships as one Go binary: run it on your laptop for yourself, or deploy the same binary to cheap cloud hosting for a small team. People use the web UI or the CLI; agents use MCP, with a ready queue, claims with expiring leases and a full activity log so several of them can share one backlog without colliding. It costs nothing to run for one person, and only the hosting bill when shared.

Status: v0.1.8, an early release. Local mode, for one person on their own machine, is ready to use, and Vaquero tracks its own work in it. Hosted mode, for a small team on cloud hosting, comes in a later release.

## Install

    brew tap stazelabs/vaquero
    brew install vaquero
    vaquero docs welcome

(`brew install stazelabs/vaquero/vaquero` does the first two in one step.)

Upgrade with `brew upgrade vaquero`; the next command restarts the server on the new version, after snapshotting the database if its layout changes. Release archives for macOS and Linux, with `checksums.txt`, are on the releases page. `vaquero docs installing-and-upgrading` has the details.

## Build from source

Requires Go 1.27 and Node (for the build-time stylesheet only).

    make tools   # one time
    cp .env.local.example .env.local   # keeps dev data in ./data
    make dev     # hot reload at http://127.0.0.1:7879 (a real `vaquero serve` uses 7878)
    make test
    make ci      # the full gate: generate, format, vet, lint, test, coverage floors

Run `make` with no target to list every target. A plain `vaquero serve` keeps its data in `~/Library/Application Support/vaquero` on macOS and `~/.local/share/vaquero` on Linux; set `VQ_DATA_DIR` to change it.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): run `make ci` before a pull request; a maintainer runs the gate on the branch before merging.

## Releasing

See docs/RELEASING.md.

## Documentation

The help is built in: open Help in the web app (`/help`), or run `vaquero docs` to list the articles and `vaquero docs cli` for the command line. The articles are in `internal/help/articles`.

## Licence

Apache 2.0. See LICENSE.
