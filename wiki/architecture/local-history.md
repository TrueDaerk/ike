---
type: concept
title: Local History
description: Per-project file snapshots on every save, with a floating picker to diff a snapshot against the current buffer or restore it through the undoable edit path, plus the per-file Timeline merging those snapshots with the file's git history
resource: internal/localhistory/localhistory.go
tags: [history, snapshots, diff, restore, persistence, timeline, git]
timestamp: 2026-08-17T00:00:00Z
---

# Local History

JetBrains-style local history, MVP slice (#1023, part of #35): every successful
editor save records the saved file into a per-project snapshot store, and the
`file.localHistory` command ("Show Local History") lets the user browse those
snapshots for the focused file, diff one against the current buffer, or restore
it into the buffer. It is cheap insurance independent of git — no commits
required.

Out of scope for the MVP (they stay in the umbrella idea #35): manual labels
("before refactor") and snapshots at unsaved-edit intervals.

## Store (`internal/localhistory`)

The store lives under the same per-project state directory as the layout and
session stores: `.ike/history/` (or `$IKE_CONFIG_DIR/history/` when the
override is set).

- `history/index.json` — per-file metadata: path → list of `{ts, hash}`
  entries, oldest-first. Paths are canonicalized (absolute), so relative and
  absolute spellings of the same file share one history. An entry may carry an
  optional `label` ("before refactor") that the Timeline renders where present;
  saves record none — naming a snapshot is still part of the umbrella idea #35 —
  but a labelled entry keeps its name across prunes.
- `history/objects/<sha256>` — content blobs, content-addressed. Identical
  content stores one blob, whether across consecutive saves of one file
  (those also skip the index entry — consecutive-save dedupe) or across
  different files.

Pruning runs at record time with two caps, both overridable on the `Store`:

- **Count:** at most 50 snapshots per file (`DefaultMaxPerFile`), oldest
  dropped first.
- **Age:** snapshots older than 30 days drop out (`DefaultMaxAge`).

Every record prunes **every** file's list (#1548), not only the saved
path's, so untouched paths age out too and emptied keys leave the index —
otherwise the index accumulated one key per path ever saved, with their
blobs referenced forever. Blobs no index entry references anymore are
garbage-collected on the record that dropped their last reference; a record
that prunes nothing skips the objects-directory sweep entirely. `Record`
swallows I/O errors — failing to snapshot must never disrupt the save that
triggered it; a missing or malformed index reads as empty.

## Save hook (`internal/app/localhistory.go`)

Every save flow — manual `editor.write`, Save All, focus-loss and idle
autosave, save-as — funnels through the editor's `saveAs`, which emits
`EventSave`. The app-side editor emitter forwards that as
`localHistorySnapshotMsg`, whose handler reads the just-written file and
records it. One central hook, so no save path can miss a snapshot.

## Picker, diff, restore

`file.localHistory` opens a floating modal (the shared `ui.Floating` shell,
pins-picker pattern) listing the focused file's snapshots newest-first with
humanized timestamps ("5m ago") plus the absolute time.

- `j`/`k` (or arrows) move the selection; `esc`/`q` closes.
- `enter` opens the reusable diff pane (#60) with the snapshot on the left
  ("name @ 5m ago") and the live buffer on the right, following the
  vcs.diff single-slot reuse behavior.
- `r` restores the snapshot into the buffer **through the normal edit path**
  (`ApplyTextEdits`, one history change): the buffer marks dirty, a single
  undo reverts the restore, and the file on disk is untouched until the next
  save (which itself records a snapshot, so restore is never destructive).

Snapshot bytes are normalized before diff/restore — decoded via `textenc`,
line endings folded to LF, final newline trimmed — to match the buffer's
native form; the save path re-applies the file's stored EOL/encoding flavor.
`normalizeBufferText` is that one normalization, shared with the Timeline's
git blobs so both entry types diff against the buffer on equal terms.

## Timeline (`internal/timeline`, `internal/app/timeline.go`)

`file.timeline` ("Show Timeline", #1916) is the same history question asked
across both sources: VS Code's Timeline in one keyboard-first modal, listing
the focused file's local-history snapshots **and** the git commits that touched
it on one chronological axis.

- **Merge layer** (`internal/timeline`) is pure — converters from
  `localhistory.Entry` / `vcs.FileLogEntry` into a common `Entry`, the source
  `Filter`, and `Merge`, which orders newest-first. At an identical timestamp
  the snapshot ranks as the **later** event and comes first: a commit and the
  save of its content share a second only when the save produced what was
  committed. Entries of the same source and timestamp keep their input order,
  so a loaded git window never reshuffles what is already on screen.
- **Git side** (`vcs.FileLogCmd`) is `git log --follow --name-only`, async and
  timeout-bounded like every other read. Each commit carries the path the file
  had *in that commit*, so `git show <hash>:<path>` resolves across renames.
  The window is cut in-process instead of with `--skip`: git's `--skip` and
  `--follow` do not compose — a skip past the rename returns nothing at all.
- **Incremental loading:** the view opens on the snapshots the store already
  has (a synchronous read) and the first commit window (50) arrives as a
  message; walking the selection toward the end of the list — or pressing `L` —
  pulls the next one in. A failing git read leaves the local half usable.
- **Degenerate files:** an untracked file (or one outside the repo) shows
  snapshots only and never queries git; a file that was never saved shows
  commits only. A file with neither notifies instead of opening an empty list.

Keys: `j`/`k` move, `enter` diffs the selected entry against the live buffer,
`m` marks an entry and `d` diffs it against the selected one (across sources —
snapshot vs commit; the older side goes left, and neither being the working
tree the diff is read-only), `r` restores a snapshot through the local-history
restore path, `y` copies a commit's full hash, `f` cycles the source filter for
the open list, `esc` closes. Every comparison lands in the reusable diff pane
via the shared `openDiffTexts` (single-slot reuse, otherwise a titled split).

The filter the view **opens** with is the `history.timeline_source` setting
(Settings → Timeline): `both` (default), `local` or `git`.
