---
title: Your data and backups
section: getting-started
summary: Where Vaquero keeps its data, what each file is for, how to export and import it, and how to restore a backup.
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
| `backups/` | Snapshots taken before a project is deleted, before an upgrade changes the database, and before a restore or an import |

Only one server can use a data directory at a time. A second one stops at once and names the process holding the lock.

## Backups

Vaquero takes a snapshot of the whole database, into `backups/`, before anything that can't be undone otherwise:

- deleting a project;
- starting a newer Vaquero that changes the database's layout (an upgrade);
- restoring another snapshot;
- importing an export.

If the snapshot can't be written (for example, the disk is full), nothing happens: the project stays, the upgraded server doesn't start and says why, or the restore stops. The newest seven snapshots are kept. `vaquero backup list` shows them.

Snapshots hold attachments only with the default `sqlite` storage; files kept in a directory or an S3 bucket are yours to back up. See [Attachments](/help/attachments#backups).

For your own backups, copy the data directory while no server is running, or copy a snapshot.

## Exporting everything

An export is one file holding all of your data, to keep, to move to another machine, or to read with other tools:

    vaquero export vaquero.tar.gz

It works while the server runs and doesn't stop anyone working. Only the owner can export; an agent can't. `vaquero export -` writes the file to standard output instead.

The file is a `.tar.gz`. Inside, `export.json` holds every project, status, milestone, tag, ticket (with its history fields and version), comment (deleted ones included, marked as deleted), link, attachment record, actor and change in the activity history, as plain JSON. Each attachment's bytes follow as `attachments/ID`, wherever they are stored. Two exports of unchanged data are identical apart from the time and version at the top of `export.json`.

Left out on purpose:

- API tokens, which belong to this server only (the activity history still records each token's name and the first 9 characters of the token that the Admin page shows, never the token itself);
- the MCP activity log;
- live claims: tickets keep their status and assignee, but no claim;
- the settings that describe this server rather than its data, such as where attachments are kept.

If an attachment's file is missing from storage, or isn't the size its record says (for example, someone deleted or changed it in the directory or bucket), the export stops, names the attachment and its ticket, and writes nothing. The file is written under a temporary name and renamed when complete, so a failed or interrupted export never leaves a partial file behind.

## Importing an export

`vaquero import` puts an export back, into an instance with no projects and no tickets: a new machine, or a data directory that has only been started. Stop the server first; import works on the data directory directly and needs no network:

    vaquero stop
    vaquero import vaquero.tar.gz

An instance that has been served but holds no projects or tickets still has its owner, its token and a few changes in its history. Those are replaced by the export's. An instance with any project or ticket is refused; import never merges.

Import is all or nothing. It checks the whole file first, then builds a new database beside the current one, `vaquero.import.db`, and checks that too. Only then does it put the new database in place, saving the current one as a snapshot in `backups/` first, and says which snapshot that is. If anything fails on the way, the new database and every attachment file it stored are deleted, and the current database is as it was. If the very last step, putting the new database in place, fails after the current one was saved, the message says where the current one is and how to put it back.

An export from a newer Vaquero is refused; upgrade Vaquero and import again. An export from an older one is read into the current layout.

Attachment files go where `VQ_BLOB_STORE` says, in the shell you run `vaquero import` from (see [Attachments](/help/attachments#where-the-bytes-are-kept)), and the message names that storage. The server must start with the same setting. If Vaquero runs under `brew services`, its environment is the service's, not your shell's: if the two differ, the server refuses to start, names both, and changes nothing. Set them to the same storage and start it again.

Tokens are not part of an export. Import removes the `token` file, so the next start makes a new token for the imported owner, and the command line on this machine picks it up. Any other token for the old instance, such as one saved in a profile for another machine or an agent, stops working: make a new one with `vaquero token create`.

## Restoring a snapshot

Stop the server, then restore:

    vaquero stop
    vaquero backup list
    vaquero backup restore vaquero-20261118-120000.sqlite

The database as it was is saved as a new snapshot first, so a restore can itself be undone. The next command starts Vaquero on the restored database.

A database written by a newer Vaquero is refused by an older one, with a message saying so; upgrade Vaquero rather than editing the file.

A copied data directory shares the original's instance id. Never run a copy against the same attachment directory or bucket as the original: the two would write and sweep each other's files.
