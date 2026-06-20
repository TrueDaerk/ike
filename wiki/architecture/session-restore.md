---
type: concept
title: Session Restore
description: Per-project workspace persistence — open file + cursor and explorer expansion/hidden/cursor saved on quit and reapplied on launch, beside the layout store.
resource: internal/app/session.go
tags: [architecture, persistence, session, explorer, editor, bubbletea]
timestamp: 2026-06-20T12:00:00Z
---

# Session Restore

The IDE reopens where it was left: on a clean quit the workspace state is written
to a per-project file, and the next launch reapplies it. This complements the
[pane layout store](/architecture/pane-layout.md) — pane geometry and split
structure persist there in `layout.json`; everything *inside* the panes (the open
file, cursors, explorer tree state) persists here in `session.json`.

## What is saved

- **Editor:** the open file path, the 0-based cursor line/column, and the
  viewport framing (scroll `top`/`left`). Saved only when a file is open. The
  scroll offset is saved **in addition to** the cursor because `Top` is sticky
  during normal editing — it is not a function of the cursor (scroll down, then
  move the cursor back up, and `Top` stays put). Restoring the cursor alone would
  reframe the file, so on-screen rows would map to different lines and mouse
  clicks would miss.
- **Explorer:** the set of expanded directory paths (the always-open root is
  excluded), the show-hidden toggle, and the path under the cursor.

Both sections are optional in the schema, so an older or partial file still loads
with the missing parts falling back to defaults.

## Storage

`internal/app/session.go` mirrors the layout store's discovery seam:
`IKE_CONFIG_DIR` overrides the location (tests redirect writes there); otherwise
the file lives under the project's `.ike/` directory as `session.json` (pretty
JSON). Like the layout store, **all write errors are swallowed** — failing to
persist must never disrupt shutdown — and a missing/unreadable/malformed file
yields a clean default workspace.

## Save and restore flow

- **Save** is routed through `Model.quit()`, which every quit path uses (`ctrl+c`,
  and `q` when `quitKey()` allows). It snapshots the editor and explorer, writes
  `session.json`, then returns `tea.Quit`. The editor's `:q` `CloseMsg` only
  detaches the buffer; it does not quit, so it does not persist.
- **Restore** runs in `NewWith` right after the layout restore, via
  `restoreSession()`: it applies the explorer state, loads the editor file (and
  clamps the saved cursor with `editor.SetCursor`), marks the explorer's active
  row, focuses the editor when a file was reopened, then `syncFocus()`.
- **Viewport framing is deferred.** During `NewWith` the editor has no size yet,
  and the first layout's `SetSize` re-derives `Top` from the cursor — which would
  clobber a restored offset. So `restoreSession` stashes the saved `top`/`left`
  in `Model.pendingScroll`; `layout()` applies it via `editor.SetScroll` once,
  right after the editor is sized, then clears the field.

## Explorer restore is synchronous

The explorer normally loads directory children **asynchronously** (`scanCmd` →
`ScanDoneMsg`). Restore cannot use that path: an async root re-scan returning
after restore would replace the root's children with fresh, unexpanded nodes and
discard the restored expansion. So `explorer.Restore` reads directories
**synchronously** (`loadSync`, shared child-building via `setChildren`),
shallowest path first so a parent's children exist before a child is expanded.
Because Restore marks the root `loaded`, `explorer.Init` skips its startup scan
when a session was restored — the one place the two paths must agree.

Expanded paths that no longer exist on disk are skipped, not fatal; the cursor is
restored only when its saved path is visible after re-expansion.

## Out of scope

Multiple editors / tabs (Roadmap 0037 will extend the layout store with per-leaf
file identity), undo history, selection/visual state, and cross-project session
history (Roadmap 0090's `restore_last`).
