---
title: Installing and upgrading
section: getting-started
summary: Installing Vaquero with Homebrew or from a release archive, keeping it running, and upgrading without losing anything.
order: 2
---
Vaquero is one program, `vaquero`, for macOS and Linux, on Intel or ARM.

## Installing with Homebrew

    brew tap stazelabs/vaquero
    brew install vaquero

The first line adds the Stazelabs tap (once per machine), so every later Homebrew command can say plain `vaquero`. `brew install stazelabs/vaquero/vaquero` does both in one step.

That is all: run any `vaquero` command and the server starts in the background. To have it start when you log in, and come back if it stops, let Homebrew run it as a service:

    brew services start vaquero

`brew services stop vaquero` stops it; `vaquero stop` doesn't, because the service would only start it again, and says so. Vaquero counts the service as running when the service manager has it: on macOS, when launchd has `sh.brew.vaquero` (or the older `homebrew.mxcl.vaquero`) loaded, with or without `~/Library/LaunchAgents/sh.brew.vaquero.plist`, since after `brew services run vaquero` there is no file; on Linux, when `~/.config/systemd/user/sh.brew.vaquero.service` (or the older `homebrew.vaquero.service`) exists and `systemctl --user is-active` says it is active (when `systemctl --user` can't reach a user manager at all, no service is running). With the file there but the service not loaded (after `brew services stop --keep vaquero`, say), `vaquero stop` stops the server as usual, and the next command starts one outside brew services and says so; `vaquero stop`, then `brew services start vaquero`, hands it back. If the service is loaded but its server doesn't come back within 20 seconds, the command stops and says to run `brew services restart vaquero`, rather than start a second server that would take the service's port.

A server started outside brew services can hold the port while the service keeps failing to start. `vaquero stop` asks the service manager which process it runs for the service: when that isn't the server holding the data directory (another process, or none at all between the service's failed restarts), `vaquero stop` stops that server, leaves the service alone and says so: "Stopped a Vaquero started outside brew services (pid N); the service can start now." When the service manager doesn't answer, `vaquero stop` still refuses: run `brew services stop vaquero`, then `vaquero stop`, then `brew services start vaquero`.

The service uses the default data directory; a Vaquero you run with its own `VQ_DATA_DIR` isn't the service's, so `vaquero stop` stops it and an upgrade restarts it as usual. On Linux the default comes from the systemd user manager's `XDG_DATA_HOME`, which can differ from your shell's.

## Settings for the server

The server reads its `VQ_` settings from the environment and from `vaquero.env` in the data directory (`~/Library/Application Support/vaquero/vaquero.env` on a Mac, `~/.local/share/vaquero/vaquero.env` on Linux). Under `brew services` the server doesn't see your shell's environment, so settings that must outlast a restart, such as where attachments are kept, go in the file. Every `vaquero` command reads it too, so with `VQ_ADDR` there the command line finds the server on its port.

One setting per line, with the same names as in the environment:

    # vaquero.env
    VQ_ADDR=127.0.0.1:7900
    VQ_BLOB_STORE=dir
    VQ_BLOB_DIR="/Volumes/Files/vaquero attachments"

A line is `KEY=value`, the value optionally in double quotes. Everything after the `=` is the value, so a `# note` at the end of a line is part of it: put notes on lines of their own. A line starting with `#` is a comment, and blank lines are skipped. Only `VQ_` settings are allowed. `VQ_DATA_DIR` can't be set there, since that is how the file is found: "VQ_DATA_DIR can't be set in vaquero.env." A `VQ_` setting Vaquero doesn't read is noted in the server's log, not refused. A setting in the environment wins over the file, and the file over the default. A setting the environment holds empty (`VQ_BLOB_DIR=`) counts as unset there, so the file's value is used: to go back to a default, remove the line from the file.

The file can hold the S3 secret and says where attachments are kept, so only you may read or change it. A file other users can read is refused, by the server and by every command: "vaquero.env can be read by other users; run chmod 600 PATH." So is one they can change: "vaquero.env can be changed by other users; run chmod 600 PATH." The server's log names where each storage setting came from (environment, file or default), never its value. `vaquero attachments move` writes the file for you, replacing a `vaquero.env` that is a link with a file of its own (see [Your data and backups](/help/data-and-backups#moving-attachments-to-other-storage)). After editing it by hand, restart the server: `brew services restart vaquero` under brew services, else `vaquero stop` and any command.

## The server's log

    vaquero logs        # the last 100 lines
    vaquero logs -f     # and every new line, until Ctrl-C

A server the command line started writes its log to `vaquero.log` in the data directory. Under `brew services` it writes to Homebrew's log directory instead, `$(brew --prefix)/var/log/vaquero.log` (`/opt/homebrew/var/log/vaquero.log` on an Apple silicon Mac). Either way, at 10 MB the log moves to `vaquero.log.1`, replacing the one before, and a new one starts. `vaquero logs` reads whichever this Vaquero uses, with no server running; `vaquero logs --path` says which.

## Installing from a release archive

On Linux without Homebrew, download the archive for your machine from the releases page, with `checksums.txt` beside it, and check it before unpacking:

    sha256sum --ignore-missing -c checksums.txt
    tar -xzf vaquero_0.1.0_linux_amd64.tar.gz
    install vaquero ~/.local/bin/

## Upgrading

    brew upgrade vaquero

The server that was running is still the old version. The next `vaquero` command notices, says `restarting the local server to run 0.2.0 (it was running 0.1.0)`, and starts the new one, so nothing else is needed. Under `brew services` it waits for the service to come back instead. An agent client starting `vaquero mcp` does the same.

With a release archive, replace the `vaquero` file with the new one; the next command restarts the server the same way.

Vaquero never checks for new releases itself; `brew outdated` tells you when one is out.

## What happens to your data

A new version may change how the database is laid out. When it does, the server first saves a snapshot of the whole database into `backups/` in the data directory, then changes it, all before it serves anything. If the snapshot can't be written (the disk is full, say), the server doesn't start, says why, and leaves the database as it was.

Going back is possible: install the older version and restore the snapshot taken before the upgrade, as [Your data and backups](/help/data-and-backups#restoring-a-snapshot) explains. Without that, an older Vaquero refuses a database a newer one has changed, rather than misreading it.
