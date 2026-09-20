---
name: editing-prodeko-site
description: >
  Use when changing prodeko.org through the prodeko-editor MCP — page text, announcements,
  links, navigation, board/partner data, CSS, layout partials, images — or when its submit,
  build, screenshot or list_my_changes output looks wrong ("Dry run", "no edits to submit",
  missing edits, a refused submit, a refused sign-in).
---

# Editing prodeko.org with the prodeko-editor MCP

Every change becomes a draft pull request in the signed-in person's name; a maintainer
merges. `get_conventions` is the authority on layout, the fi/en pairing, design tokens,
voice, and exactly which files are writable. Call it first. This skill covers what it
leaves out.

## Loop

1. `get_conventions`.
2. `search` for the visible text to find the file.
3. `read_file` the template that renders the field, before editing. Front matter values are
   HTML-escaped and not rendered as Markdown, so what a field can do is decided by the
   template, not by what you type into it.
4. `edit_file`, then `build`.
5. For anything visual, look before you submit: `render` for the DOM, `screenshot` for the
   page. Iterate on your own edit until the picture matches the request.
6. `submit`, then read its whole output (see below).

## Layout changes

Partials and page layouts are writable; the skeleton, the asset pipeline, render hooks and
i18n plumbing are not — `get_conventions` states the exact list. The server enforces the
loop: `submit` refuses a layout change that was not built and screenshotted since the last
edit, and requires a description of what looks different. Screenshot both languages (one
call does both when the page has a pair) and say in the PR description what changed
visually.

Hand off to a developer instead of editing when the request needs: a new page type or
section, anything in the asset or resource pipeline, i18n changes, or anything where the
right answer depends on how the theme is wired rather than on what the output looks like.
Say plainly it is a developer job and put what is needed in the PR description.

## Images

`begin_image_upload` returns a link; the person opens it and drops in a JPEG or PNG (at
most 5 MB, link works once, expires in 15 minutes). The upload lands in the current change
under `site/assets/images/` and the upload page tells them the exact path — ask them to say
when it is done, then reference that path from content. Bytes never travel through the
chat; do not ask the person to paste an image into the conversation for uploading.

## Review feedback

When asked to fix what review asked for, start with `get_feedback`: it returns the pull
request's state and every comment verbatim. Address the comments, build, and `submit`
again — it continues the same branch and PR. `abandon_change` throws a change away
(closes the PR, deletes the branch); it is destructive, so confirm with the person and
name the slug from `list_my_changes`.

## When the result looks wrong

| Symptom | Cause | Do |
|---|---|---|
| Text should be a link, but the template prints it in a `<span>` or reads no `url` field | The template decides; if the template is in the writable set, change it (Layout changes above), else it is fenced | Check `get_conventions` for whether the template is writable. If not, tell the user and put the needed change in the PR description. Never put `<a>` or Markdown in front matter. |
| `submit` refuses: not built, or not screenshotted | The server gates layout changes on build and screenshot since the last edit | Run what the refusal names, look at the result, then submit. |
| `submit` output contains "Dry run" | Server has no `GITHUB_TOKEN`: branch pushed to the server's own origin, no PR, no preview | Tell the user no PR exists. The token is host config, theirs to set. |
| "the change has no edits to submit", or your edits are gone | A server restart resets the workspace to upstream | `read_file` again. Upstream may have moved too (new fields, changed templates). Reapply, `build`, `submit`. |
| `list_my_changes` shows "not submitted yet" but `submit` refuses | Same reset | Same. |
| `screenshot` refuses every call | The server has no headless Chromium (local dev setups) | Fall back to `render` and say the visual check could not be run. |
| Sign-in refused, missing realm roles | The account lacks the media role or the membership role | Relay the refusal message: it names the missing roles and who grants them. A lapsed guild membership is the usual cause. |

## Before submitting

- Copy you invented goes out under the user's name. Show it and get a yes first. Never
  invent dates, places or eligibility claims.
- PR title and description in the language of the change, usually Finnish.
- The fi and en pages share a `translationKey` but can hold unrelated content (different
  announcements). State in the description which side you left alone. `translation_status`
  reports which pages are missing their pair or have drifted apart.
- A follow-up edit after `submit` continues the same branch and PR.

## Worked example: make a front-matter field render as a link

Request: "the announcement's linkText should actually link somewhere".

1. `search` for the visible text → it is `announcement.linkText` in
   `site/content/fi/_index.md`.
2. `read_file` the partial that renders it → it prints a `<span>`, no `href`, and the
   front matter has no URL field.
3. The partial is writable: `edit_file` it to read an `announcement.link` field and render
   `<a>` when it is set; `edit_file` the front matter of both language sides to add the
   field.
4. `build`, then `screenshot` the front page — the pair is captured in both languages.
5. `submit` with a Finnish title and a description saying what looks different: "nosto on
   nyt linkki".
