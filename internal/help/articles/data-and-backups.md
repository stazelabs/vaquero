---
title: Your data and backups
section: getting-started
summary: Where Vaquero keeps its data, what each file is for, how to export and import it, how to import issues from Linear, how to restore a backup, how to move attachments to other storage, and how to compact the database.
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
| `vaquero.log` | The output of a server the command line started in the background; at 10 MB it moves to `vaquero.log.1`, replacing the one before. `vaquero logs` prints its end. Under `brew services` the log is elsewhere; see [Installing and upgrading](/help/installing-and-upgrading#the-servers-log) |
| `move.pending` | Present only while a move of attachments to other storage is unfinished; see [Moving attachments](#moving-attachments-to-other-storage) |
| `backups/` | Snapshots taken before a project is deleted, before an upgrade changes the database, and before a restore, an import, a compaction or a move of attachments to other storage |

Only one server can use a data directory at a time. A second one stops at once and names the process holding the lock.

## Backups

Vaquero takes a snapshot of the whole database, into `backups/`, before anything that can't be undone otherwise:

- deleting a project;
- starting a newer Vaquero that changes the database's layout (an upgrade);
- restoring another snapshot;
- importing an export;
- compacting the database;
- moving attachments to other storage.

If the snapshot can't be written (for example, the disk is full), nothing happens: the project stays, the upgraded server doesn't start and says why, or the restore, import, compaction or move stops. The newest seven snapshots of each of those kinds are kept, so a run of project deletes never pushes out the snapshot taken before an upgrade; the kind is in the file name (`vaquero-predelete-…`). Beyond those seven, the newest snapshot taken before an upgrade from each older database layout is kept too, so the last copy of a layout is never removed. Snapshots from older releases have plain names and are never removed. `vaquero backup list` shows them all.

A Vaquero older than 0.1.2 doesn't list snapshots whose names carry a kind. To restore one with it, copy the file over `vaquero.db` by hand while no server is running.

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

If an attachment's file is missing from storage, or isn't the size its record says (for example, someone deleted or changed it in the directory or bucket), the export stops, names the attachment and its ticket, and writes nothing. So does an export whose `export.json` would be larger than 256 MB, the most `vaquero import` reads: Vaquero never writes an export it can't import. `HEAD /api/v1/export` makes the same checks without sending the file. The file is written under a temporary name and renamed when complete, so a failed or interrupted export never leaves a partial file behind.

## Importing an export

`vaquero import` puts an export back, into an instance with no projects and no tickets: a new machine, or a data directory that has only been started. Stop the server first; import works on the data directory directly and needs no network:

    vaquero stop
    vaquero import vaquero.tar.gz

An instance that has been served but holds no projects or tickets still has its owner, its token and a few changes in its history. Those are replaced by the export's. An instance with any project or ticket is refused; import never merges.

Import is all or nothing. It checks the whole file first, unpacking the attachment files into a folder in the data directory (`import-spool-…`, removed when the import ends; it needs room for every attachment and says so when there isn't), then builds a new database beside the current one, `vaquero.import.db`, and checks that too. Only then does it put the new database in place, saving the current one as a snapshot in `backups/` first, and says which snapshot that is. If anything fails on the way, the new database and every attachment file it stored are deleted, and the current database is as it was. If the very last step, putting the new database in place, fails after the current one was saved, the message says where the current one is and how to put it back. An import stopped outright (the machine lost power, or the process was killed) can leave attachment files in a directory or bucket; it notes them in `import.pending` in the data directory before storing any, and the next `vaquero import` or server start deletes them. If they can't be deleted, the import stops and the server logs a warning, naming the files to remove by hand and the record to delete after.

An export from a newer Vaquero is refused; upgrade Vaquero and import again. An export from an older one is read into the current layout.

Attachment files go where `VQ_BLOB_STORE` says, in the shell you run `vaquero import` from or in `vaquero.env` (see [Attachments](/help/attachments#where-the-bytes-are-kept)), and the message names that storage. Before it reads the file, import writes and deletes a small test object there, so wrong credentials or a read-only bucket stop it at once, with nothing changed. The server must start with the same setting. If Vaquero runs under `brew services`, its environment is the service's, not your shell's: if the two differ, the server refuses to start, names both, and changes nothing. Put the storage settings in `vaquero.env` in the data directory, which the service reads (see [Installing and upgrading](/help/installing-and-upgrading#settings-for-the-server)), and start it again.

Tokens are not part of an export. Import removes the `token` file, so the next start makes a new token for the imported owner, and the command line on this machine picks it up. Any other token for the old instance, such as one saved in a profile for another machine or an agent, stops working: make a new one with `vaquero token create`.

## Importing from Linear

`vaquero import linear FILE.csv` makes tickets from Linear's issue export, the CSV file Linear emails a link to (Linear's documentation: [Exporting data](https://linear.app/docs/exporting-data)). Unlike `vaquero import`, it runs against the server like any other command, so the server can keep running and other projects stay as they are:

    vaquero import linear issues.csv
    vaquero import linear issues.csv --project OPS

Each issue keeps its team key and number: ENG-123 becomes ticket ENG-123, so branch names and commit references that name it still resolve. Each team in the file becomes a project with the team's key and name. With `--project KEY`, a file with one team goes into that project instead, keeping the numbers. The project is new, or one with no tickets; a project with tickets, an archived one, or one whose numbering is already past the file's first number (its tickets moved to another project) is refused. Numbering goes on after the highest imported number, and the numbers the file skips are never given. The import runs as you, the owner: only the owner chooses a ticket's number, so it refuses `--agent` and `VAQUERO_AGENT`.

What is imported, from the columns Linear documents:

- `ID`: the key and number. `Title` and `Description`: the ticket's title and description.
- `Status`: a status the project already has by that name, whatever its case; Linear's `Canceled` is the project's cancelled status. Any other name becomes a new status: `Triage` in backlog, `Duplicate` in cancelled, and the rest in the category the issues' `Started`, `Completed` and `Canceled` dates show (none of them is todo).
- `Priority`: No priority, Urgent, High, Medium and Low are the same priorities here.
- `Labels`: each label becomes a tag.

Comments, relations and attachments aren't in the CSV, so they aren't imported, and the summary says so. Neither are the other documented columns, such as the assignee, cycle, project, estimate and dates; the summary lists the ones the file had, and any column Linear doesn't document as unknown. Archived issues come in as ordinary tickets, with their status. Tickets are dated the day of the import.

The whole file is checked before anything is written: a missing column the import needs (`ID`, `Team`, `Title`, `Description`, `Status`, `Priority` or `Labels`) is refused, naming each one, and so is a row with no ID, title or status, a title or description with hidden control characters or over its length, an unknown priority, a repeated ID or a label over 40 characters, naming its line. Projects, statuses and tickets are then made one at a time, each with its own entry in the history. If the server refuses one part way, the import stops there and says which ticket it stopped at. What it made stays, in every project it reached, teams before the one it stopped in included, and the message lists them. To start over, delete those projects in their settings (each delete saves a snapshot first) and import the file again.

## Restoring a snapshot

Stop the server, then restore:

    vaquero stop
    vaquero backup list
    vaquero backup restore vaquero-20261118-120000.sqlite

The database as it was is saved as a new snapshot first, so a restore can itself be undone. The next command starts Vaquero on the restored database.

A database written by a newer Vaquero is refused by an older one, with a message saying so; upgrade Vaquero rather than editing the file.

A copied data directory shares the original's instance id. Never run a copy against the same attachment directory or bucket as the original: the two would write and sweep each other's files.

## Moving attachments to other storage

Where attachment files are kept is set by `VQ_BLOB_STORE` and the settings beside it (see [Attachments](/help/attachments#where-the-bytes-are-kept)). Once there are attachments, the server refuses to start pointed anywhere else. To move them, stop the server, set the new storage's settings in your shell (or in `vaquero.env`), and move:

    vaquero stop
    export VQ_BLOB_STORE=dir
    vaquero attachments move --to dir

`--to` names the new storage: `sqlite` (into the database), `dir` or `s3`. Its directory or bucket comes from `VQ_BLOB_DIR` or the `VQ_S3_` settings, as for the server. If `VQ_BLOB_STORE` in your shell is set to something else, the move stops and says so, since the server must be started with the same storage afterwards. The files are read from where the database says they are now; from a bucket, with the `VQ_S3_ACCESS_KEY_ID` and `VQ_S3_SECRET_ACCESS_KEY` in your shell or in `vaquero.env`.

Each file is copied, then read back and compared with the original by size and checksum. Only when every file is in place and checked does Vaquero save the database as a snapshot (`vaquero-premove-…`) and then switch to the new storage; if the snapshot can't be saved, nothing switches. Restoring that snapshot undoes the switch, as long as the old copies are still there (with `sqlite` storage they are in the snapshot itself). Every snapshot or export taken before a move away from a directory or a bucket with `--delete-old` expects the files in that old storage, so restoring one afterwards leaves its attachments without their files. Before copying, it proves the two are different storage: it writes a small test file into the new storage and looks for it through the old, and refuses two directories that are the same or one inside the other ("These are the same storage; nothing was moved."); it checks again before deleting anything. Before copying into a directory or the database, it checks that the copies fit there, and that the data directory's disk also has room for the snapshot. Previews of images aren't copied; they're made again the first time they're shown. A move to the storage already in use is refused: "Attachments are already stored there."

The old copies stay where they were unless you add `--delete-old`, which deletes them, previews included, once the snapshot is saved and the switch is made. It deletes only attachment files and previews; anything else it finds there is left and named. After a move out of the database, `vaquero backup compact` gives their space back.

If a move stops part way (a file missing from the old storage, a copy that doesn't match, a full disk, Ctrl-C), the old storage is still the one in use and the server starts on it as before; `--delete-old` deletes nothing. The move records its progress in `move.pending` in the data directory: run the same command again and it resumes, skipping the files already copied and checked. A move to somewhere else instead first deletes what the stopped one copied, from a directory or the database; copies left in a bucket are named for you to remove by hand. Until then, the server notes in its log that a move is unfinished.

Once the switch is made (at once, when there are no attachments to move), the move saves the new storage's settings in `vaquero.env` in the data directory: `VQ_BLOB_STORE`, with `VQ_BLOB_DIR` for a directory or the five `VQ_S3_` settings for a bucket, keeping every other line of the file. The file is written whole and then put in place, readable only by you, and the move shows what it saved, with the access key shortened to its first and last 4 characters and the secret to its last 4. A move that stops before the switch leaves the file as it was. The old storage's settings are removed once nothing needs them: at once without `--delete-old`, else after the old copies are deleted, since a move run again to finish deleting reads them there. A move to `sqlite` saves `VQ_BLOB_STORE=sqlite`. The server reads the file on every start, under `brew services` too (see [Installing and upgrading](/help/installing-and-upgrading#settings-for-the-server)), so `brew services restart vaquero`, or any command, starts it on the new storage. A setting in your shell still wins over the file: unset the old ones before starting the server from that shell. When a setting from the file names other storage than the database records, the server's refusal says "(from vaquero.env)".

Restoring the `vaquero-premove-…` snapshot doesn't change `vaquero.env`: set `VQ_BLOB_STORE` there back to the storage the snapshot records (the server's refusal names it) before starting the server.

## Compacting the database

Deleting things doesn't shrink `vaquero.db`: the space is kept for new data. With the default `sqlite` storage, attachment files live in the database, so deleted attachments can leave it much bigger than what it holds. To give that space back, stop the server and compact:

    vaquero stop
    vaquero backup compact

Compacting is all or nothing. It saves the database as a snapshot first (`vaquero-precompact-…`), then writes a compacted copy beside it, `vaquero.db.compact`, rebuilds the search index in the copy and checks it: the database's own integrity check, every reference between records, and the same number of rows in every table. Only then does it put the copy in place, and it says how big the database was and is now, and which snapshot holds the one before. If anything fails on the way, the copy is deleted and the database is the file it was. It needs free space for the snapshot and the copy, and says so when there isn't.

A database from an older Vaquero is upgraded when the server starts; start it once, stop it, then compact.
