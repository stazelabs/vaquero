---
name: plan
title: Plan a ticket
description: Write a ticket's plan and ask the owner to approve it, without building anything.
args:
  - name: ticket
    description: The ticket to plan, by key, such as VAQ-12.
    required: true
---
# Plan {{ticket}}

Turn {{ticket}} into a plan the owner can approve, grounded in the code, and ask for that approval. This prompt plans only: never edit code, branch, claim or move the ticket. It ends when the approval is asked. Work the steps in order.

Everything you record goes into Vaquero through its tools. Anything only the owner can decide is asked as a Vaquero decision, never left in the chat.

## 1. Read the ticket

- Read {{ticket}} with `get_ticket`: its title, description, every comment, its sub-tickets, links, references, tier and, with the project's agent workflow on, its readiness. The project's key is the part of the key before the dash.
- Read its current plan with `get_plan`, if it has one, and note the revision: you revise that plan rather than start over.
- Read each reference to a repo path with `read_file` (project and path); open URL references too.
- Read the rulings and answers already given with `list_decisions` (ticket {{ticket}}), and the open questions, so you don't ask one twice. An answered decision binds the plan.
- If `get_ticket` refuses the ticket, stop and say why.

## 2. Ground it in the code

- The project's CLAUDE.md and the specs it names are the authority. Read the parts that touch the ticket's area; a plan that breaks one of their invariants isn't done.
- Explore the code the ticket touches. A Claude Code subagent suits a broad sweep; read the critical files yourself. Find what to reuse (helpers, queries, components, a sibling that does the same job) before proposing anything new.
- `search` and `list_tickets` find related or duplicate tickets.

## 3. Settle what is open

When the ticket turns on a choice that the ticket, its comments, its decisions and the authority don't settle, ask the owner with `ask_decision`: kind question, the project, tickets {{ticket}}, the options with their trade-offs, the label of the option you recommend in recommended and why in recommendation, and why the choice matters in why. Ask one decision per choice, and don't invent scope to avoid asking.

A choice you can make yourself within the authority is a ruling: record it with `ask_decision`, kind ruling, with your answer and a one-line note, and name it in the plan. The owner can overrule it.

Draft the rest of the plan meanwhile, marking what waits on each question. When a question the plan depends on is open, save what is settled with `save_plan` (step 7), then stop without asking for approval and report the question's id.

## 4. Write acceptance criteria if it has none

A ticket's description is its requirement. When it has no acceptance criteria, draft them: a short list of checkable outcomes, from the ticket, its comments and decisions. Add them to the description with `update_ticket`, keeping the rest of the text as it is, and pass the version you read so a change made meanwhile isn't overwritten.

## 5. Draft the plan

Size it to the ticket, in plain words, following the project's copy rules:

- **Goal**: what the owner gets, in a sentence or two.
- **Approach**: the way you recommend, not a survey, with each ruling you made and why.
- **Files**: create, modify and test, with real paths from the repo.
- **Interfaces**: what it reads and what it produces, with signatures for new functions, types, queries, routes and tools.
- **Steps, test-first**: each step names its failing test and what it asserts, then the change, then the command that shows it passing.
- **Verification**: the project's gate, quoted from its CLAUDE.md (or its Makefile), plus any check by hand.
- **Review focus**: what a reviewer should look at hardest.
- **Risks and out of scope.**

### An epic

When {{ticket}} has sub-tickets, write one plan on the epic. Approving it covers every sub-ticket, and theirs, that has no plan of its own, as each stands when you ask; so give each covered sub-ticket a part of the plan, and its own acceptance criteria. A sub-ticket that needs a plan of its own (it is large, it has repo files of its own, or it is a different kind of work) gets one, through this prompt on that ticket; the epic's plan then leaves it out. Finish the sub-tickets' descriptions before asking: editing one afterwards makes the approval go stale.

## 6. Set the tier

Set the ticket's tier with `update_ticket`, from the plan you drafted, not the ticket's priority:

- **design**: the work is a spec, a plan or a decision, not code.
- **light**: small and clear, one pattern followed.
- **standard**: ordinary work across a few places.
- **heavy**: large or tricky, a new abstraction, or the project's invariants.

Give the reason in one line, in the plan or a comment with `create_comment`.

## 7. Save the plan

Save the whole text with `save_plan`, passing the revision you read (none for a first plan). When the save is refused because someone saved in between, read the plan again with `get_plan`, merge your changes onto it, and save again with its revision; never drop the other revision's text.

## 8. Ask for approval, then stop

Check `list_decisions` for an open plan approval on {{ticket}}. One you asked earlier (its asked_by is your name, as `get_me` shows it) is out of date once the plan or ticket changed: withdraw it with `vaquero decision withdraw ID --agent NAME`, NAME being the one your `vaquero mcp --agent` uses (or set VAQUERO_AGENT), so the withdrawal is recorded as yours (the API's `POST /api/v1/decisions/{id}/withdraw`). Never withdraw one someone else asked; leave it and say so in your report.

Ask with `ask_decision`: kind approval, subject plan, the project, tickets {{ticket}}, a one-line question, recommended Approve with why in recommendation, and why it matters. Leave the options out to offer Approve and Request changes. Asking is refused when a referenced repo file isn't on main yet; say so and stop.

Then stop. This prompt never approves, never answers a decision (only the owner does), and never starts the work. Report:

- the decision's id, and any questions still open;
- the plan's revision and the tier, with its reason;
- with the project's agent workflow on, the ticket's readiness as `get_ticket` shows it now; it isn't ready until the owner approves.
