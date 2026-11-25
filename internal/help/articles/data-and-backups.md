---
title: Your data and backups
section: getting-started
summary: Where Vaquero keeps its data, what each file is for, and how to restore a backup.
order: 3
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
| `backups/` | Snapshots taken before a project is deleted, before an upgrade changes the database, and before a restore |

Only one server can use a data directory at a time. A second one stops at once and names the process holding the lock.

## Backups

Vaquero takes a snapshot of the whole database, into `backups/`, before anything that can't be undone otherwise:

- deleting a project;
- starting a newer Vaquero that changes the database's layout (an upgrade);
- restoring another snapshot.

If the snapshot can't be written (for example, the disk is full), nothing happens: the project stays, the upgraded server doesn't start and says why, or the restore stops. The newest seven snapshots are kept. `vaquero backup list` shows them.

Snapshots hold attachments only with the default `sqlite` storage; files kept in a directory or an S3 bucket are yours to back up. See [Attachments](/help/attachments#backups).

For your own backups, copy the data directory while no server is running, or copy a snapshot.

## Exporting everything

An export is one file holding all of your data, to keep, to move to another machine, or to read with other tools:

    vaquero export vaquero.tar.gz

It works while the server runs and doesn't stop anyone working. Only the owner can export; an agent can't. `vaquero export -` writes the file to standard output instead.

The file is a `.tar.gz`. Inside, `export.json` holds every project, status, milestone, tag, ticket (with its history fields and version), comment (deleted ones included, marked as deleted), link, attachment record, actor and change in the activity history, as plain JSON. Each attachment's bytes follow as `attachments/ID`, wherever they are stored. Two exports of unchanged data are identical apart from the time and version at the top of `export.json`.

Left out on purpose:

- API tokens, which belong to this server only;
- the MCP activity log;
- live claims: tickets keep their status and assignee, but no claim;
- the settings that describe this server rather than its data, such as where attachments are kept.

If an attachment's file is missing from storage (for example, someone deleted it from the directory or bucket), the export stops, names the attachment and its ticket, and writes nothing. The file is written under a temporary name and renamed when complete, so a failed or interrupted export never leaves a partial file behind.

## Restoring a snapshot

Stop the server, then restore:

    vaquero stop
    vaquero backup list
    vaquero backup restore vaquero-20261118-120000.sqlite

The database as it was is saved as a new snapshot first, so a restore can itself be undone. The next command starts Vaquero on the restored database.

A database written by a newer Vaquero is refused by an older one, with a message saying so; upgrade Vaquero rather than editing the file.
