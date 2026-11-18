---
title: The board
section: using
summary: Columns and rails, the ready mark, filters, dragging, live updates and the milestone panel.
order: 2
---
## Columns and rails

The board shows a project's tickets by status. Todo, in progress and in review statuses are full columns. Backlog and cancelled statuses are narrow rails at the edges: click one to open it. Done columns show the last seven days; a link at the bottom leads to the list view with everything older.

## Ready tickets

A card marked **Ready**, with an accent edge, is a ticket someone could start now: it is in a todo status, nobody is assigned, no open ticket blocks it, and it has no open sub-tickets.

## Filters

The chips under the project name narrow the board: Mine (assigned to you), Agents (assigned to agents), a tag, or a priority. Click a milestone in the side panel to show only its tickets, or "No milestone" for tickets without one. Filters live in the address, so you can bookmark or share a filtered board. Clear filters puts everything back.

## Moving cards

Drag a card to another column to change its status, or within a column to reorder it. If the move is refused (the ticket changed, or the status was deleted), the board reloads and says why.

## Live updates

The board, the list view and the Activity page update themselves when anything changes, whether from another tab, the command line or an agent. A refresh waits while you are dragging. Updates pause while the tab is hidden and catch up when you come back.

## The list view

List shows every status, done and cancelled included, with every ticket that matches the filters. Use it to find older work.

## Keys

`c` makes a ticket, `g b` and `g l` switch between board and list, and `j` and `k` move between cards. See [Keyboard shortcuts](/help/keyboard-shortcuts).
