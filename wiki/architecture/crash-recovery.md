---
type: concept
title: Crash Recovery
description: Vim-swapfile-style crash recovery — debounced full-text snapshots of dirty buffers, written atomically to the project state dir, restored on next launch.
resource: internal/backup
tags: [architecture, backup, crash-recovery, persistence]
timestamp: 2026-07-09T22:30:00Z
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
  them at launch and prompts per file, then age-based GC prunes the rest.

The write side (#167, `internal/app/backup.go`) rides the shared-document sync
seam: `editor.SyncMsg` fires on every buffer change and save, and the
originating pane's dirty flag decides — dirty `Mark`s the buffer on the
debouncer (key = file path, or a pane-scoped `untitled:` token for pathless
buffers), clean `Cancel`s the mark and removes the snapshot. One armed
`tea.Tick` wakes the model at the earliest deadline; the tick captures the due
buffers' text and writes the snapshots off the Update loop as a `tea.Cmd`,
re-arming while marks remain. Close-with-discard removes the closing pane's
snapshot (unless another pane still shows the shared document), and a clean
quit removes the snapshots of every open buffer — so a leftover at startup
always means a crash. Snapshots skipped at the restore prompt belong to no
open pane and survive the quit.

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

The `[backup]` config section (#167) tunes the subsystem; edits apply live
through the settings panel's **Backup** page (hot-reload, no restart):

| Key | Default | Meaning |
|---|---|---|
| `backup.enable` | `true` | Turns the subsystem on. Disabling it — in config or live — also **purges existing snapshots**. |
| `backup.debounce_ms` | `2000` | Quiet interval after the last edit before a dirty buffer is snapshotted (clamped to ≥ 100). |
| `backup.max_age_days` | `7` | Snapshots older than this are pruned by the startup GC (clamped to ≥ 1). |

**GC ordering:** the age-based GC (`Service.Prune`) runs only after the restore
prompt has closed — every leftover snapshot is offered first, never silently
pruned. With no leftover snapshots there is nothing to prune and no prompt.

**Privacy:** snapshots contain full file contents (they live under the project
state dir, outside the project tree). `enable = false` turns the subsystem
fully off and purges everything on disk — at startup and immediately on a live
config reload.
