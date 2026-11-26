---
title: Projects and tickets
section: using
summary: Keys, statuses, priorities, tags, sub-tickets, links and milestones, and how to rename, archive or delete a project.
order: 1
---
## Projects

A project has a name and a key. The key is 2 to 10 capital letters or digits, starting with a letter, such as `VAQ`. Its tickets are numbered from 1: `VAQ-1`, `VAQ-2`. Addresses accept any case and leading zeros (`/t/vaq-012` goes to `VAQ-12`).

## The projects page

The projects page shows each active project as a card, or as a row in a list: switch with Cards and List at the top, and the page remembers your choice. Each shows the project's icon, name and key, the start of its description, and:

- **Counts**: tickets ready to start (by the same rule as the board's ready cards, and with the [agent workflow](/help/agent-workflow) on, its readiness too), in review, and open (everything not done or cancelled).
- **Milestone**: the open milestone due soonest (else the first open one), with how many of its tickets are done, or "No open milestone".
- **Last change**: when anything in the project last changed.

The "⋯" button opens quick settings in place: the project's name and description, and its icon. Saving brings you back to the projects page. "All settings →" opens the project's full settings. Archived projects are listed under Archived at the end.

## Project icons

Every project shows an icon beside its name: on the projects page, in the project's header, in the breadcrumb on its ticket pages and beside its tickets in search results. Until you choose one it is the project's monogram: the first letter or digit of each of the first two words of its name (or the first two characters of its key) on a colour of its own. The colour never changes, not even when the key does.

In project settings, under Icon, choose the monogram, a glyph, or an image of your own: a PNG, JPEG or WebP of up to 256 KB, shown square and cropped to fit. An uploaded image is kept when you switch back to the monogram or a glyph, so you can choose it again without uploading it. `vaquero project icon` and [the API](/help/api) make the same changes.

The glyphs: folder, rocket, smartphone, globe, server, database, book, wrench, code, terminal, cloud, cpu, package, layers, palette, camera, music, gamepad, heart, star, sun, leaf, flame, zap, shield, bell, mail, map, compass, truck, plane, briefcase, graduation-cap, flask, bug, puzzle, lightbulb, hammer, chart and users.

## Statuses

Every new project starts with six statuses: Backlog, Todo, In progress, In review, Done and Cancelled. Each status belongs to a category of the same names, and the category decides where it shows on [the board](/help/the-board) and whether a ticket counts as open. In project settings you can rename statuses, add more (say, Testing in the in progress category), reorder them, or delete one no ticket uses. A new status goes after the last status of its category. Each category keeps at least one status. The [command line](/help/cli) (`vaquero statuses`, `vaquero status`) and [the API](/help/api) make the same changes.

## Tickets

A ticket has a title (up to 200 characters), a description in markdown, a status, a priority, an assignee, tags, an optional parent and an optional milestone.

- **Priority** is none, urgent, high, medium or low.
- **Tags** are words you make up as you go; a new one is created the first time you use it. See [Tags](#tags) to rename, merge or delete them.
- **Tier** is the execution tier, shown when the [agent workflow](/help/agent-workflow#execution-tiers) is on: design, light, standard or heavy, or none. A ticket may also name an exact **model**; otherwise the project's tier map picks one.
- **Sub-tickets** are tickets whose parent is another ticket in the same project. A ticket can't become its own ancestor.
- **Comments** are markdown. You can edit or delete your own.

Every change is recorded in the ticket's history and on the Activity page, with one exception: reordering a card within its column shows only in the ticket's history. Renaming, merging or deleting a tag is one line on the Activity page and one in the history of each ticket it changed (a tag change from an older Vaquero that didn't record its tickets shows on the Activity page only).

## Tags

Tags match whatever their case: `UI`, `ui` and `Ui` are one tag, which keeps the spelling it was first given. Project settings list each tag the project's tickets carry, with how many carry it.

- **Rename** a tag to change it on every ticket in the project. Other projects that use the same tag keep it as it was, unless only its case changes, which respells it everywhere.
- **Merge** by renaming a tag to a name the project already has. Vaquero asks first, then tags those tickets with the other tag; a ticket that had both keeps one.
- **Delete** a tag to take it off every ticket in the project, after a confirmation. Its tickets stay as they are otherwise.

The same changes can be made with `vaquero tag` or [the API](/help/api#writing). Each of these is one line on the Activity page and one in each changed ticket's history, and it raises the version of every ticket it changes, so an edit form opened before it asks you to look again rather than putting the old tag back.

## History

A ticket's page ends with its history, newest first. When the description changes, "Show changes" under that line opens the edit as a line diff: removed lines marked `-`, added lines `+`, with long unchanged stretches folded. Descriptions longer than 64 KB on either side are noted as too long to show, and edits made before Vaquero 0.1.5 show the line only. The Activity page shows the line without the diff.

Dragging a card up or down its column is recorded too. Several drags in a row by the same person, with nothing else between them, read as one line, such as "reordered VAQ-4 in Todo 3 times".

A line about a link names the other ticket as it is now: its current key, linked, even after its project's key changed, or "a deleted ticket" once it is gone. Links recorded before Vaquero 0.1.3 whose other ticket could not be identified keep the key they were made with.

## Links

A ticket can block, relate to or duplicate another ticket, in any project. Vaquero refuses a link that would make a loop of blockers, directly or through a chain (`VAQ-1` blocks `VAQ-2` blocks `VAQ-3` blocks `VAQ-1`), and refuses two tickets duplicating each other. An open blocker keeps a ticket from being ready on the board.

On a ticket with no links yet, the form sits behind an "Add link" button. In the ticket box, type a number for a ticket of the same project (`12` or `#12`), a full key for another project's (`OTH-3`), or words of a title; Vaquero suggests up to ten matching tickets as you type.

## Milestones

A milestone is a named goal with a description and an optional due date. The board's side panel shows each open milestone's progress, and clicking one shows only its tickets. Close a milestone when it ships. Milestones are added and changed in project settings, or with `vaquero milestone` and [the API](/help/api).

## Moving a ticket to another project

On a ticket's page, choose a project under "Move to project", then Move ticket and Confirm. From the command line it is `vaquero ticket move-to VAQ-12 OTH`; the API has `POST /api/v1/tickets/{ticket}/move-to`, and a [bulk change](/help/api#bulk-changes) can move tickets too.

- The ticket takes the next number in its new project (`OTH-31`). Its old key keeps leading to it: `/t/VAQ-12` goes to `/t/OTH-31`, and the API, the command line and agents accept `VAQ-12` and answer for `OTH-31`. A ticket that moves again, or back, keeps every old key.
- Its sub-tickets move with it, each taking a new number too. A sub-ticket can't move on its own: move its parent, or detach it first.
- Its status, milestone and assignee carry over by name. A status the new project doesn't have becomes that project's first status of the same kind (a todo stays a todo); a milestone it doesn't have is dropped.
- Its comments, attachments, links, plan and history stay with it. The move is one line in its history and on the Activity page, naming both projects.
- With the [agent workflow](/help/agent-workflow) on, a moved ticket needs approving again in its new project, and again if it comes back: an approval counts only when it was asked after the ticket last arrived where it is. Its earlier approvals stay in its history but no longer make it ready. Only you, not an agent, can move a ticket out of a project with the workflow on into one with it off. Open decisions about it stay in the old project; the move's line counts them, and a plan approval asked there can no longer approve it.

## Changing a project key

In project settings, under Key. The old key stops working at once: links, branch names and commit messages that use it no longer lead anywhere, and another project may take it.

## Archiving and deleting

Archiving hides a project at the bottom of the list and stops new tickets; restore it any time. Vaquero asks before it archives a project, deletes a status or deletes a comment; Cancel or Escape leaves everything as it was. Deleting removes the project, its tickets, comments, links and history for good; a ticket that moved to another project earlier stays there, with its history, and its old key here stops working. You type the project's key to confirm, and Vaquero takes a snapshot of the database first (see [Your data and backups](/help/data-and-backups#backups)).
