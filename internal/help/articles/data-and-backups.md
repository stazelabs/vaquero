---
title: Your data and backups
section: getting-started
summary: Where Vaquero keeps its data, what each file is for, and how to restore a backup.
order: 2
---
## The data directory

Vaquero keeps everything in one directory, readable only by you:

| System | Directory |
|---|---|
| macOS | `~/Library/Application Support/vaquero` |
| Linux | `$XDG_DATA_HOME/vaquero`, or `~/.local/share/vaquero` |

Set `VQ_DATA_DIR` to use another. The Admin page shows the one in use.

## What is in it

| File | What it is |
|---|---|
| `vaquero.db` (with `-wal` and `-shm` beside it while running) | The database: every project, ticket, comment and change |
| `vaquero.lock` | Held while a server runs, so two servers never share one directory |
| `token` | The API token the command line uses on this machine; see [API tokens](/help/api-tokens) |
| `vaquero.log` | The output of a server the command line started in the background |
| `backups/` | Snapshots taken before a project is deleted |

Only one server can use a data directory at a time. A second one stops at once and names the process holding the lock.

## Backups

Deleting a project takes a snapshot of the whole database first, into `backups/`. If the snapshot can't be written (for example, the disk is full), nothing is deleted. The newest seven snapshots are kept.

For your own backups, copy the data directory while no server is running, or copy a snapshot.

## Restoring a snapshot

1. Stop the server (stop `vaquero serve`, or end the background process).
2. Move `vaquero.db` and any `vaquero.db-wal` and `vaquero.db-shm` files aside.
3. Copy the snapshot into the data directory as `vaquero.db`.
4. Start Vaquero again.

A database written by a newer Vaquero is refused by an older one, with a message saying so; upgrade Vaquero rather than editing the file.
