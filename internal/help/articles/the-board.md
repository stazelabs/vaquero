---
title: The board
section: using
summary: Columns and rails, the ready mark, filters, dragging, live updates and the milestone panel.
order: 2
---
## Columns and rails

The board shows a project's tickets by status. Todo, in progress and in review statuses are full columns. Backlog and cancelled statuses are narrow rails at the edges: click one to open it. On a narrow screen an open rail lies over the columns instead of pushing them aside; close it with its close button, by clicking the rail again, or with esc. Done columns show the last seven days; the "older" link at the bottom opens the list view on done tickets, the older ones included.

## Ready tickets

A card marked **Ready**, with an accent edge, is a ticket someone could start now: it is in a todo status, nobody is assigned, no open ticket blocks it, and it has no open sub-tickets.

## Filters

The chips under the project name narrow the board: Mine (assigned to you), Agents (assigned to agents), a tag, or a priority. Click a milestone in the side panel to show only its tickets, or "No milestone" for tickets without one. On a narrower screen the panel is a Milestones disclosure above the board, with the same links. Filters live in the address, so you can bookmark or share a filtered board. Clear filters puts everything back.

## Moving cards

Drag a card to another column to change its status, or within a column to reorder it. If the move is refused (the ticket changed, or the status was deleted), the board reloads and says why. If it doesn't go through at all (an error, or Vaquero isn't running), the card goes back where it was and a message says so.

## Live updates

The board, the list view and the Activity page update themselves when anything changes, whether from another tab, the command line or an agent. A refresh waits while you are dragging. Updates pause while the tab is hidden and catch up when you come back. A page also catches up on changes made while it was loading, or while Vaquero was restarting.

## The list view

List shows every status, done and cancelled included, with every ticket that matches the filters. Use it to find older work. Besides the board's chips it has one per status category (Backlog, Todo, In progress, In review, Done, Cancelled), which shows only the statuses in that category; `?category=done` in the address does the same, and a category it doesn't know shows every status. The board has no category filter, since its columns already are the categories, and ignores one in the address.

## Keys

`c` makes a ticket, `g b` and `g l` switch between board and list, and `j` and `k` move between cards. See [Keyboard shortcuts](/help/keyboard-shortcuts).
