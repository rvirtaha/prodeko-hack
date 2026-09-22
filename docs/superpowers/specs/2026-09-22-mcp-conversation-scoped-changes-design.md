# Conversation-scoped changes for the content editor MCP

A new conversation with the editor starts on a clean checkout of the site.
Yesterday's half-finished change is never silently inherited; continuing old
work is an explicit act, and work that was merged disappears on its own.

This revises the change lifecycle described in
[the content editor design](2026-09-19-content-editor-mcp-design.md). The
tools, the fence, the branch namespace and the review boundary are untouched.

## The problem

The toolset keeps one current change per user and, when its in-memory map is
empty, resumes the newest worktree on disk regardless of that change's state.
The map itself lives as long as the process. Both paths conflate
conversations: a chat opened today lands in the change a chat left behind
yesterday, dirty files and all, even when that change was already submitted or
merged. Observed in practice: a one-line title edit joined a day-old change
carrying unsubmitted edits to eight unrelated files, and the only way out was
the model noticing and asking.

Two defects compound. The server has no notion of a conversation, so the only
scope it can bind a change to is the user. And a change has no end of life:
merged and closed pull requests keep their worktrees, keep counting toward the
three-change cap, and remain candidates for resumption.

## The design in one paragraph

The current-change binding expires when idle, disk-resume is removed, and
nothing is resumed implicitly. Reads serve from a shared checkout of the base
branch, so only a write opens a change. A `resume_change` tool reattaches to
an open change by slug or pull request number, the first call of a fresh
working session mentions any open changes so the model can offer to continue
one, and a change whose pull request is merged or closed archives itself.

## Binding with an idle expiry

`current[user]` becomes a binding that carries the change and a last-used
timestamp. Every tool call through the binding refreshes the timestamp; a
binding idle longer than 45 minutes is dropped on next access. The binding is
set by exactly two events: the first write of a session, which opens a fresh
change, and `resume_change`. `workdir.Manager.Resume` and its call site go
away — the map is no longer a cache of what is on disk but the only binding
there is.

The first write opens a change that is nobody else's. Its name comes from the
file that was edited, and a file name repeats: a name already taken by a
worktree or a branch takes a number, so today's `main.css` edit cannot land in
the `main` change yesterday left behind. Opening is serialised per person, so
the two tool calls of one turn cannot each open one.

A working session starts when a tool call arrives for a user with no live
binding — none was ever set, or the last one expired or was dropped. That
moment is tracked per user, because two things key on it: the proactive note
and the base view refresh. The binding itself may only be set later, by the
session's first write.

Consequences, accepted deliberately:

- A server restart or an expiry mid-conversation means the next edit opens a
  fresh change. The proactive note names the half-done one and the model can
  resume it. Visible and recoverable, where the old behaviour was silently
  wrong in the more common case.
- One person running two conversations inside the same 45 minutes still
  shares one binding. The server cannot tell simultaneous conversations apart
  without MCP session ids. Out of scope; a later spike may implement
  `Mcp-Session-Id` and observe what claude.ai actually sends. The binding
  keyed by user is the structure a session key would slot into.

## Reads without a change

Every read currently opens a change, which under fresh-per-conversation would
mint a worktree per conversation and exhaust the cap. Instead the manager
keeps one shared read-only view: a worktree at `wt/.base` on a detached
checkout of `origin/HEAD`. The leading dot keeps it outside the username
alphabet, so it can never collide with a user's directory.

The base view serves `list_files`, `read_file`, `search`,
`translation_status`, and also `build`, `render` and `screenshot` with a build
root of its own, so a conversation that only wants to look at the site never
creates a change. It refreshes with fetch and reset the first time a working
session touches it, and one lock serialises refresh, reads and builds: a
refresh or a build has it to itself, reads share it, and a render or screenshot
holds it across the whole answer, since the build root it reads is emptied at
the start of every build anybody runs. Adequate for the two or three concurrent
editors this serves.

Once a binding holds a change, all tools serve from that change, as today.

## resume_change

Arguments: `slug` or `pr`, exactly one. A pull request number resolves through
the GitHub API to its head branch, which must lie inside the caller's own
`media/<user>/` namespace; anything else is refused. The tool reattaches to
the existing worktree, sets the binding, and answers with what the change
contains: files touched against the base, whether uncommitted edits exist,
pull request state, CI state, preview link.

Resuming a change whose pull request is merged or closed is refused with a
sentence saying the work is finished and a new change starts on the next
edit — and the change is archived on the spot. In dry-run mode a pull request
number cannot be resolved, so `resume_change` there accepts only a slug and
says why.

## Self-archiving

Wherever the server already consults GitHub about a change — `list_my_changes`,
`resume_change`, and the cap check when a change is created — a merged or
closed pull request means the change is archived: worktree removed, local
branch deleted, binding dropped if it was current. Merged work stops counting
toward `MaxOpenChanges` and disappears from listings, so the cap self-heals.

The sweep runs behind the person's back, inside an errand they asked for
something else from, so what it takes away has to be work GitHub already has: a
finished change carrying uncommitted edits is left alone, and `abandon_change`
is how somebody says out loud that they are done with those.

In dry-run mode nothing is known about pull requests, so changes leave only
through `abandon_change`, unchanged from today.

## The proactive note

The first tool call of a working session appends a note when the user has
open changes, one line per change: slug, pull request number and state, file
count, dirtiness. It closes with the sentence that `resume_change` continues
one and that otherwise the first edit starts a new change. The note appears
once per session and is suppressed until the binding is next dropped.

The conventions text gains the matching guidance: changes are fresh by
default, resuming is explicit, and a person saying "jatka" or asking to fix
review feedback is the cue to check `list_my_changes` and offer to resume.

## Testing

- Binding expiry in the toolset tests, with the injected clock: a call after
  the idle window opens a fresh change rather than the old one.
- Archiving against the existing fake GitHub: merged and closed pull requests
  remove the worktree and free the cap; open ones do not.
- `resume_change` by slug, by pull request number, refusal of a number whose
  head is outside the caller's namespace, refusal plus archive of a merged
  one.
- Base view: reads before any write touch no user worktree; a build in the
  base view lands in its own build root; the refresh picks up a moved
  `origin/HEAD`.
- The note: present with open changes on the first call of a binding, absent
  on the second, absent when there are none.
