# Direction reset (2026-11-26)

A whole-product review after v0.1.7, with nothing sacred, and the direction the owner chose from it. This document is the authority for the VAQ queue's shape from here on. Where it disagrees with an earlier spec or plan, this wins; earlier plans not yet built are inputs, re-cut when their run comes up.

## Intent

Vaquero is the seed of an agent-native, local-first software factory for one person or a small team. It should be as simple and quick as a good tracker at one end, and far simpler to set up and run than a multi-agent orchestration platform at the other: one binary, one file, a handful of ideas a person can hold in their head.

## What the review found

1. **We don't use what we built.** The VAQ project had the agent workflow off, no repo path, no priorities and no recorded decisions. Rulings lived in chat and plan files; runs went through an outside process.
2. **The trust layer is unpredictable.** Readiness rested on about 22 rulings and 11 reason strings. One reason had four causes. Reasons never said what to do. Approvals could not be seen or answered in the web.
3. **The queue leaned toward recording and tracing** (sampled factory metrics, audit cadence policies, a run state machine) and held nothing for daily basics: edit in place, quick create, a command palette, an inbox, saved views.
4. **The web is slow to operate and noisy.** A separate edit page; no status control on the ticket; settings in six places and three patterns; a JSON text box for the tier map; agent run logs filling comments and Activity; no project switcher.
5. **Seams.** 42 CLI commands with mixed verbs, 8 missing from the usage text. "Profile", "link", "plan", "release" and "ready" each mean two or three things. Help that contradicts the product. A first run that never says where the web UI is. Decisions, plan editing and adding references with no web surface.
6. **Follow-up buckets hid real work,** a security item among it.
7. **What heavier systems get right, Vaquero already has:** work lives in the store, not in a session, and readiness comes from dependencies. Their cost (dozens of concepts, several runtime dependencies, a daemon) is what to avoid.

## Decisions (owner, 2026-11-26)

1. **Agents pull work.** Vaquero stays the place agents come to. Vaquero starting headless agent sessions itself is a named later step; nothing built now may block it.
2. **The workflow is built into Vaquero** as its own prompts (`plan`, `work`, `triage`, `audit`), ported from the owner's existing agent-workflow skills. Claude Code only at first; tiers stay harness-neutral.
3. **Audience:** the owner now; a small team soon, through full hosted mode (sign-in, roles, a container image) once the solo factory is excellent.
4. **Break anything.** The API, the CLI, the MCP tools, pages and concepts may change or go. Only data must survive upgrades. This replaces the "API is additive" rule until hosted mode.
5. **One simple trust rule** (below).
6. **Dogfood strictly.** VAQ has its repo path set and the workflow on. Plans, rulings and approvals are Vaquero records. Every gap found is a ticket.
7. **Runs are derived from facts.** A run is a name, a branch and its tickets. Its state is read from the tickets and git. The only records are the merge and release approvals, each pinning the branch head it was asked about. Nobody advances a state by hand.
8. **All four knowledge systems stay,** after the seat and the loop: requirements traced, architecture and design artifacts, review beside the code, test management. Each is re-designed briefly against the new shell and the simple rule before it is built.
9. **Documents by lifetime.** Working documents (plans, drafts, requirements) live in Vaquero. Settled knowledge (specs, architecture decisions) is committed to the repo when approved; Vaquero reads, renders and links it.
10. **The owner's seat.** Edit in place and fast create; a command palette and keys; an inbox of what needs the owner; saved views. A left sidebar: Inbox, My work, views, then each project opening to Focus, Board, List, Runs and Docs. Look and density are refreshed. All of it is mocked up with the owner before it is built.
11. **Agent progress is a work log,** folded on the ticket and its run, apart from people's comments.
12. **A live view,** readable from across a room, global or per project: who is working on what, a folded live stream, what waits on the owner, and flow counts. No stored samples. Activity becomes its filtered history.
13. **Order.** A short reset, then runs that each pair one owner-facing piece with one factory piece. Audits come early, on Vaquero's own prompts.
14. **A second project early.** One of the owner's other projects moves onto Vaquero soon after the reset.
15. **No more follow-up buckets.** A review finding is its own ticket, or it is dropped with a reason.

## The one simple rule

With a project's workflow on:

- **A ticket is ready** when it is a todo the board would call ready and its plan is approved.
- **An approval covers** the ticket it was asked about: its title and description, its plan, and the repo files it references, as they were when asked. An epic's approval also covers the sub-tickets the epic had when asked, each as it was then.
- **A ticket's approval is** its own, if it has a plan; otherwise its nearest ancestor's with a plan.
- **An approval goes stale** when anything it covered changes: the text, the plan, a referenced file, a covered sub-ticket, or the ticket moving to another parent or project. A stale approval is not an approval.
- **The page says why and what to do,** in a handful of reasons phrased as actions:
  - "Ask for plan approval."
  - "Changed since approval: {what}. Ask again."
  - "Its epic changed since approval: {what}. Ask again."
  - "Can't check its files: {why}."
- **Only the owner approves.** An agent relays the owner's words and is recorded as the relayer.
- **Tier** defaults to standard and gates nothing.
- **A cancelled epic covers nothing.**

Everything else in the earlier rulings (arrival times, pinned parents as a separate idea, the timing of an approval against a cancelled ancestor, leaning subtleties) follows from these or is dropped.

## The queue

| Milestone | What it holds |
|---|---|
| 1 Reset | One run: the simple rule, one CLI vocabulary, the first run, the dogfood switch |
| 2 Seat and loop | The owner's seat and the built-in workflow, interleaved |
| 3 Second project | One other project moved over and worked through Vaquero |
| 4 Knowledge | Requirements, architecture, review, tests |
| 5 Hosted mode | Sign-in, roles, the container image |
| Later | Vaquero starting agents, pull requests and checks, native Windows |

### 2 Seat and loop, by run

A design session with mockups comes first: the shell, look and density, the ticket page, the inbox and the live view. Its approval gates the UI tickets.

| Run | Owner's seat | Factory loop |
|---|---|---|
| A | Sidebar shell and visual refresh; the inbox | The `plan` prompt; the `audit` prompt and four audit packs; the first audits file tickets |
| B | The ticket page as a working document: every field edited in place, a status control, a lifecycle strip, plan and references edited in the web | Work log entries; the `work` prompt (a ticket, or an epic as a run) |
| C | The live view; Activity as its history | Runs derived from facts; merge and release approvals; the run page |
| D | Quick create, the command palette, keys on the ticket page; saved views and My work; bulk select | The `triage` prompt; MCP gaps; the audit record |
| E | Settings in one pattern | Project workflow settings in the repo, read by the prompts |

## Anti-goals

- A concept a new user must learn before their first ticket, beyond project, ticket and status.
- A second daemon, a database server, or any runtime dependency beyond the one binary (git stays optional).
- A rule whose outcome the owner can't predict from the ticket page.
- A page that exists only to show what was recorded.
