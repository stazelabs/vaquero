---
title: The board
section: using
summary: Focus, the board's columns and rails, the ready mark, filter menus, dragging, live updates, the milestone panel and the list.
order: 2
---
## Focus

A project opens on Focus, which answers what's next and what just finished. It has three lanes, side by side on a wide screen and stacked on a phone:

- **Next** shows the ready tickets (see [Ready tickets](#ready-tickets)), most urgent first (Urgent, High, Medium, Low, then no priority), and in board order within a priority. It shows five; "N more ready" opens the rest in place. "M waiting" lists the todo tickets that aren't ready, each with why: "Blocked by" the open ticket blocking it, "Has open sub-tickets", "Claimed by" or "Assigned to" someone, and with the [agent workflow](/help/agent-workflow#readiness) on, its readiness reasons, such as "Ask for plan approval." With Mine on, Next shows the todo tickets assigned to you first, then the ready ones. When nothing is ready the lane says "Nothing ready. Triage the backlog.", and names Needs decision when the project has that status.
- **In flight** shows the in progress tickets, then the in review ones, eight to a status before "N more". Each card shows its claim, as on the board.
- **Just finished** shows the tickets done in the last seven days, newest first, each with its day: "today", "yesterday", then the weekday. It shows eight before "N more this week"; "Older: in List" opens the list on done tickets.

Focus takes the same filters as the board, and a board address bookmarked before Focus existed (`/p/KEY?tag=ui`) opens Focus with its filters. The board itself is at `/p/KEY/board`. The tabs under the project name, Focus, Board and List, keep the filters as you switch. Focus updates itself like the board, and the lanes you opened stay open.

## Columns and rails

The board shows a project's tickets by status. Todo, in progress and in review statuses are full columns. Backlog and cancelled statuses are narrow rails at the edges: click one to open it. On a narrow screen an open rail lies over the columns instead of pushing them aside; close it with its close button, by clicking the rail again, or with esc. Done columns show the last seven days; the "older" link at the bottom opens the list view on done tickets, the older ones included.

## Ready tickets

A card marked **Ready**, with an accent edge, is a ticket someone could start now: it is in a todo status, nobody is assigned, no open ticket blocks it, and it has no open sub-tickets. With the [agent workflow](/help/agent-workflow#readiness) on, it also needs a current plan approval, and each card shows its tier.

## Filters

The filter row under the project name narrows Focus, the board and the list. Mine (assigned to you) and Agents (assigned to agents) are chips. Priority, Milestone and Tags are menus: open one and pick a choice, or pick it again to clear it. The Tags menu lists every tag in the project and has a search box that narrows the list as you type. Each filter in force shows as a chip with a ×, which removes it. On the board you can also click a milestone in the side panel to show only its tickets, or "No milestone" for tickets without one. On a narrower screen the panel is a Milestones disclosure above the board, with the same links. Filters live in the address, so you can bookmark or share a filtered board. Clear filters puts everything back. A saved address whose tag no ticket in the project carries any more (the tag was renamed, merged or deleted) says "No tag bug in this project." instead of showing an empty board, with a link that clears just the tag filter.

## Moving cards

Drag a card to another column to change its status, or within a column to reorder it. If the move is refused (the ticket changed, or the status was deleted), the board reloads and says why. If it doesn't go through at all (an error, or Vaquero isn't running), the card goes back where it was and a message says so.

## Live updates

Focus, the board, the list view and the Activity page update themselves when anything changes, whether from another tab, the command line or an agent. A refresh waits while you are dragging. Updates pause while the tab is hidden and catch up when you come back. A page also catches up on changes made while it was loading, or while Vaquero was restarting.

## The list view

List shows every status, done and cancelled included, with every ticket that matches the filters. Use it to find older work. Each status is a section you can fold by clicking its name; this browser remembers which are open, for each project. Done and cancelled statuses start folded, and show 50 tickets at a time: "Show 50 more" adds the next 50, and the list keeps showing them when it updates itself. Besides the board's filters it has a Status menu (Backlog, Todo, In progress, In review, Done, Cancelled), which shows only the statuses in that category, unfolded; `?category=done` in the address does the same, and a category it doesn't know shows every status. The board has no category filter, since its columns already are the categories, and ignores one in the address.

## Keys

`c` makes a ticket, `g f`, `g b` and `g l` switch between Focus, board and list, and `j` and `k` move between cards. See [Keyboard shortcuts](/help/keyboard-shortcuts).
