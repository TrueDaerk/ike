---
type: concept
title: Foundation Slice
description: Root model that hosts the explorer and editor panes, owns layout/focus, and routes messages between them.
resource: internal/app/app.go
tags: [architecture, bubbletea, foundation]
timestamp: 2026-06-20T00:00:00Z
---

# Foundation Slice

The first vertical slice of IKE (roadmap 0010). Goal: open a directory, browse
the tree, open a file, edit it with vim controls, save it.

## Structure

```
cmd/ike/main.go     entrypoint, tea.NewProgram(app.New(), WithAltScreen)
internal/app/       root model: layout + focus + global keys + routing
internal/explorer/  file tree pane
internal/editor/    line buffer + modal vim state machine
```

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
