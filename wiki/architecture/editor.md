---
type: concept
title: Editor
description: Modal vim-like text editor pane with a line buffer and normal/insert/command modes.
resource: internal/editor/editor.go
tags: [architecture, editor, vim]
timestamp: 2026-06-19T00:00:00Z
---

# Editor

`editor.Model` holds the file as a line buffer (`[]string`, never empty) and a
cursor (`row`, `col`). `Update` dispatches each key to the handler for the
current `Mode`.

## Modes

- **Normal** — motions `h j k l`, `0` `$`, `gg` `G`, `w` `b`; edits `x`
  (delete rune), `dd` (delete line). Two-key sequences (`gg`, `dd`) use a
  `pending` rune. Enters insert via `i a o O`, command via `:`.
- **Insert** — text entry, `enter` splits the line, `backspace` deletes/joins,
  `esc` returns to normal (cursor steps back one column, vim-style).
- **Command** — accumulates the `:` line; `enter` runs it.

## Commands

- `:w` — write the buffer to disk (joined with `\n`, trailing newline) and clear
  the dirty flag.
- `:q` — emit `CloseMsg` to detach the editor.
- `:wq` / `:x` — save then emit `CloseMsg`.

## Loading and rendering

`Load(path)` normalises `\r\n`, splits into lines, drops a single trailing empty
line, and resets cursor/scroll/dirty state. `View` renders the visible window
(`top`..`top+height`) and reverse-highlights the cursor cell on the focused
line. The dirty flag and 1-based cursor position feed the app status line.
