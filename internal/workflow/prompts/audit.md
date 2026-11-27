---
name: audit
title: Audit a project
description: Run one kind of audit over a project and file what it finds as tickets, without fixing anything.
args:
  - name: project
    description: The project to audit, by key, such as VAQ.
    required: true
  - name: kind
    description: The kind of audit.
    required: true
    values: qa, hygiene, continuity, docs
---
# Audit {{project}}: {{kind}}

Run the {{kind}} audit of {{project}}: find, verify, file. It runs in a clone of {{project}}'s repository; paths are relative to its root. Work the steps in order.

The audit never fixes. It never edits product code, never edits, moves or comments on an existing ticket without the owner's yes, and never pushes, merges or tags. The one exception is the docs audit, which corrects documentation itself, never code (step 3, docs). Everything you record goes into Vaquero through its tools. Anything only the owner can decide is asked as a Vaquero decision, never left in the chat.

What to look for comes from the project's audit pack. How to look comes from this text.

## 1. Find the pack

Read `.vaquero/audits/{{kind}}.md`. When it is missing, read `.claude/agent-workflow/audits/{{kind}}.md` instead; for the docs audit, `.claude/agent-workflow/audits/update-docs.md` also counts. With none of them, write nothing and stop with exactly this:

"No audit pack for {{kind}}: run vaquero workflow pack {{kind}} > .vaquero/audits/{{kind}}.md and fill it in."

The pack's sections are found by their exact `##` headings, the ones `vaquero workflow pack {{kind}}` prints. When one is missing, other than a section the template calls optional, stop and name it. A section's body may be "none", but one still holding the template's `<…>` placeholder text is unfilled: treat it as missing. Text outside the headings is context.

Then read everything the pack's **Standards** section lists, in full. A finding traces to a written rule from those documents, or, in continuity, to a named peer set (step 4); anything else is an opinion and isn't filed.

Then read the project in Vaquero:

- `list_statuses` (project {{project}}): findings are filed in Backlog, or the project's first backlog status if it has none by that name.
- `list_tickets` (project {{project}}, tag `audit-{{kind}}`): what earlier audits filed. A finding that matches a cancelled one is dropped unless the code changed since.

## 2. Choose the focus

The focus is every dimension in the pack (for qa every area, for docs every partition), unless the owner named some in the chat. An audit keeps no run state yet; say what you covered in the report.

## 3. Sweep

Follow only the part for {{kind}}.

For hygiene, continuity and docs, sweep with Claude Code subagents, one per dimension (or partition), started in a single message so they run in parallel. Each brief carries the dimension's row and any method for it, the rules it rests on quoted from the Standards documents (not paraphrased), the pre-pass output, the pack's **Search surface** with its exclusions (for docs, the partition's files), and the pack's **Known non-findings** word for word. Ask each for at most its 10 strongest findings (15 for docs), each with the rule, `file:line` for every occurrence, the concrete failure or cost, and a proposed fix: evidence, not adjectives. Subagents are read-only: they never edit, never touch the network or a paid service, and never run anything the pack forbids.

### hygiene

1. Run the pack's **Pre-pass** commands first. Never spend a subagent on what a linter finds, and never file what the project's gate would catch: tooling output is context for the sweep, not a ticket. Run an optional tool only for the dimension the pack ties it to.
2. Sweep the pack's **Dimensions**. A cross-cutting invariant the pack names belongs to the dimension it names.
3. Rank by security, then correctness, then performance, then hygiene. A performance finding names the path and why it is hot; a security finding names the attacker and what they get.

### continuity

1. Read the commits since the last `audit-continuity` ticket was filed (`git log --oneline --since=DATE`, with a time of day, or the last 100 commits on a first run): new view files are where fresh drift lives, so name them in the briefs.
2. Sweep the pack's **Dimensions**, each with its **Methods** subsection quoted. A deviation row is checked against its quoted rule. A discontinuity row is checked against a peer set: the sibling surfaces that do it the other way.
3. Partial adoption is measured, not spot-checked: count a shared component's call sites both ways and report the ratio with both lists. The finding is the tail ("use X on the remaining N surfaces", with the list), never "X is inconsistent". Below about a third adoption, report the ratio and don't file.
4. A narrow-viewport row from source is only a suspicion ("likely breaks below the small breakpoint"); the qa audit confirms it in a browser.

### qa

The qa audit drives the running app in Chrome. Never drive the browser from a subagent: walk the flows yourself, one at a time.

**Load the browser.** Load the tools in one ToolSearch call:

```
select:mcp__claude-in-chrome__tabs_context_mcp,mcp__claude-in-chrome__navigate,mcp__claude-in-chrome__computer,mcp__claude-in-chrome__read_page,mcp__claude-in-chrome__tabs_create_mcp,mcp__claude-in-chrome__tabs_close_mcp,mcp__claude-in-chrome__read_console_messages,mcp__claude-in-chrome__read_network_requests,mcp__claude-in-chrome__form_input,mcp__claude-in-chrome__resize_window,mcp__claude-in-chrome__javascript_tool
```

Without them, stop: "This audit drives Chrome through Claude in Chrome; connect it and run again." Load the chrome-browser skill before the first browser step when the session lists it.

**Boot a disposable server.** Two Vaquero servers are in play, and they must never be confused. The tracker is where you file findings: only through the MCP tools, never driven in the browser. The scratch server is a copy of the app that only this audit drives, with data that is thrown away.

- Stop before booting when the pack's **Never targets** or **Boot** section is missing, "none", or still holds the template's `<…>` text.
- Never touch anything in **Never targets**; a command whose output names one is a stop.
- Start the scratch server exactly as **Boot** says, in the background, and whatever it says, with a new empty temporary data directory of its own and a free port that isn't the tracker's. When Boot leaves either unset, or points at an existing data directory, stop. Record the port, the PID, the data directory and the log.
- Every later command in the run that talks to an app server carries the scratch environment. A `vaquero` command without it goes through the saved profile to the tracker; for a Vaquero app, give each one `VAQUERO_SERVER=local` with the scratch `VQ_ADDR` and `VQ_DATA_DIR`, and never `--server` with a saved server's name.
- Check the health URL answers (if the server exited, read its log and stop), and note the log's line count so you read only this run's lines.
- Sign in as the pack says. Open a sign-in link in a new tab, and first confirm its host and port are the scratch server's; if it names anything else, stop.
- Elevate an account, or make a second one, only on the scratch data, as the pack says, and never without the scratch environment set on that command.

**Browser safety.**

- Work in a new tab of your own (`tabs_create_mcp`); never take over the owner's tabs.
- Never trigger a native alert, confirm or prompt dialog: they freeze the extension. Finding one is itself a finding; report the element without clicking it.
- Stay on the scratch host and port. Don't follow links that leave it, and never navigate to a Never target, the tracker included.
- Don't read or echo cookies or tokens from page scripts. Return counts and short strings from `javascript_tool`, never whole documents.
- Don't enter real credentials or real email addresses.
- When an element won't click, a page hangs or the extension stops answering, stop after 2 or 3 attempts and say so. Don't loop.
- Dialogs that need a person, such as permission prompts and file pickers, are the owner's.

**Walk the areas.** The pack's **Areas** lists flow slices and cross-cutting passes. These passes are known here; the pack may add to them or override one with a `### Pass X` subsection:

- N, console and network: on every page visited, `read_console_messages` (errors) and `read_network_requests` (4xx, 5xx, and any request to another host).
- O, dark mode: find whether the theme follows the system or a toggle. When it follows the system, read `matchMedia('(prefers-color-scheme: dark)').matches` with `javascript_tool`; when it is light, ask the owner in the chat to switch it, or mark the pass untested (needs a dark system). Hunt for surfaces that didn't swap.
- P, narrow viewport: `resize_window` reports success even when it does nothing and has a floor near 600 px, below which the window wedges. Ask for about 600, then measure what you got with `matchMedia` for the breakpoints, never `innerWidth`. Use one fresh window per size: a tab opened after shrinking inherits the wedged width, so close every tab you opened (`tabs_close_mcp`) and call `tabs_context_mcp` with createIfEmpty true for a new window. Close narrowed windows at the end rather than resizing them back. Below the floor, measure a suspect row's intrinsic width in an off-screen container of the target width (288 px for a 320 px phone) and compare its scrollWidth with its clientWidth. Only the page body scrolling sideways is a defect; a table scrolling inside its own overflow wrapper is not.
- Q, keyboard and focus: focus always visible, order follows the page, Enter and Space work buttons and switches, Escape closes what opened, nothing traps focus.
- R, validation and errors: submit forms empty, oversized and malformed. Expect an inline error or a toast, never a 500, a lost form or a native dialog.
- S, role gating: walk as each lower role the pack's Pass S describes; forbidden controls are absent, and a gated URL typed by hand is refused, never a 500.
- T, recent fixes: re-walk the flow of each `audit-qa` ticket done since the last qa audit.

On each page: navigate by the app's own links (a URL typed by hand only to test it is addressable), take a screenshot with `computer`, do the slice's actions, then check the console and network. Record each candidate as you go: the URL, the exact steps, expected and actual, the console, network and log evidence. Give anything you create a `QA-` prefix.

- Screenshots lie mid-transition: wait 2 seconds and check the page before believing a visual state, and check the server log for whether a missing action's request was sent.
- Target elements by label, name or text, never by index.
- Give anything that arrives by polling the time the pack allows before calling it missing.

**Limits.** The pack's **Never targets** and **Limits** are absolute. Never send real email or call a paid service. Destructive admin actions only on `QA-` data you made. A flow you can't test within them is reported as untested (out of bounds), never improvised around.

**Triage.** Drop environment noise silently: extension chatter, a navigation while the server was starting, anything the pack's **Triage** lists. An odd page that is right for odd scratch data is not a defect. A stale page after a change you know succeeded may be the browser's cache: check the scratch data as the pack says and reload with `?cb=1` before believing it. For each defect left, read the server log from your mark with the pack's greps, and first show the grep matches a request you know succeeded: a zero proves nothing until the pattern is shown to fire.

**Clean up** once step 4's reproductions are done, before asking the owner anything: stop the server by its PID (`kill PID`, never `pkill -f`), do the pack's **Cleanup**, close every tab and window you opened, and ask the owner to switch the system theme back if you asked them to change it.

### docs

The docs audit corrects documentation itself, and files tickets only for what it can't fix: code that is wrong where the document is right.

1. Run `git status --porcelain`. When anything is uncommitted, stop and say so; never mix in other work. On the main branch, create a branch `docs/audit-YYYY-MM-DD` (today's date) first; on any other branch, work there.
2. Read the pack's **Standards** document in full: it is the conventions contract. It is corrected for fact only (paths, names, package lists, what is built); never invent, relax or delete a rule.
3. Read the last 100 commits (`git log --oneline -100`): shipped features and new packages are where docs drift. Run the pack's **Ground truth** commands for the facts the documents claim.
4. Edit only the documents the pack's **Surfaces** table names, as it says. Never edit a source or test file, a doc comment in code included; never a path the **Frozen** list names, a generated file, or a dated spec or plan. Drift that needs a change anywhere else is a ticket (steps 4 to 7).
5. Sweep with one subagent per area in the pack's **Partitions**, each brief carrying the area's files, the **Frozen** list, the ground truth, the **Voice** rules and the **Extra rules** for its area. A finding is the file and line, the claim as written, the evidence at `file:line` in the code, and a proposed replacement.
6. Drift is: something called planned that shipped, or listed as built that was dropped (worst); a link, path, command, setting, route or symbol that no longer exists; a flow the code no longer performs; two documents that disagree (settle it against the code, fix both); a shipped package the layout section doesn't mention (add one line in its style); and the **Voice** rules, fixed in passing. A dated spec that differs from the build is not drift.
7. Verify each finding against the code yourself (step 4), then edit: the smallest correct change in the document's own voice. Never invent a feature, a ticket key or a date: a claim you can't verify is deleted or left alone, never replaced with a plausible one. Run a surface's tests from **Extra rules** as soon as you touch it.
8. Run the check the pack's **Verify** names for the documents you changed (help articles, for example, may be held by tests), and see it pass. If it won't pass, stop and leave the work uncommitted for the owner.
9. Commit only the paths you changed (`git add` with each path), in the project's commit style, with the areas swept and the corrections in the body. Never push, merge or tag.

The rest of the steps cover the docs audit's tickets: code that drifted from a correct document, and drift you couldn't verify well enough to correct.

## 4. Verify in the main loop

A subagent's finding is a claim. Open the cited code yourself and confirm each one; never hand verification back to a subagent. Drop anything that fails, without arguing for it: a short list of confirmed findings beats a long list of plausible ones.

- The pack's **Known non-findings** are final: drop a finding they cover. Generated files are never evidence.
- Apply the pack's **Verification notes**, when it has them.
- Dead code: grep for every way the symbol could be referenced (templates, generated code, route mounts, strings in scripts, test-only use) before believing it is dead.
- Duplication: open both sites; near-duplicates that differ in a way that matters are not duplicates.
- Security and correctness: trace the real route. Is the guard really absent, or does a parent carry it?
- Performance: confirm the path is hot. "Could be slow at scale" with no evidence is not a finding.
- A continuity discontinuity survives only when all four hold: at least two peers, opened and confirmed, do it the other way; they are siblings in one of the pack's peer groups; nothing in the Standards, the pack or a code comment explains the difference; and a person would notice it.
- A qa defect is reproduced once more from a fresh page, unless the log proves it alone; traced through the pack's **Code map** to a `file:line` in source; and names the convention it breaks, when one does.

One finding is one root cause: a hazard at six places is one finding listing all six. Two unrelated findings are never one ticket, and a deviation and a discontinuity are never merged.

A live, exploitable security hole is told to the owner in the chat at once, not left for the report.

## 5. Look for duplicates

For each finding, search the open tickets: `search` with its key words, and `list_tickets` (project {{project}}, open true), read through for the same hazard. When an open ticket already covers it, don't file it: list it as a duplicate with that ticket's key, and offer the new evidence as a comment on it (step 6).

## 6. Ask the owner

Unless the owner already said yes in the chat to filing these findings, ask once with `ask_decision`: kind question, project {{project}}, and in the question every finding numbered, each with its proposed title, tier and one line of evidence, then the duplicates (each with its ticket and the comment you'd add) and one line per dropped finding, so the owner can overrule. Offer two options, "File all" and "File none", and say in why that the owner can answer in words naming the ones to file. Tell the owner the decision's id in the chat: they answer with `vaquero decision answer ID "File all"`, or in words with `--text`. Never answer it on your own; when the owner answers in the chat instead, relay it with `answer_decision`, their words quoted in words.

Then stop and report the question's id; don't poll for the answer. The owner answers later, and the question holds the findings, so a later run of this audit files them: before sweeping again, it reads the answer with `vaquero decision show ID` (or `list_decisions`, project {{project}}, kind question) and, on a yes, files what was approved. When the owner answers in this chat while you are still here, file then.

File what the answer approves and nothing else. When the owner calls a finding intentional, write the line you would add to the pack's **Known non-findings** in your report; edit the pack only when the owner asks.

## 7. File each finding as its own ticket

For each approved finding, `create_ticket` with:

- project {{project}}, status Backlog (as step 1 found it);
- a title that leads with the verb and names the hazard and its scope: "Scope the inbox query to the workspace";
- tags: `audit-{{kind}}`, and no other;
- tier: light for a mechanical change, standard for ordinary work in a few places, heavy for cross-cutting or tricky work, design when the fix needs a decision first;
- priority 1 only for a confirmed exploitable security hole, 2 for a risk of data loss or a broken primary flow; otherwise leave it out;
- a body in Markdown, with these parts:
  - **Finding**: what is wrong, with the rule it breaks quoted, or for a discontinuity the peer set spelled out.
  - **Evidence**: `file:line` for every occurrence; for qa, the page, the steps, expected and actual, and the log or console lines quoted, then the traced `file:line`.
  - **Why it matters**: the concrete failure or cost.
  - **Acceptance criteria**: a short list of checkable outcomes.
  - A last line: "Found by the {{kind}} audit on YYYY-MM-DD." with today's date.

Never cite a ticket you haven't created: create it first and use the key it returns. Relate findings that touch each other with `link_tickets`, kind relates. Add each approved comment to its duplicate with `create_comment`.

## 8. Report

Report in the chat:

- the kind, and the dimensions, areas or partitions covered;
- the tickets filed, by key;
- the findings dropped, each with its reason;
- the duplicates found, each with the open ticket's key;
- for qa, the flows left untested and why, and the cleanup done;
- for docs, the documents corrected, the commit and the branch;
- the decision's id, when the answer is still to come;
- any lines proposed for the pack's **Known non-findings**.

A clean dimension is a real result: say so rather than finding something to file.
