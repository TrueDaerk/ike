---
type: concept
title: Merged Rotated Log Timeline
description: "#1996 — a rotated log set (app.log, app.log.1, app.log.2026-08-01, app.log.2.gz) opens as one chronological read-only buffer: suffix-based ordering, gz members decompressed, origin separators per region, follow mode tailing the newest member across a rotation, both large-file caps filling the budget from the newest end."
resource: internal/logset
tags: [architecture, log, rotation, follow, read-only, large-file, gzip]
timestamp: 2026-08-20T00:00:00Z
---

# Merged Rotated Log Timeline (#1996)

A rotated log is one log split across files. `app.log` holds the last hour,
`app.log.1` the hour before, `app.log.2.gz` the day before that — and the
question an investigation asks ("what happened around midnight") lives on both
sides of a rotation boundary. IKE already *recognized* rotated files as logs
(#1745) and could tail the live one (#1928); #1996 opens the whole set as one
chronological buffer.

`log.openRotatedSet` ("Open Rotated Log Set (Merged Timeline)") merges the set
behind the focused buffer — from any member, the live log or a rotated one — and
opening a log that has siblings offers it once per path per session.

Three files carry it:

- `internal/logset` — detection, ordering, merging. The format layer: no UI, no
  editor.
- `internal/app/logsets.go` — the command, the buffer install, the toasts, the
  follow wiring.
- `internal/editor/mergedlog.go` — the editor seam: what a merged buffer is and
  how follow mode behaves on one.

## What counts as a set

`logset.Stem` reduces a member name to the live log's name, and every file in
the same directory reducing to that stem is a member. The shapes are the ones
the [language lookup](./languages.md) already recognizes (#1745), so detection
and highlighting cannot disagree:

- a sequence number — `app.log.1`, `app.log.42`;
- a date stamp — `app.log.2026-08-01`, `app.log.20260801`;
- either of those with `.gz`/`.gzip` on top — `app.log.2.gz`;
- the stem itself, plain (`app.log`, the live log) or compressed (`app.log.gz`,
  a rotation that kept the name).

The remainder has to keep an extension of its own: `backup.1` and `notes.gz`
reduce to `backup`/`notes`, which name no log, so they are nobody's set — the
same rule that keeps `.1` from turning every numbered file into a log buffer.
An eight-digit suffix that is no valid date reads as a (large) sequence number
rather than being rejected; the reading only affects ordering *inside* the set.

A set of exactly one file is still a set — `Rotated()` is what the command asks
before merging, so an unrotated log says "no rotated log set next to app.log"
instead of opening a second view of the same content.

## Ordering: rotation order, mtime as the fallback

Members are ordered **oldest first, the live log last**:

- dated members by date, ascending;
- numbered members by descending number (`.2` is older than `.1`);
- the live log is always the newest, whatever its own mtime says.

A directory mixing the two spellings — or holding a name-keeping `app.log.gz`,
which carries no rotation rank at all — has no suffix order to read, so the
whole set falls back to the modification times (name as the tiebreak). That
choice is made **once per set**, not per comparison, which is what keeps the
comparison a total order however mixed the directory is.

## The merged buffer

The content is every member's lines in order, each region opened by an **origin
separator** naming its file:

```
──── app.log.2.gz ────
2026-08-08 08:00:00 INFO  compressed line
──── app.log.1 ────
2026-08-09 09:00:00 INFO  yesterday
──── app.log ────
2026-08-10 10:00:00 INFO  live
```

`logline.OriginLine` writes the separator and `logline.OriginName` recognizes
it again, so the two sides share one format. A recognized separator captures
whole-line as `log.origin` (accent colour, `logrender.go`) and is *not* parsed
as a log line — it is a span, not buffer structure, so toggling log rendering
off shows it raw like every other layer. Everything else in the log stack keeps
working across the whole buffer: severity colours, logfmt pairs, rainbow
threads, repeat folding, the inter-line deltas (a separator carries no
timestamp, so it shows no hint and does not break the delta chain — the first
line of a region measures against the last stamped line of the one above it,
which is exactly the boundary gap worth seeing).

The buffer is **read-only** (`editor.ShowMergedLog` over `ShowReadOnly`, the
[archive-viewer](./archive-viewer.md#read-only-buffers) seam): there is no file
to write it back to. Its path is virtual in the same shape the other previews
use — `/var/log/app.log!merged/app.log` — whose tail is the set's own file
name, so `filepath.Ext` gives the language, the highlighting and the tab label
with no special casing. Tab and pane title read `app.log (merged) [RO]`, which
is what tells it apart from a plain open of the same file. Like every read-only
preview it is session-local: not persisted with a pane's tabs, kept out of the
reopen ring.

Re-running the command on a merged buffer re-merges the set into the same tab —
that is its refresh.

## Caps: the budget is filled from the newest end

Merging reads (and decompresses) files, so it runs **off the Update loop** as a
command and lands as a message: a set of a dozen members must never stall a
keystroke. Both [large-file thresholds](./editor.md) (#149) bound what it
reads, the same ceilings a single file gets:

- `files.large_file_kb` caps the bytes,
- `files.large_file_lines` caps the lines.

The order the budget is spent in is the design decision: members are read
**newest first** and both budgets cut **from the front** of a region. What a
cap costs is therefore the *oldest* end of the timeline — never the lines next
to the live log, which is the end the boundary question is about. A cut shows
as a warning toast naming what was lost ("3 older file(s) omitted at the
large-file limit"), and the merged buffer itself is subject to the ordinary
large-file degradation once it crosses a threshold.

Compressed members go through `gzfile.Read`, so the decompressed-byte cap is
the [bomb guard](./gz-viewer.md#caps-the-decompression-bomb-guard) here too. A
member that cannot be read at all (a corrupt `.gz`, no permission) is skipped
and named in the same toast; only a set yielding *nothing* is an error.

## Follow mode across a rotation

[Follow mode](./follow-mode.md) tails a file, and a merged buffer has none — so
it tails the set's **newest member** (`followSrc`, `followTarget`) while the
buffer keeps its virtual path:

- **Enabling** costs no read. The merge already read that member to its end and
  handed the offset over (`Merged.Tail`/`TailTerm`), so the buffer is anchored
  where it stands; re-reading the source the way a plain open does would throw
  the older members away.
- **Appends** stream in through the ordinary incremental path. The newest
  member's lines are the buffer's last ones, so an append lands exactly where
  it belongs — including the partial-line continuation and the incremental
  repeat-run extension.
- **A rotation** (the newest member removed, re-created or truncated) is the
  one case an append cannot express: the replacement's lines belong *after* the
  ones already in the buffer, which is a new merge of the whole set. The view
  emits `MergeLogSetMsg` and parks its event handling (`mergeWait`) until the
  fresh merge lands; the root model re-detects the set — the rotated-away
  content is now `app.log.1` and merges into its own region — re-installs the
  content in place and keeps follow armed (paused or not, as the user left it).
  A merge that fails re-anchors the follower at the source's current end
  instead of stranding it on a request that never lands.
- Watcher events reach the view by **follow source** rather than by path
  (`routeMergedLogFollow`), since the buffer's path names no file — the same
  routing problem the [gz viewer](./gz-viewer.md#read-only-and-reload) solves
  for its previews. The set's newest member is `Track`ed with the watcher so
  the poll fallback compares something that exists on disk, and a removed
  source is re-tracked exactly as it is for a single-file follower.
- A set whose newest member is **compressed** offers no byte offset to resume
  from, so follow refuses with a message rather than tailing bytes that are not
  the buffer's.

## Boundaries

- **A snapshot unless followed.** A merged buffer that is not in follow mode is
  the set as of the last merge; it does not re-merge on every write to the live
  log. Re-running the command (or following) is how it catches up — re-merging
  a multi-file set per append would cost far more than it tells.
- **Read-only, always.** No writing back into a rotation set.
- **One directory.** Members are found next to the stem; a set spread over
  directories (an archive directory per day) is out of scope.
- **bzip2, xz, zstd** members are out of scope for the same reason the [gz
  viewer](./gz-viewer.md#boundaries) leaves them out.
