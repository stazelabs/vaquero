---
title: Attachments
section: using
summary: Attach files to tickets, how they are shown and served, and where their bytes are kept.
order: 6
---
A ticket can carry files: screenshots, logs, a spec. They show in the Attachments list on the ticket page, and a description or comment can refer to them.

## What you can attach

Any file up to 10 MB. A larger one is refused with "Files can be at most 10 MB." and nothing is stored. A file's type is read from its contents, never from its name or from what the sender says it is.

On the ticket page, drop files on the Attachments area, choose them with Choose files, or paste them into a description or comment: a pasted image becomes an image in the text, any other file becomes a link. A new ticket can take files once it exists.

From the command line:

    vaquero attach VAQ-12 screenshot.png trace.log
    vaquero attachments VAQ-12
    vaquero detach 01M89WJCM72AW7ZD4DHSKJ0RQ1

`vaquero attach` prints a Markdown reference for each file, ready to paste into a description or comment. `-` reads standard input, with `--name` for the file's name. See [The command line](/help/cli). Over the API, upload with a multipart form; see [The API](/help/api).

Only the person who attached a file, or the owner, can remove it.

## Images on the ticket page

Images come first in the Attachments list, as a gallery of small previews with their names. Click a preview to open the full image in a new tab. Other files are listed below the gallery.

Vaquero makes each preview itself, when the image is uploaded, so the page loads small files rather than every full-size image. A preview fits within 192 by 192 pixels and is never larger than the image. PNG, JPEG and GIF files (a GIF's first frame) get one, turned upright when a photo says it was taken sideways. A WebP image, an image over 40 megapixels, or one that can't be read shows the original instead.

A preview lives at `/thumbs/ID`. Previews are not part of an export: after an import, each is made again the first time it is shown. Removing a file removes its preview.

## How files are served

A file lives at `/attachments/ID/NAME` on the server, with no project key, so a reference keeps working if the project's key changes. A wrong name, or an id in lower case, redirects to the right address.

PNG, JPEG, GIF and WebP files show in the page. Every other file, including SVG and HTML, is a download: it is never shown inside Vaquero, whatever it is named. Markdown shows an image only when it points at an attachment; an image from anywhere else becomes a plain link. See [Writing in Markdown](/help/markdown#images-and-attachments).

Every file answer carries `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`. A browser checks with the server each time it shows a file, so a removed file stops showing.

## Where the bytes are kept

By default, in the database file, with everything else. These settings move them, chosen when the server starts, from the environment or from `vaquero.env` in the data directory (see [Installing and upgrading](/help/installing-and-upgrading#settings-for-the-server)):

| Variable | Meaning |
|---|---|
| `VQ_BLOB_STORE` | `sqlite` (the default), `dir` or `s3` |
| `VQ_BLOB_DIR` | For `dir`: the directory; default `attachments` inside the data directory |
| `VQ_S3_ENDPOINT` | For `s3`: a host or host:port of an S3-compatible service, with no `https://` and no bucket path |
| `VQ_S3_BUCKET` | For `s3`: the bucket |
| `VQ_S3_ACCESS_KEY_ID` | For `s3`: the access key |
| `VQ_S3_SECRET_ACCESS_KEY` | For `s3`: the secret; never logged |
| `VQ_S3_REGION` | For `s3`: the region; default `auto` |

All four of the endpoint, bucket, key and secret are needed for `s3`. Once a server has stored attachments, it refuses to start with a different `VQ_BLOB_STORE`, or the same one pointed somewhere else (another `VQ_BLOB_DIR`, or another S3 endpoint or bucket), saying where the files are, since their bytes would be out of reach. To move attachments to other storage, stop the server and run `vaquero attachments move`; see [Your data and backups](/help/data-and-backups#moving-attachments-to-other-storage).

## Backups

Snapshots and `vaquero backup restore` cover the database. With `sqlite` storage that includes every attachment. With `dir` or `s3` storage the files are not in the database, so a snapshot holds only the records of them: backing up the directory or the bucket is yours to do. Restoring an older snapshot makes the files of newer attachments orphans, which the sweep deletes after an hour, so undoing that restore leaves records without files. See [Your data and backups](/help/data-and-backups#backups).

With `sqlite` storage, deleting an attachment doesn't shrink the database file; `vaquero backup compact` gives the space back. See [Your data and backups](/help/data-and-backups#compacting-the-database).
