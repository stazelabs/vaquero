---
title: Installing and upgrading
section: getting-started
summary: Installing Vaquero with Homebrew or from a release archive, keeping it running, and upgrading without losing anything.
order: 2
---
Vaquero is one program, `vaquero`, for macOS and Linux, on Intel or ARM.

## Installing with Homebrew

    brew install stazelabs/vaquero/vaquero

That is all: run any `vaquero` command and the server starts in the background. To have it start when you log in, and come back if it stops, let Homebrew run it as a service:

    brew services start vaquero

`brew services stop vaquero` stops it; `vaquero stop` doesn't, because the service would only start it again.

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
