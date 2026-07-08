---
type: concept
title: Foundation Slice
description: Root model that hosts the explorer and editor panes, owns layout/focus, and routes messages between them.
resource: internal/app/app.go
tags: [architecture, bubbletea, foundation]
timestamp: 2026-07-07T00:00:00Z
---

# Foundation Slice

The first vertical slice of IKE (roadmap 0010). Goal: open a directory, browse
the tree, open a file, edit it with vim controls, save it.

## Structure

```
cmd/ike/main.go     entrypoint, tea.NewProgram(app.New())
internal/app/       root model: layout + focus + global keys + routing
internal/explorer/  file tree pane
internal/editor/    line buffer + modal vim state machine
```

IKE runs on **Bubble Tea v2** (`charm.land/bubbletea/v2`; see
former Roadmap 0085 — planning moved to GitHub issues, spec in git history). Under v2, alt-screen, mouse mode
and the kitty keyboard enhancements are declared on the root model's `View()` (which
returns a `tea.View` struct), not passed as `tea.NewProgram` options. The root dispatches
`tea.KeyPressMsg` only, ignoring `tea.KeyReleaseMsg`, and normalises the four v2 mouse
messages (`MouseClickMsg`/`MouseReleaseMsg`/`MouseWheelMsg`/`MouseMotionMsg`) into one
internal `mouseEvent` for the drag handler.

Each pane is a `tea.Model`-shaped component (Init/Update/View) embedded in the
root `app.Model`. The root forwards `tea.Msg` to the focused child and owns
layout. Layout geometry itself is no longer hard-coded: the root drives a pure
split tree (see [Pane Layout & Drag](/architecture/pane-layout.md)) that computes
each pane's rectangle and supports mouse divider-resize and title-bar move.

## Message routing

- `explorer.OpenFileMsg{Path}` — emitted when the user opens a file; the root
  calls `editor.Load(path)` and moves focus to the editor.
- `editor.CloseMsg{}` — emitted by `:q` / `:wq`; the root replaces the editor
  with a fresh empty one and returns focus to the explorer.

## External file changes (Roadmap 0140)

`internal/watch` is the file-watcher service (#80): fsnotify on the project
root, recursive (skipping `.git`, watching newly created directories),
debounced ~100ms with per-path coalescing (removal wins; create survives a
follow-up write). It emits `watch.EventMsg{Kind, Path}` — `FileChanged` /
`FileCreated` / `FileRemoved` / `DirChanged` — through `host.Send`; the root
model routes file kinds to the editor leaf owning the path
(`editorKeyForPath`) and `DirChanged` to the explorer (consumers land in
#81–#83).

- **Self-event suppression:** the editor emits `EventSave` after every disk
  write; the app's emitter adapter stamps `watcher.MarkSaved(path)`, and
  events for that path within 500ms are dropped — IKE's own saves never
  round-trip as external changes.
- **Poll fallback:** for filesystems where fsnotify under-reports (network
  mounts), open buffers are `Track`ed and `Poll()` compares mtime+size,
  hashing on suspicion (an mtime-only touch never reports), behind the same
  message shape.
- **Config:** `files.watch = true|false` (default true). `main.go` starts the
  watcher after wiring `Send`; a project switch (Roadmap 0090) calls
  `StartWatcher` again, which restarts on the new root.

## Slow-update diagnostics (#125)

Anything that stalls the root model's `Update` freezes the whole UI (the #123
deadlock was invisible until it hung). Every Update pass over 200ms appends a
timestamped line — message type + duration — to the per-project state log
(`.ike/debug.log`, `IKE_CONFIG_DIR`-aware like the layout store). Logging is
best-effort; a failed write never affects the editor.

## Focus and global keys

`Tab` toggles focus between panes. `Ctrl+C` always quits; `q` quits when the
explorer is focused or when the editor is focused in normal mode
(`app.quitKey`). While the editor is capturing text (insert or command mode),
global single-letter keys are suppressed so typed characters reach the buffer
(`app.editorCapturing`).

## Status line

A one-row bar renders the editor mode, file name, dirty marker, and 1-based
cursor position; when the editor is in command mode it shows the typed
`:command` line instead.
