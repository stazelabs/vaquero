# Vaquero

Vaquero herds the chaos.

Vaquero is an agent-native, local-first, lightweight ticket tracker. It ships as one Go binary: run it on your laptop for yourself, or deploy the same binary to cheap cloud hosting for a small team. People use the web UI or the CLI; agents use MCP, with a ready queue, claims with expiring leases and a full activity log so several of them can share one backlog without colliding. It costs nothing to run for one person, and only the hosting bill when shared.

Status: early development. Not ready for use.

## Build from source

Requires Go 1.27 and Node (for the build-time stylesheet only).

    make tools   # one time
    make dev     # http://127.0.0.1:7878

## Licence

Apache 2.0. See LICENSE.
