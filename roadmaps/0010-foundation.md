# Roadmap 0010 — Foundation: File Explorer + Vim Editor

First vertical slice of IKE. Goal: open a directory, browse the tree, open a
file, edit it with vim-style controls, save it. Everything else (windowing,
tabs, splits, resizing) builds on this base.

## Scope

- Boot a bubbletea app with an alt-screen, full-window layout (done in base).
- Left pane: file explorer rooted at the working directory.
- Right pane: editor that loads the selected file.
- Vim-like modal editing in the editor (normal / insert at minimum).
- Focus switching between panes.

## Architecture

```
cmd/ike/main.go        entrypoint, tea.NewProgram
internal/app/          root model: layout + focus + global keys
internal/explorer/     file tree model (read dir, navigate, select)
internal/editor/       buffer + modal vim state machine + rendering
internal/keys/         shared keymap definitions
```

Each pane is its own `tea.Model`-shaped component (Init/Update/View) embedded in
the root model. The root forwards `tea.Msg` to the focused child and owns layout.

## Milestones

- [x] Base app: alt-screen, two-pane layout, focus switch, quit.
- [ ] Explorer: list working-dir entries, arrow/`j`/`k` navigation, enter to descend, `-`/`..` to go up.
- [ ] Explorer → editor: selecting a file emits an "open file" msg the root routes to the editor.
- [ ] Editor: load file into a line buffer, render with cursor, scroll on overflow.
- [ ] Editor normal mode: `h j k l`, `0 $`, `gg G`, `w b`, `x`, `dd`.
- [ ] Editor insert mode: `i a o O`, text entry, `esc` back to normal.
- [ ] Save: `:w` writes buffer to disk; `:q` / `:wq` close.
- [ ] Status line: mode indicator, file name, dirty flag, cursor position.
- [ ] Tests: explorer navigation, editor motions, modal transitions, save round-trip.

## Out of scope (later roadmaps)

Windowing, tabs, pane splitting/resizing/moving, syntax highlighting, search,
LSP, config, plugins.
