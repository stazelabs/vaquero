---
title: Welcome to Vaquero
section: getting-started
summary: What Vaquero is, how it runs on your machine, and where to go next.
order: 1
---
Vaquero is a ticket tracker for one person or a small team, and for the agents working beside them. It is one program, `vaquero`, with one database file. It has a web page (this one), a command line, and an API that scripts and agents use.

## How it runs

On your own machine Vaquero runs in local mode. The server listens only on this computer (`127.0.0.1:7878` unless you set `VQ_ADDR`), answers only requests addressed to `localhost`, `127.0.0.1` or `::1`, and has no sign-in: you are the owner. Start it with `vaquero serve`, or run any other `vaquero` command and it starts the server in the background for you and says where it is. `vaquero open` opens it in your browser.

Local mode is meant for a personal machine: every program on this computer can reach the server, so on a computer other people use, Vaquero needs hosted mode, with sign-in, which comes in a later release. [API tokens](/help/api-tokens#who-can-use-the-local-server) has more.

Everything lives in one data directory. [Your data and backups](/help/data-and-backups) says where it is and what is in it, and [Installing and upgrading](/help/installing-and-upgrading) how to install Vaquero and keep it current.

## The pieces

- **Projects** hold tickets. Each project has a short key, such as `VAQ`, and its tickets are numbered `VAQ-1`, `VAQ-2` and so on. See [Projects and tickets](/help/projects-and-tickets).
- **The board** shows a project's tickets by status, with the tickets ready to start marked. See [The board](/help/the-board).
- **The command line** works with the same projects and tickets from a terminal, and runs the things that need the server stopped, such as backups. See [The command line](/help/cli).
- **The API** is what the command line uses, and what your scripts and agents can use too. See [The API](/help/api).

## Getting around

The sidebar on the left of every page has the search box (`⌘K` or `/` takes you there), then Inbox, My work (the open tickets assigned to you, across your projects) and Activity (every change, newest first). Below them are your projects, by name: the one you are in opens to its Focus, Board and List, and All projects leads to the projects page, where archived projects are too. At the foot are your profile (your name and picture), Help, Admin (version, data, backups, API tokens and the MCP log) and a button for the keyboard shortcuts. The bar above each page names it; in a project it has the Focus, Board and List tabs, Settings and New ticket. On a narrow screen the sidebar is hidden behind the menu button at the left of that bar, and closes when you pick a link, press Escape or tap beside it. Press `?` on any page for the shortcuts, or read [Keyboard shortcuts](/help/keyboard-shortcuts).

Every article here is also plain markdown: add `.md` to its address, or run `vaquero docs` and a topic name.
