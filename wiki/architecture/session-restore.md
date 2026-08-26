---
type: concept
title: Session Restore
description: Per-project workspace persistence — every pane's tabs with their caret and framing, plus explorer expansion/hidden/cursor, saved on quit and reapplied lazily on launch, beside the layout store.
resource: internal/app/session.go
tags: [architecture, persistence, session, explorer, editor, tabs, lazy, bubbletea]
timestamp: 2026-08-26T12:00:00Z
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
- **Panes** (#2177): every editor pane's per-tab view state, keyed by instance
  key — one `{path, line, col, top, left}` entry per document tab, in tab
  order. This is the whole working set, not just the focused document: the
  `editor` section above stays as the (redundant) record of the focused tab so
  older builds keep restoring something. The tab *list* itself — order, active
  tab, pins — lives in `layout.json` with the rest of the pane identity (see
  [Editor Tabs](/architecture/editor-tabs.md)); the two halves meet at restore
  and are matched **by path**, so a layout that drifted from the session simply
  finds no view and opens that tab at the top.
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
  clobber a restored offset. So the restore stashes the saved `top`/`left`
  in `Model.pendingScroll` — one entry per editor pane, since every pane's
  active tab restores its own framing (#2177); `layout()` applies each via
  `editor.SetScroll` once, right after that pane is sized, and drops the entry.
  Tabs that materialize later need none of this: they load into an
  already-sized pane and frame themselves.

## Lazy tab restore (#2177)

Reading every tab of every pane at startup does not scale — a working set of a
hundred files would be a hundred file reads, decodes and undo-history loads
before the first frame. So **only each pane's active tab is read during
restore**; every other tab comes back as a *deferred tab*.

- `pane.Tab` carries a `*pane.Deferred` (path + caret + framing) and a loader
  closure instead of a loaded document; its editor model is empty. The pane
  API around it is path-aware without being load-aware: `TabPath(i)` answers
  with the file a tab stands for whether or not it is read, and `TabForPath`
  matches deferred tabs, so opening a file activates its existing tab instead
  of duplicating it. The **tab bar, the layout save, the dirty sweeps and the
  explorer's open-file marks all go through `TabPath`** — anything that used
  `TabEditor(i).Path()` there would defeat the laziness on the first frame.
- **`Instance.activate` is the single materialization point.** Every path that
  puts a tab on screen — click, keymap, tab close, drag, `EditorForPath` —
  funnels through it, so a deferred tab can never render empty. Materializing
  is one-shot: a file deleted between restore and activation leaves an empty
  scratch tab rather than retrying forever.
- The loader (`Model.deferredLoader`) does what an explicit open does:
  it adopts an already-loaded editor of the same path as a shared document
  (#142) instead of reading a divergent copy, otherwise reads the file and
  re-applies the saved caret and framing. A file it actually read is recorded
  on the **registry** (`NoteLoaded`), not on the model — the tabs park and
  resume with their registry while the model around them is rebuilt on every
  project switch. The update tail (`drainLazyLoads`) consumes the record and
  gives each new buffer the wiring `openPathAt` gives: reparse, VCS and
  coverage marks, watcher tracking, `EventFileOpened`. An open that happened
  to land on a deferred tab drops its path from the record first, so the
  wiring never fires twice.
- Tabs that are still deferred at quit persist unchanged — their saved view
  carries over verbatim (`snapshotTabViews`), so a restart never resets a tab
  merely because it was not visited.
- **Missing files** are stat-checked once during restore, skipped, and
  summarized in a single warning notification (`N files are gone and did not
  reopen`) — one deleted directory would otherwise raise dozens of toasts.
- **Scratch files** (`internal/scratch`) are ordinary files on disk and
  restore like any other tab. Read-only views whose content is *not* reachable
  by path — an archive member's preview (#1762) — are dropped, like the
  unsaved text of a pathless scratch tab, which is the
  [crash recovery](/architecture/crash-recovery.md) side's job.

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

## Undo history

Undo/redo survives the restart alongside the session (#148): quit (and every
save and editor close) writes each clean document's stacks to `.ike/undo/`
via `internal/undostore`, and the reopen's ordinary `editor.Load` adopts them
when the file content still hashes to the value recorded at persist time.
A file changed between sessions restores with an empty history — see the
history section of [editor](/architecture/editor.md) for the format, caps,
and the `files.persistent_undo` flag.

## Out of scope

Selection/visual state, and cross-project session history (Roadmap 0090's
`restore_last`). A deferred tab's undo history loads with its file, so it
follows the lazy read rather than the restore.
