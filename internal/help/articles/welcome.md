---
title: Welcome to Vaquero
section: getting-started
summary: What Vaquero is, how it runs on your machine, and where to go next.
order: 1
---
Vaquero is a ticket tracker for one person or a small team, and for the agents working beside them. It is one program, `vaquero`, with one database file. It has a web page (this one), a command line, and an API that scripts and agents use.

## How it runs

On your own machine Vaquero runs in local mode. The server listens only on this computer (`127.0.0.1:7878` unless you set `VQ_ADDR`), answers only requests addressed to `localhost`, `127.0.0.1` or `::1`, and has no sign-in: you are the owner. Start it with `vaquero serve`, or run any other `vaquero` command and it starts the server in the background for you.

Everything lives in one data directory. [Your data and backups](/help/data-and-backups) says where it is and what is in it, and [Installing and upgrading](/help/installing-and-upgrading) how to install Vaquero and keep it current.

## The pieces

- **Projects** hold tickets. Each project has a short key, such as `VAQ`, and its tickets are numbered `VAQ-1`, `VAQ-2` and so on. See [Projects and tickets](/help/projects-and-tickets).
- **The board** shows a project's tickets by status, with the tickets ready to start marked. See [The board](/help/the-board).
- **The command line** does everything the pages do. See [The command line](/help/cli).
- **The API** is what the command line uses, and what your scripts and agents can use too. See [The API](/help/api).

## Getting around

The bar at the top of every page has Projects, Activity (every change, newest first), search, Help, Admin (version, data and API tokens) and the keyboard shortcuts. Press `?` on any page for the shortcuts, or read [Keyboard shortcuts](/help/keyboard-shortcuts).

Every article here is also plain markdown: add `.md` to its address, or run `vaquero docs` and a topic name.
