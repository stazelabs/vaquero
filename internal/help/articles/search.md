---
title: Search
section: using
summary: Finding tickets by the words in their titles, descriptions and comments.
order: 4
---
## Searching

Type in the search box at the top of any page (press `/` to jump to it) and press Enter. Vaquero looks through every ticket's title and description and every comment, in all projects, and shows the best matches first with the matching words highlighted.

## How words match

Each word you type matches words that start with it, so `deploy` finds "deploy", "deployed" and "deployment". A result must contain every word, either in the ticket's title and description together or within one comment: `deploy failed` won't find a ticket with "deploy" in its title and "failed" only in a comment. Punctuation separates words, and search operators are treated as ordinary words. Only the first 20 words of a long search are used.

Deleted comments are not searched.

## From the command line

`vaquero search WORDS...` prints the same results. The API has `GET /api/v1/search?q=`; see [The API](/help/api).
