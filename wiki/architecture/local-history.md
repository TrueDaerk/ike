---
type: concept
title: Local History
description: Per-project file snapshots on every save, with a floating picker to diff a snapshot against the current buffer or restore it through the undoable edit path
resource: internal/localhistory/localhistory.go
tags: [history, snapshots, diff, restore, persistence]
timestamp: 2026-07-23T00:00:00Z
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
  absolute spellings of the same file share one history.
- `history/objects/<sha256>` — content blobs, content-addressed. Identical
  content stores one blob, whether across consecutive saves of one file
  (those also skip the index entry — consecutive-save dedupe) or across
  different files.

Pruning runs at record time with two caps, both overridable on the `Store`:

- **Count:** at most 50 snapshots per file (`DefaultMaxPerFile`), oldest
  dropped first.
- **Age:** snapshots older than 30 days drop out (`DefaultMaxAge`).

Blobs no index entry references anymore are garbage-collected on the next
record. `Record` swallows I/O errors — failing to snapshot must never disrupt
the save that triggered it; a missing or malformed index reads as empty.

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
