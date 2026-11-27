---
title: Agents and claims
section: api-and-agents
summary: How agents name themselves, take work from the ready queue and hold it with a claim that lapses if they stop.
order: 3
---
Several agents can work from one backlog without stepping on each other. Each agent names itself, picks a ticket from the ready queue, and claims it. A claim lasts for a lease; an agent that keeps working renews it, and one that stops lets it lapse, which hands the ticket back.

## Naming an agent

On this machine an agent uses your token and says who it is. On the command line that is `--agent NAME` or `VAQUERO_AGENT`; through the API it is the `Vaquero-Agent` header. Vaquero creates the agent the first time its name is used, and its comments, edits and claims are recorded under that name. Names are 1 to 80 letters, digits, spaces, dots, dashes or underscores.

To give an agent a token of its own, for a client that connects over HTTP, run `vaquero token new NAME --agent AGENT`. That token always acts as that agent, whatever name a request gives. A token made on the Admin page or with `vaquero token new NAME` alone acts as you, and says who it is only through `--agent` or the `Vaquero-Agent` header. See [API tokens](/help/api-tokens#making-a-token).

## The ready queue

A ticket is ready when it is in a todo status, has no assignee and no claim, nothing open blocks it, and it has no open sub-tickets. In a project with the [agent workflow](/help/agent-workflow#readiness) on, it also needs a current plan approval. The queue lists ready tickets most urgent first (urgent, high, medium, low, then no priority), each in board order.

    vaquero ready VAQ
    vaquero ready --limit 5 --json

## Claiming, renewing and unclaiming

`vaquero claim VAQ-12` claims a ticket for 15 minutes; `--lease 45m` asks for longer, from 1 minute to 2 hours. Claiming assigns the ticket to the agent and moves it from backlog or todo to the project's first in-progress status. Claiming a ticket you already hold renews the claim.

Only one agent can hold a ticket. A second claim is refused with the holder's name and when its lease ends. A ticket that is done or cancelled, or assigned to someone else, can't be claimed.

While it works, the agent renews the lease with `vaquero heartbeat VAQ-12` (again with `--lease` if it wants). Renewing doesn't change the ticket's version, so it never makes the agent's own edits conflict. When it is finished, or wants to stop, `vaquero unclaim VAQ-12` gives up the claim and leaves the ticket's status and assignee as they are.

Only the holder can renew or unclaim a claim, and only while its lease is running.

## Claims are advisory

A claim says who is working on a ticket; it doesn't lock it. Anyone can still edit, move or comment on a claimed ticket, and the change goes through. When the ticket is claimed by someone else, the answer says so: the API and MCP answers to an update, a move, a comment, a link or an attachment carry `"warning": "Claimed by builder until 12:15 UTC."` (bulk updates carry it on each item), the command line prints it as `vaquero: warning: Claimed by builder until 12:15 UTC.` on standard error, and the web edit form shows the same note above the fields. Check with the holder before changing work in progress.

## When a claim lapses

Vaquero checks for lapsed claims every 30 seconds. A lapsed ticket that is still in progress goes back to the status it was claimed from, unassigned, and is ready again; the history says the claim lapsed. A ticket the agent had already moved on, to review say, keeps its status and assignee and only loses the claim.

Moving a ticket to done or cancelled ends its claim straight away, so finished work never lapses back.

## On the board

A claimed card shows who holds it, with a robot for an agent, and how long its lease has left ("12 min left"), counting down as you watch. When a claim lapses the card moves back to where it came from on its own. Claimed tickets are never marked Ready. The board's Agents filter shows the tickets assigned to agents.

## Agents over MCP

Agent clients can do all of this with MCP tools instead of commands: `ready_tickets`, `claim_ticket`, `heartbeat` and `unclaim_ticket`. [MCP](/help/mcp) explains connecting one, and the Admin page's MCP activity shows every call agents made.
