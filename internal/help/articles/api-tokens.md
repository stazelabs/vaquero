---
title: API tokens
section: api-and-agents
summary: Making, using and revoking the tokens that let the command line, scripts and agents use the API.
order: 2
---
An API token lets a program act in Vaquero as you: read projects and tickets, and make changes. Treat it like a password.

## Making a token

On the Admin page, open API tokens, give the token a name that says where it will be used ("laptop scripts", "build agent") and create it. Or run `vaquero token new NAME`. The token starts with `vq_` and is shown once: copy it then, because Vaquero keeps only a fingerprint of it and can't show it again.

A token made this way acts as you. The Admin page can make only that kind. To make a token that acts as an agent, run `vaquero token new NAME --agent AGENT`: Vaquero makes the agent the first time its name is used, and the token is made for it. Whatever it does is recorded under that agent's name, and it keeps acting as that agent whatever name a request gives. `vaquero token list` shows whose each token is.

## Using it

Send it as `Authorization: Bearer vq_...` with every request to [the API](/help/api). For the command line on another machine, save it as a server: `vaquero server new NAME URL TOKEN`, or `vaquero server new NAME URL -` to paste it on standard input instead.

## The local token

On this machine you don't need to make one for the command line. The server keeps a token called "local" in the data directory's `token` file (readable only by you), and the command line reads it from there. If that token is revoked, the server writes a new one, and the command line picks it up on its next command. Making a new local token revokes any other token of yours named "local", so give tokens you make yourself another name.

## Seeing and revoking tokens

The API tokens page (and `vaquero token list`) shows each token's name, the first few characters of it, whose it is, when it was made and when it was last used. Delete one there, or with `vaquero token delete PREFIX`; it stops working at once.

## Who can use the local server

In local mode there is no sign-in: the server trusts every program on this computer that can reach it, including programs run by other people with accounts on it, and any of them can make itself a token on the API tokens page. Local mode is meant for a personal machine. On a computer other people use, run Vaquero in hosted mode, which has sign-in; hosted mode comes in a later release, so until then, don't run Vaquero on a shared computer.
