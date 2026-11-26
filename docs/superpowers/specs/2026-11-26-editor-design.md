# The editor: paste that just works, ticket suggestions, click to edit (VAQ-6)

Owner brainstorm, 2026-11-26. Builds on attachments (`internal/web/handlers/attachments.go`, `app.js`'s paste and drop), the Markdown renderer (`internal/markdown`) and VAQ-2's criteria markers.

## Intent

The owner's litmus test: **"I can paste an image into a text box on the ticket page and it just works."** Today every body box uploads a pasted file but then shows `![name](/attachments/…)` instead of the image; the new-ticket form takes no paste at all (an upload needs a ticket); and the description is edited on a separate page.

This round (owner): the litmus test, plus ticket-link suggestions, plus **click to edit** on the ticket page. The rest of "fluid mixed content" (headings, lists and tables shown as typed, a toolbar) comes later on the same base.

## Goals

- Paste or drop an image into any body box (new ticket, description, new comment, comment edit) and see the image there at once, saved as plain Markdown.
- Typing a ticket key offers matching tickets.
- Title, description and one's own comments are edited in place on the ticket page.
- What is saved is exactly the Markdown the person or agent wrote: nothing reflowed, re-bulleted or escaped.

## Non-goals

- A what-you-see editor; a toolbar; tables, headings or lists rendered while typing (later, same base).
- Inline editing of status, priority, tags, milestone or tier (the edit page keeps them).
- Collaborative editing.

## Anti-goals

- Rewriting stored Markdown (it would change VAQ-2's markers and the approval hash, and agents' text).
- A box that loses typed text: to a live refresh, a conflict, or a failed upload.
- Anything that needs JavaScript to save: without it, every box is today's text box.

## Approaches

1. **A Markdown editor with live preview in place** (CodeMirror 6, MIT, no framework): the document is the Markdown itself; images render as widgets when the cursor is off their line. **Chosen** (owner, 2026-11-26).
2. A what-you-see editor (ProseMirror, TipTap) serialising to Markdown: rewrites text on save, and some build their interface from markup the CSP refuses. Rejected.
3. The text box plus a preview strip: the image isn't in place; fails the litmus test. Rejected.

## The editor

- **One component**, `templates.MarkdownEditor(props)`, replaces `TextArea` for every body field. It renders today's `<textarea>` (the form value, and the whole editor without JavaScript) with `data-editor`; `app.js` mounts the editor over it and keeps the textarea's value current on every change, so forms, htmx posts and the confirm dialog work unchanged.
- **Vendored:** one pre-built file, `internal/web/static/vendor/editor.min.js` (CodeMirror 6's view, state, commands, history, autocomplete, language and Markdown packages, at pinned versions), built by a script from pinned packages and committed like htmx; its licences in `internal/legal/vendor/` and the third-party list. Loaded only on pages with a `data-editor` box. **The CSP (owner, 2026-11-26):** `style-src` gains the request's nonce on every page, as `script-src` has (`style-src 'self' 'nonce-…'`); the editor's one `<style>` element carries it (`EditorView.cspNonce`, from the page's existing nonce). `style=` attributes stay banned and un-nonced style elements stay refused. CLAUDE.md's CSP invariant says so, and the middleware's tests hold it. The editor's look is theme tokens through `input.css` classes.
- **Images in place:** a line holding only an image whose destination is an `/attachments/{id}/…` path shows the image (the original, lazily loaded, at most the box's width and 480 px tall) whenever the cursor isn't on that line; moving onto it shows the Markdown line, to edit, move or delete. Other images and non-attachment links stay text, as the renderer treats them.
- **Uploads:** pasting or dropping files works as today (size checked before upload, each file on its own line): while uploading, an "Uploading screenshot.png…" widget holds the place; on success it becomes the Markdown line, which shows as the image; on failure it is removed and the error toast shows. A non-image file becomes its link line, shown as a file chip with name and size.
- **Ticket suggestions:** typing a project key and a hyphen (`VAQ-`), then digits or words, opens suggestions: by number, or by title words, across projects, from `GET /suggest/tickets?q=` (JSON, same origin, the reader; the same matching as the link box's `LinkTargets`). Choosing one inserts the plain key (`VAQ-12`), which agents write too. The renderer then links ticket keys (below).
- **Keys:** Cmd/Ctrl+Enter saves (submits the form), Escape cancels where a cancel exists; Tab moves focus out (never a tab character), so keyboard navigation and the shortcuts (`?`, `g …`) keep working outside the box.

## Ticket keys in rendered Markdown

`internal/markdown` gains `RenderWith(src, Options{ProjectKeys map[string]bool})`: a `KEY-N` in text (not in code, links or URLs) whose KEY is an existing project's links to `/t/KEY-N` (which redirects old keys). Pages pass the project keys, read once per request. `Render` stays as it is for callers without keys (controller's call: plain keys stay the convention, and "UTF-8" isn't a ticket).

## Pasting into a ticket that doesn't exist yet

- The new-ticket form carries a **draft id** (a ULID minted with the form, a hidden field). Its box uploads to `POST /p/{key}/drafts/{draft}/attachments`: bytes into the blob store under the usual key, and a row in **`pending_attachments`** (draft id, project, actor, attachment id, name, type, size, created). Markdown points at `/attachments/{id}/{name}` as for any attachment; serving a pending one is allowed to its uploader only.
- **Creating the ticket** with that draft id moves its pending rows into `attachments` in core's create transaction (one `ticket.created` event, its payload listing them; no separate `attachment.added` events). Every pending file of the draft is attached, not only those still named in the body, so a file pasted and then deleted from the text still shows in the ticket's files, where it can be removed (controller's call: never lose an upload silently).
- **Unclaimed drafts:** the sweep deletes pending rows and their bytes older than 24 hours, with the orphan cleanup.
- Only core writes `pending_attachments` (an upload is core's `AddPendingAttachment`, **with no event** (owner, 2026-11-26): a pending file isn't tracker state yet, like a claim's heartbeat; the second event-free write, recorded in CLAUDE.md beside heartbeats). History shows the files when the ticket is created.
- **The rest of the attachment code knows about pending files** (controller's calls): the 1-hour orphan sweep treats a key with a `pending_attachments` row as referenced; the 24-hour draft cleanup removes rows and bytes; `vaquero attachments move` copies pending files with the rest and its `--delete-old` keeps them deletable; pending files aren't exported (an export is of tracker state); the blob-backend lock counts them as attachments; serving and previews treat a pending id like an attachment, visible to its uploader only.

## Click to edit

- **On the ticket page:** the title, the description and one's own comments. Clicking the text (not a link, an image, a ticket key or a checkbox-like marker inside it) swaps the rendered text for the editor in place, sized to it, with the cursor near where the click landed; a small Edit control shows on hover and focus, and Enter on it does the same. A ticket with an empty description shows "Add a description" as the click target.
- **Save:** a Save button and Cmd/Ctrl+Enter post through htmx with the ticket's version to small partial routes (`POST /t/{ticket}/title`, `POST /t/{ticket}/description`, each a `core.PatchTicket` naming only that field, so nothing else is cleared; a comment to the existing comment edit), answering the section's fragment (controller's call) (the title and description to the ticket update, a comment to the comment edit); the answer swaps the section back to rendered text. **Cancel:** a button and Escape; with changes, Escape asks in `#vq-confirm` first.
- **Conflicts:** a stale version answers with the editor still open, the typed text kept, and the edit form's banner ("This ticket changed since you opened it. …") with a link to see the current text. A claim note shows above the editor when someone else holds a live claim, as on the edit page.
- **Live updates:** while any editor is open on the page, the page's live refreshes wait (as the board's do during a drag) and run when it closes.
- **Without JavaScript:** nothing is clickable; an Edit link beside the title goes to the edit page, which stays for every field.
- Comments by others aren't editable (core's rule); the owner's moderation, if any, is unchanged.

## Constraints

- No `style=`, no inline script, no `<svg>` in templates; theme tokens only.
- `TestWorkflowOffUnchanged` holds (no change to ticket JSON or the board).
- Help: the `markdown` article says images show in place and how ticket suggestions work; `keyboard-shortcuts` lists Cmd/Ctrl+Enter and Escape in editors; `attachments` says pasting works on a new ticket.
- e2e (chromedp): paste an image into each box (new ticket included) and see an `<img>` in the editor; the saved body is the expected Markdown byte for byte; suggestions on `VAQ-`; click to edit, save, cancel with confirm, a conflict keeping text; a live change waiting while editing; no CSP refusal; 375 px and 1280 px, both themes.

## Left to the builder

- The build script's tooling (it runs only when the editor bundle is updated, never in `make gen` or the gate) and the bundle's exact package list.
- The editor's class names and sizing.
- How the cursor position is mapped from the click.
