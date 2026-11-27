---
title: Writing in Markdown
section: using
summary: How to format ticket descriptions and comments: headings, lists, code, tables, task lists, links, images and attachments.
order: 7
---
Ticket descriptions and comments are written in Markdown: plain text with a few marks that become formatting when the ticket page shows it. The small Markdown link under each description and comment box opens this page in a new tab, so what you've typed stays. Project and milestone descriptions are plain text.

Vaquero reads the widely used dialect of Markdown that adds tables, task lists, struck-through text and bare web addresses as links. Each example below shows what you type.

## Paragraphs and line breaks

A blank line starts a new paragraph. A single line break doesn't: the two lines run together, as one paragraph.

    First line
    second line, same paragraph

    A new paragraph

To break a line without starting a paragraph, end it with two spaces or a backslash.

    Ends with two spaces  
    so this is a new line

## Headings

Start a line with `#` for a heading, `##` for a smaller one, `###` for smaller still.

    # Heading
    ## Smaller heading
    ### Smaller still

## Emphasis

    *italic*, **bold**, ~~struck through~~

## Lists

Start each line with `-` (or `*`) for a bulleted list, or a number and a dot for a numbered one. Indent a bullet two spaces to put it under the item above.

    - One
    - Two
      - Indented under two
    1. First
    2. Second

## Task lists

A list item that starts with `[ ]` shows a checkbox; `[x]` shows it ticked. The boxes can't be clicked on the page: to tick one, edit the text.

    - [ ] Write the migration
    - [x] Agree the schema

## Code

Put backticks around code inside a sentence.

    Run `vaquero ready` first.

For a block of code, put a line of three backticks above it and another below. The block keeps its spacing and line breaks. A language name after the opening backticks is allowed, but the code isn't coloured.

    ```
    vaquero claim VAQ-12
    vaquero heartbeat VAQ-12
    ```

## Tables

A header row, a row of dashes, then one row per line, with `|` between the cells. Every column is left-aligned, whatever colons the dash row has.

    | Step | Who |
    |---|---|
    | Review | Sam |

## Quotes

Start a line with `>` to quote it.

    > The build fails on Linux only.

## Links

Put the text in square brackets and the address in parentheses after it.

    [the runbook](https://example.com/runbook)

An address typed on its own, starting with `http://`, `https://` or `www.`, becomes a link too.

    https://example.com/status

Every link is marked so search engines don't follow it, and a link that would run a script is shown as plain text. Ticket keys like VAQ-12 stay plain text; to link a ticket, use its address, or add a link on the ticket page (see [Projects and tickets](/help/projects-and-tickets)).

## Images and attachments

An image is a link with `!` in front: `![what it shows](address)`. Only an attachment shows as an image. Pasting or dropping a file into a description or comment attaches it and writes the reference for you, an image for a picture and a link for any other file; `vaquero attachment new` prints the same reference. See [Attachments](/help/attachments).

    ![The error dialog](/attachments/01M89WJCM72AW7ZD4DHSKJ0RQ1/error.png)
    [trace.log](/attachments/01M89WJD1B4C3PZ8N2X7GQ5K6V/trace.log)

An image from anywhere else becomes a link to it, labelled with its description, because the page loads images only from Vaquero itself.

    ![A chart](https://example.com/chart.png)

## HTML

HTML isn't run or drawn: tags show as the text you typed, so writing about a `<select>` element keeps the word.

    Pick a value from the <select> list.
