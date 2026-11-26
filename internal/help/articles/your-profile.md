---
title: Your profile
section: using
summary: Your display name and avatar, where they show, and how agents are told apart.
order: 9
---
Your profile holds the name and picture Vaquero shows for you. Open it from the avatar in the top bar, or go to `/profile`.

## Your display name

Type a name and choose Save profile. It can be up to 80 characters, with no line breaks. It can't be a name an agent has, in any case, so every name you see belongs to one actor. Leave it empty to show Owner again.

The name shows at once everywhere you appear: ticket history (earlier lines too), comments, the assignee field, board cards, and every API and command line answer. Your account itself is still called Owner, so when you set a ticket's assignee by name, from the API or with `vaquero ticket edit --assignee`, either Owner or your display name works.

## Your avatar

Upload a PNG, JPEG or WebP image of up to 256 KB; it shows round, cropped to fit. Other types, SVG and GIF included, are refused. Without an image, your initials show on a coloured tile. Remove avatar, behind a confirmation, goes back to the initials.

## Agents

Agents have no profile. Each shows its initials on a tile whose colour comes from the agent itself, so two agents look different at a glance and keep their colours.

Only you edit your profile. An agent acting for you, with `--agent`, the `Vaquero-Agent` header or through MCP, is refused with "Only the owner edits the owner's profile."

## From the command line and the API

`vaquero me` shows your profile; `--name`, `--avatar FILE` and `--remove-avatar` change it. See [the command line](/help/cli#commands). The API has `GET` and `PATCH /api/v1/profile` and `PUT` and `DELETE /api/v1/profile/avatar`; see [the API](/help/api#your-profile).
