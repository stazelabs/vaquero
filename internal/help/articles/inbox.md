---
title: The inbox
section: using
summary: Everything waiting on you, across your projects, answered where it sits.
order: 10
---
The inbox gathers what needs you from every project that isn't archived. Open it from Inbox at the top of the sidebar, press `g i`, or go to `/inbox`. The number beside Inbox in the sidebar counts the questions and approvals waiting on you plus the tickets in review; it shows only when something waits. When nothing does, the inbox says "Nothing needs you."

## Decide

Open questions and approvals, the oldest first, each as a card. A card's first line says what it is (a question, or a plan, merge or release approval), its project, who asked and how long ago. Below are the question, the tickets it names, and a button for each option with its trade-off under it. When the agent recommended an option, that option comes first, marked Recommended.

A plan approval says what approving covers, as it was asked: "Covers VAQ-12 (plan rev 2) and 3 sub-tickets as asked", or "(no plan)" when the ticket has none. Show plan opens the ticket's plan under the card.

Choosing an option answers at once, as your own direct answer. Add a note first if you want one: it opens a box whose text goes with the answer. The card then leaves the list and the next card takes the keyboard focus. When the answer can't be taken, the card stays and says why under its buttons: the plan or requirement changed since it was asked, the project was archived, or it was answered already (in another tab, the command line or by an agent relaying you). Withdraw, behind a confirmation, takes back a question or approval nobody needs any more.

A link to a decision, `/decisions/ID`, opens the inbox at its card; `vaquero decision show` prints that link while the decision waits.

## Review, Check and rulings

- **Review** lists every ticket in an in-review status of a project with the [agent workflow](/help/agent-workflow) on, the longest waiting first, with who moved it there. A project with the workflow off isn't listed.
- **Check** lists the claims that lapsed in the last 24 hours on tickets that aren't done or cancelled: the ticket is already back in the queue, so there is nothing to do but look. Dismiss hides one in this browser only.
- **Rulings agents made** lists the rulings agents recorded in the last 7 days, each folded to its question and the call. Open one to see its card, where Overrule offers the options again; choosing one replaces the agent's call with yours.

## Answered today

At the foot, Answered today (with its count) opens what you answered since midnight UTC, each with "Answered directly by you" or, when an agent relayed it, "Relayed by" the agent with your words.

## Filters and keys

The chips along the top narrow the inbox to one project, or to approvals or questions alone (which shows Decide by itself). The filters live in the address, so a filtered inbox can be bookmarked. The inbox follows changes as they happen, and waits while you type a note.

On the inbox, `j` and `k` move between the cards, `1` to `9` choose the focused card's option, `n` opens its note, and `o` opens its first ticket. Typing in a note box types; it answers nothing. See [Keyboard shortcuts](/help/keyboard-shortcuts).
