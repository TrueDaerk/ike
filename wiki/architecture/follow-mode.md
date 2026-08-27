---
type: concept
title: Follow Mode (tail -f)
description: Per-view follow mode for buffers — streams appended file content read-only with the viewport stuck to the end, pause-on-scroll, a live filter/highlight over the tail, incremental log analyses, and truncation/rotation handling (#1928, #2255).
resource: internal/editor/follow.go
tags: [architecture, editor, log, watch, follow, filter]
timestamp: 2026-08-27T00:00:00Z
---

# Follow Mode (tail -f)

Follow mode (#1928) turns a buffer into a live tail: `view.toggleFollow`
(palette, `alt+shift+f`) watches the open file for appended content and
streams new lines into the buffer while the viewport sticks to the end — the
standard `less F` / IDE-console behavior, without leaving IKE. It composes
with the log stack: [log rendering](/architecture/languages.md) highlights,
repeat-run folding and inter-line deltas keep applying as lines arrive.

## Semantics

- **Per view, read-only.** The toggle flips one view; the buffer refuses
  edits and writes while following (`readOnly`, restored on toggle-off), so
  the streaming append never has to negotiate with the undo model. A dirty
  buffer refuses to enter follow mode; a view loading another file leaves it.
- **Enable = sync.** Toggling on re-reads the file once (through the normal
  reload path, a no-op on identical content), anchors `followOffset` at its
  byte length and jumps cursor + viewport to the end.
- **Pause on scroll.** After any user-driven movement — keys and palette
  actions via the editor `Update` wrapper, wheel (`ScrollBy`) and scrollbar
  jumps/drags directly — the paused flag re-derives from one predicate:
  cursor on the last line *and* the viewport showing it. Moving away pauses
  (appends keep streaming, the view stays put), jumping back (`G`, scrolling
  down) resumes. The status line shows `FOLLOW` / `FOLLOW (paused)`
  (`internal/app/statusline.go`). "The end" means the last line the view
  *shows*: under a live filter it is the last matching one, so the pause
  predicate and the auto-scroll both follow the filtered tail.

## Live filter & highlight

A busy stream cannot be narrowed by scrolling — it keeps growing. The filter
line (#2255, `internal/editor/logfilter.go`) narrows or marks it as it
arrives:

- `view.followFilter` (`alt+shift+g`, palette) opens the `|` line: every
  non-matching line is hidden, existing content and new appends alike.
- `view.followHighlight` (palette) opens the `*` line: nothing is hidden, the
  matches take a warning-tinted background instead.
- `view.clearFollowFilter` (palette), an emptied pattern, or leaving follow
  mode restores the whole stream.

The pattern language is the search line's: a plain substring by default, `\v`
switching to a regex (`ctrl+r` writes the marker), `\c` / `\C` forcing the
case mode over smartcase. Unlike the search line, an invalid regex is *not*
silently demoted to a literal — a filter that quietly matched something else
would hide the wrong half of the log — it reports inline on the filter line
and leaves the stream unfiltered until it compiles. Typing is live: the view
re-narrows per keystroke, and Esc restores the filter that was active when the
line opened.

The filter is a **view** concern, like folding: the buffer keeps every line
(clearing restores everything), a shared view of the same document filters
independently, and hiding rides the fold machinery — `lineHidden` counts a
filtered-out line exactly like a collapsed fold body, so motions, scrolling,
mouse mapping and the render loop need no special case. A [merged rotation
set](/architecture/log-timeline.md) follows the same pipeline and so filters
the same way, over its older members included.

The status line carries the state next to the follow badge:
`FILTER error (12)`, `HIGHLIGHT ~w\d+ (3)`, `FILTER foo (no matches)`, or the
compile error of a broken pattern. The match count is cached per document
version and **extended** over appends (`logFilterState`, mirroring
`extendLogRuns`): recounting a tailed log per poll is exactly the
O(document)-per-append work #2163 removed from this path.

## Incremental appends

Watcher events for the followed path divert from the reload flow
(`handleExternalChange` → `followHandleEvent`). A grown file is read only
past `followOffset`; the decoded tail continues an unterminated last line in
place (`followTerm`) and appends the rest as new lines. A trailing lone `\r`
or split UTF-8 rune is held back until the next write completes it
(`splitIncompleteTail`); non-UTF-8 encodings reload instead of appending —
chunk boundaries are not safe to decode incrementally.

The append flows through the normal `EventChange` path (docVersion bump,
shared-view sync, LSP), and highlighting re-parses off-loop as usual. The
repeat-run cache is extended *incrementally* (`extendLogRuns`,
`internal/editor/logfold.go`): only the tail from the first new/changed line
is rescanned, seeded with the run state above it, so an append costs the new
lines — not the whole buffer.

## Truncation & rotation

- A file **smaller** than `followOffset` (truncation, copytruncate rotation)
  reloads wholesale, re-anchors the offset, and toasts.
- A **removed** file marks the rotation pending — the pane stays open (the
  app skips its externally-deleted close for following editors) and the poll
  stamp is refreshed so the replacement is seen; the next create/change
  event reloads from the new file, with a toast.
- `FileCreated` under a follower always means a rename-style rotation (IKE
  itself never writes a followed, read-only buffer) and reloads wholesale.

## Events & the follow tick

Follow mode reuses the external-file-change service (`internal/watch`):
fsnotify covers files under the watched root, and the poll fallback
(`Track`/`Poll`, mtime+size, hash below the large-file cap) covers the rest.
The app half (`internal/app/follow.go`) arms **one** demand-armed tick on the
editor's `FollowMsg`, polls off-loop each `editor.follow_poll_ms`
(default 500, clamped 100–10000, Settings → Editor), and re-arms only while
some view still follows — no idle cost when nothing does, per the
[performance rules](/architecture/performance.md).

## Merged rotation sets

A [merged rotated log timeline](/architecture/log-timeline.md) (#1996) is a
buffer assembled from several files, whose own path names nothing on disk. It
follows the set's **newest member** instead (`followSrc`, `followTarget` in
`internal/editor/mergedlog.go`): enabling costs no read (the merge handed over
the member's end offset), appends stream into the buffer's tail as usual, and
the wholesale-reload cases turn into a re-merge request to the root model —
a rotated file's lines belong *after* the ones the buffer holds, which no
append can express. Watcher events reach such a view by follow source rather
than by path.

Out of scope (follow-ups): following remote files over SSH, SFTP editing.
