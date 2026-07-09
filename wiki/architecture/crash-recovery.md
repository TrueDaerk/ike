---
type: concept
title: Crash Recovery
description: Vim-swapfile-style crash recovery — debounced full-text snapshots of dirty buffers, written atomically to the project state dir, restored on next launch.
resource: internal/backup
tags: [architecture, backup, crash-recovery, persistence]
timestamp: 2026-07-09T20:30:00Z
---

# Crash Recovery

If IKE (or the terminal) dies with unsaved edits, those edits are gone. Roadmap
0210 adds a vim-swapfile-style safety net: a **backup service** periodically
snapshots *dirty* buffers, and a restore flow on the next launch offers to
recover anything a previous session left behind. Auto-save (#174) and persistent
undo (#148) are adjacent but neither covers a crash mid-edit.

The subsystem lives in `internal/backup` and is deliberately dumb: full text, not
deltas, so recovery never depends on replaying edit history correctly.

## Snapshot format

One file per document, named `<sha256(key)>.ikebak` so the name is
filesystem-safe and fixed-length regardless of the original path. Each file is a
magic line, a few `key: value` header lines, a blank line, then the **full text
verbatim** (further blank lines belong to the text — only the first blank line
terminates the header):

```
IKEBAK1
key: /abs/path/to/file.go      # stable buffer identity (drives the filename)
path: /abs/path/to/file.go     # on-disk base file; empty for an untitled buffer
has_base: true                 # false ⇒ "no base file" (untitled/new buffer)
base_mtime: 2026-07-01T09:00:00Z   # mtime of the on-disk base version
base_hash: <sha256 hex>            # hash of the on-disk base version
timestamp: 2026-07-09T12:00:00Z    # when the snapshot was taken

<full buffer text…>
```

`BaseInfo(path)` stats and hashes the on-disk file to fill `base_mtime` /
`base_hash`; the restore flow (#166) compares them against the file at launch to
warn when the base changed since the snapshot was taken.

## Design

The package splits **timing** from **I/O**, and neither holds editor state:

- **`Debouncer`** tracks a per-key deadline. The caller `Mark`s a key on every
  edit (only dirty buffers are marked) and asks which keys are `Due` at a given
  time. A burst of edits collapses into one pending snapshot that fires after the
  buffer has been quiet for the debounce interval (~2s default). It holds no
  timer — the caller supplies "now" (the real clock in the app, a fake clock in
  tests), which keeps the timing fully unit-testable. Clean buffers are never
  marked, so nothing is written when nothing is dirty.
- **`Service`** reads and writes snapshot files under a directory (`Dir(base)` =
  `<state dir>/backups`, a sibling of `layout.json` / `session.json`, never
  inside the project tree — no `.swp` litter, no 0140 watcher self-events).
  `Snapshot` writes **atomically** (temp file in the same dir → fsync → rename →
  best-effort dir fsync) so a reader never sees a half-written file. `Remove`
  deletes a snapshot (missing is not an error); `List` returns every readable
  snapshot oldest-first, skipping junk and malformed files.

## Lifecycle

A snapshot's life is tied to the dirty flag:

- **Created / refreshed** while a buffer is dirty, debounced after the last edit.
- **Removed** on save, on close-with-discard, and on clean shutdown (the quit
  path already walks open buffers).
- **Leftover on startup ⇒ the previous session died**: the restore flow lists
  them at launch and prompts per file, then age-based GC prunes the rest (#167).

The write side (marking a buffer on the change seam, snapshotting due buffers off
the Update loop as a `tea.Cmd`, and `Cancel` + `Remove` on save / discard / quit)
is the app's event-loop integration; this concept documents the service subsystem
(#165) plus the restore flow (#166).

## Restore flow (#166, `internal/app/recovery.go`)

At launch the root model scans the snapshot directory (`scanRecovery`, in the
constructor). If any snapshots are found, once the window is sized it opens a
floating prompt (`maybeOpenRecovery`) that reuses the save-conflict UX — a modal
that owns the keyboard until dismissed. The prompt lists every recoverable file
with a cursor and a per-file base-changed warning:

- **`r` restore** — opens the recovered text as a **dirty** buffer (`RestoreText`
  on the editor): onto the base file for a titled buffer (Load establishes the
  path, then the recovered text overwrites it), or into a fresh untitled editor
  for a "no base file" snapshot. The snapshot is then removed.
- **`d` discard** — deletes the snapshot without opening it.
- **`s` skip** — leaves the snapshot for the next launch.
- **`j`/`k`** move the cursor; **`esc`** skips all remaining (keeps them).

**Base-changed detection** (`baseChanged`) compares the on-disk file's current
hash (mtime as a fallback) against the snapshot's `base_hash` / `base_mtime`; a
mismatch — or a missing base file — is flagged inline so the user knows the file
moved on under the recovered edits. A diff option joins once the diff viewer
(#60) lands.

## Configuration & privacy

A `[backup]` config section (#167) will expose `enable`, `debounce_ms`, and
`max_age_days` (default 7, pruned at startup after the restore prompt).
**Privacy:** snapshots contain file contents; `enable=false` turns the subsystem
fully off and purges existing snapshots.
