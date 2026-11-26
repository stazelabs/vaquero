---
title: Projects and tickets
section: using
summary: Keys, statuses, priorities, tags, sub-tickets, links and milestones, and how to rename, archive or delete a project.
order: 1
---
## Projects

A project has a name and a key. The key is 2 to 10 capital letters or digits, starting with a letter, such as `VAQ`. Its tickets are numbered from 1: `VAQ-1`, `VAQ-2`. Addresses accept any case and leading zeros (`/t/vaq-012` goes to `VAQ-12`).

## Statuses

Every new project starts with six statuses: Backlog, Todo, In progress, In review, Done and Cancelled. Each status belongs to a category of the same names, and the category decides where it shows on [the board](/help/the-board) and whether a ticket counts as open. In project settings you can rename statuses, add more (say, Testing in the in progress category), reorder them, or delete one no ticket uses. A new status goes after the last status of its category. Each category keeps at least one status. The [command line](/help/cli) (`vaquero statuses`, `vaquero status`) and [the API](/help/api) make the same changes.

## Tickets

A ticket has a title (up to 200 characters), a description in markdown, a status, a priority, an assignee, tags, an optional parent and an optional milestone.

- **Priority** is none, urgent, high, medium or low.
- **Tags** are words you make up as you go; a new one is created the first time you use it. See [Tags](#tags) to rename, merge or delete them.
- **Sub-tickets** are tickets whose parent is another ticket in the same project. A ticket can't become its own ancestor.
- **Comments** are markdown. You can edit or delete your own.

Every change is recorded in the ticket's history and on the Activity page, except that reordering a card within its column shows only in the ticket's history.

## Tags

Tags match whatever their case: `UI`, `ui` and `Ui` are one tag, which keeps the spelling it was first given. Project settings list each tag the project's tickets carry, with how many carry it.

- **Rename** a tag to change it on every ticket in the project. Other projects that use the same tag keep it as it was, unless only its case changes, which respells it everywhere.
- **Merge** by renaming a tag to a name the project already has. Vaquero asks first, then tags those tickets with the other tag; a ticket that had both keeps one.
- **Delete** a tag to take it off every ticket in the project, after a confirmation. Its tickets stay as they are otherwise.

Each of these is one line on the Activity page, and it raises the version of every ticket it changes, so an edit form opened before it asks you to look again rather than putting the old tag back.

## History

A ticket's page ends with its history, newest first. When the description changes, "Show changes" under that line opens the edit as a line diff: removed lines marked `-`, added lines `+`, with long unchanged stretches folded. Descriptions longer than 64 KB on either side are noted as too long to show, and edits made before Vaquero 0.1.5 show the line only. The Activity page shows the line without the diff.

Dragging a card up or down its column is recorded too. Several drags in a row by the same person, with nothing else between them, read as one line, such as "reordered VAQ-4 in Todo 3 times".

A line about a link names the other ticket as it is now: its current key, linked, even after its project's key changed, or "a deleted ticket" once it is gone. Links recorded before Vaquero 0.1.3 whose other ticket could not be identified keep the key they were made with.

## Links

A ticket can block, relate to or duplicate another ticket, in any project. Vaquero refuses a link that would make a loop of blockers, directly or through a chain (`VAQ-1` blocks `VAQ-2` blocks `VAQ-3` blocks `VAQ-1`), and refuses two tickets duplicating each other. An open blocker keeps a ticket from being ready on the board.

## Milestones

A milestone is a named goal with a description and an optional due date. The board's side panel shows each open milestone's progress, and clicking one shows only its tickets. Close a milestone when it ships. Milestones are added and changed in project settings, or with `vaquero milestone` and [the API](/help/api).

## Changing a project key

In project settings, under Key. The old key stops working at once: links, branch names and commit messages that use it no longer lead anywhere, and another project may take it.

## Archiving and deleting

Archiving hides a project at the bottom of the list and stops new tickets; restore it any time. Vaquero asks before it archives a project, deletes a status or deletes a comment; Cancel or Escape leaves everything as it was. Deleting removes the project, its tickets, comments, links and history for good. You type the project's key to confirm, and Vaquero takes a snapshot of the database first (see [Your data and backups](/help/data-and-backups#backups)).
