# prodeko-editor plugin

Edit prodeko.org from Claude. The plugin connects the prodeko-editor MCP
server and ships the skill that teaches Claude how the site is edited. Every
change becomes a draft pull request under your own name; a maintainer reviews
and merges, and nothing goes live before that.

What you can do through it: change page text and announcements, edit
navigation, board and partner data, adjust the stylesheet, change the layout
partials and page templates, and upload images. The site answers with builds,
rendered pages and screenshots, so Claude sees what its edit did before
submitting.

## Install

```
/plugin install prodeko-editor
```

or add this repository as a plugin marketplace and install from there. On the
first tool call a browser window opens for sign-in with your Prodeko account
(id.prodeko.org).

## Access

Editing requires two realm roles on your Prodeko account: the media role,
granted by hand, and the membership role, maintained automatically. If
sign-in is refused, the message names the missing role and who to ask; a
lapsed guild membership is the usual cause. The media role is granted by the
guild's IT team.

## What it will not do

Publishing is always a maintainer's merge. New page types, template plumbing
and anything under the site's build pipeline are developer work in the
repository itself; the skill tells Claude to say so rather than improvise.
