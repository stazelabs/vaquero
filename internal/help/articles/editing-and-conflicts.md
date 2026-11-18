---
title: Editing and conflicts
section: using
summary: How Vaquero keeps two people, or a person and an agent, from overwriting each other's changes.
order: 3
---
## Versions

Every ticket has a version number that rises each time the ticket changes. When you open a ticket's edit form, the form remembers the version you started from.

## When someone else saved first

If the ticket changed while your form was open, saving shows a warning naming the ticket: it changed since you opened the form. Nothing is saved yet. Open the ticket in another tab to see what changed, then either save again to replace those changes with yours, or cancel.

Comments don't change a ticket's version, so a new comment never blocks your edit.

## Only what you change

An edit changes only the fields it names. From the command line or the API, sending just a new priority leaves the title, tags and everything else as they are. Tags can be added or removed one at a time without replacing the list.

## From scripts and agents

Read the ticket and note its `version`. Send that version with your change (`--version` on the command line, `"version"` in the API). If someone changed the ticket in between, the change is refused: the API answers `409` with the ticket as it is now, and the command line exits with code 3 and prints it. Read it, decide, and try again. Without a version, a change always applies. See [The command line](/help/cli#exit-codes) and [The API](/help/api#errors).
