---
title: Search
section: using
summary: Finding tickets by the words in their titles, descriptions and comments, and help articles by their titles and headings.
order: 4
---
## Searching

Type in the search box at the top of any page (press `/` to jump to it) and press Enter. Vaquero looks through every ticket's title and description and every comment, in all projects, and shows the best matches first. Under each result is a line from its description, or from a comment, with the matching words highlighted; a ticket found by its title alone shows the start of its description.

The page shows the best 50. When more tickets match, it says so ("Showing 50 of 73 matching tickets"): add words to narrow the search.

## Help articles

The same search looks through these help articles too. When any match, they show in a Help section above the tickets, at most five of them. An article matches when every word you typed is in its title, its one-line summary or its section headings; the article text itself isn't searched. A word in the title counts most, then a heading, then the summary. When a heading is where the words were found, the result names it and opens the article at that heading.

## How words match

Each word you type matches words that start with it, in tickets and in help articles alike, so `deploy` finds "deploy", "deployed" and "deployment". A result must contain every word somewhere in the ticket: its title, its description or any of its comments. `deploy failed` finds a ticket with "deploy" in its title and "failed" only in a comment. Punctuation separates words, and search operators are treated as ordinary words. Only the first 20 words of a long search are used. Accents don't matter, however they were typed: `resume`, `résumé` and a decomposed `résumé` find each other.

## Ranking

Each word counts for where it was found in the ticket: in the title most, then the description, then a comment. A ticket's score adds those up over the words, so more words in better places rank higher. How often a word repeats doesn't count, and among equal scores the most recently updated ticket comes first.

Deleted comments are not searched.

## From the command line

`vaquero search WORDS...` prints the same results. The API has `GET /api/v1/search?q=`; see [The API](/help/api).
