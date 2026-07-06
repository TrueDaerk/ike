---
type: concept
title: File Explorer
description: Expandable file-tree pane rooted at a fixed project base that emits an open-file message.
resource: internal/explorer/explorer.go
tags: [architecture, explorer, tree]
timestamp: 2026-07-06T00:00:00Z
---

# File Explorer

`explorer.Model` shows the project as an expandable tree of `node`s rooted at a
**fixed base** (`explorer.New(".")` — the working directory). The root is never
replaced and the explorer never ascends above it. The root node is expanded on
startup; directory children are read lazily the first time a node is expanded,
sorted directories-first then alphabetically.

Scans never block the update loop: expanding a directory dispatches a `scanCmd`
`tea.Cmd` that reads the directory off-thread and returns a `ScanDoneMsg`;
`applyScan` then installs the children and rebuilds. `Init` kicks off the root
scan, so the tree is empty for one frame and fills in on the first message.

The visible tree is flattened into `rows` (rebuilt on every expand/collapse) for
cursor navigation; each node carries its `depth` for indentation.

## Configuration

`Configure(host.Config)` applies the merged `[explorer]` section (owned by the
`internal/config` schema, Roadmap 0040) before the first render:

| key | meaning |
| --- | --- |
| `explorer.show_hidden` | initial visibility of dot-entries (toggleable at runtime) |
| `explorer.tree_indent` | spaces per depth level (indent-guide width) |
| `explorer.sort` | within-level ordering (`name`); directories are always first |
| `explorer.colors.<ext\|glob>` | per-filetype colour; `dir` and `default` are required fallbacks |

Colours (`colors.go`) resolve a node by checking, in order: an exact **glob**
match (globs sorted for determinism), the `dir` fallback for directories, a bare
**extension** match, then `default`. Values are colour names (`blue`, `cyan`,
`gray`, …), hex (`#1f6feb`), or raw ANSI indices. When no `[explorer.colors]` is
configured, a built-in default table is used so the tree is never monochrome.

## Navigation

- `j` / `k` / arrows — move the cursor over visible rows.
- `enter` — toggle a directory (expand/collapse) in place, or open a file
  (emits `OpenFileMsg{Path}`).
- `l` / `right` — expand a collapsed directory, step into the first child of an
  expanded one, or open a file.
- `h` / `left` — collapse an expanded directory, otherwise jump to the parent
  node. Never moves above the root.

Directories render with a `▾`/`▸` marker; a read error is retained and shown in
place of the tree.

## Mouse

The root model forwards mouse events that land in the explorer pane, translating
the absolute cell into the tree's content-local space (inside the pane border,
padding, and title row) before calling the explorer:

- **Left press** on a row (`MouseClick`) selects it and activates it — toggling a
  directory or opening a file — mirroring `enter`.
- **Motion** over a row (`SetHoverAt` / `ClearHover`) sets a transient hover
  highlight; leaving the pane clears it.
- **Wheel** over the pane scrolls without moving the cursor, like a real
  scrollbar: vertical by default (`ScrollBy`), horizontal with **shift** held or
  the wheel's own left/right buttons (`ScrollXBy`), `wheelLines` per notch.
- **Left press** on a scrollbar track jumps that axis proportionally.

## Row highlighting

A row's **base** style is its per-filetype colour (`nodeStyle` → `colors.style`),
plus italics for hidden (dot-prefixed) entries. `rowKind` then classifies how the
row is highlighted, strongest first: the focused **cursor** (`selStyle`, blue
background) → the mouse **hover** (base colour + grey background) → the **open
file** (`activeStyle`, accent — the editor's current file, set via `SetActive` on
open, cleared on editor close) → otherwise the base style (directory or plain
file colour). The classification lives in `rowKind` so it is testable independent
of the terminal colour profile.

## Commands

Every user action is a registry `Command` (scoped to the explorer context) with a
default `Keymap`; each only dispatches an explorer `Msg` that the root model
routes back into `Update`. The canonical binding set is owned by Roadmap 0080 —
these are defaults.

| command | default key | effect |
| --- | --- | --- |
| `explorer.toggleHidden` | `.` | show/hide dot-entries (`ToggleHiddenMsg`) |
| `explorer.refresh` | `r` | invalidate + re-scan the selected subtree (`RefreshMsg`) |
| `explorer.collapseAll` | `c` | fold the tree back to the root (`CollapseAllMsg`) |
| `explorer.reveal` | — | move the cursor to the open file (`RevealMsg`) |
| `explorer.newFile` | `a` | prompt for a name, create an empty file (`NewFileMsg`) |
| `explorer.newFolder` | `A` | prompt for a name, create a directory (`NewDirMsg`) |
| `explorer.delete` | `d` | delete the selected entry after confirmation (`DeleteMsg`) |
| `explorer.rename` | `R` | prompt (prefilled with the current name) to rename the selected entry (`RenameMsg`) |
| `explorer.undo` | `Ctrl+Z` | reverse the last file operation after confirmation (`UndoMsg`) |

Hidden files are filtered from `rows` unless `show_hidden` is on; toggling just
rebuilds (no re-scan), since all children — hidden included — are cached on the
node.

## File operations

`fileops.go` adds create / rename / delete / undo on top of navigation. Each step that
mutates the filesystem is gated behind a **modal prompt** (`Model.prompt`):
`promptInput` reads a filename (Enter accepts, Esc cancels), `promptConfirm`
reads a yes/no answer (`y`/Enter accept, anything else cancels). While a prompt
is open `Prompting()` is true, and the root model routes every key straight to
the explorer (ahead of the keymap and global layers) so typed names and answers
are not stolen by other bindings.

A `promptInput`'s text carries a rune-index cursor (`prompt.pos`), not just
append/backspace at the end: `Left`/`Right` step it, `Home`/`End` jump it,
`Delete` removes forward, and typed text/`Backspace` act at `pos` rather than
always at the string's end (rename starts with `pos` at the end of the
prefilled name). The cursor cell itself is reverse-video (`promptCursorStyle`)
over the rune already there (a blank cell past the last rune), not an inserted
caret glyph — so it never shifts the surrounding text as it moves.

`View` overlays the box via `overlay.Center(out, m.promptBox(), m.width,
m.height)` — the explorer's **own** `m.width`/`m.height` (its pane content
area), not the full terminal, since `out` here is the explorer's own rendered
tree. So the box is centered within the pane, not the screen. Mouse clicks
must land in the same content-local space `MouseClick` uses:
`promptBoxOrigin()` recomputes that centering math with the model's own
dimensions, and `PromptMouseClick(x, y)` maps a content-local click on the
input row to a `pos`. The app computes those content-local coordinates itself
(pane rect + `paneContentX`/`paneContentY`, same as a normal pane click) and
routes mouse presses there instead of through the normal pane hit-test
whenever `explorerCapturing()` is true (explorer focused with a prompt open).

New entries are created next to the selection — inside the selected directory, or
beside the selected file. Deletes do not `os.Remove`; they move the entry into a
hidden, same-filesystem trash directory (`.ike-trash/` under the project root, so
the rename never crosses devices), which is what makes an undo able to restore
it. Completed operations are pushed onto a linear undo stack (`ops`):

- **Undo of a create** removes the entry it added.
- **Undo of a delete** moves the trashed entry back to its original path.

Rename (`promptRename` / `renameEntry`) is a plain `os.Rename` within the same
directory, prompted with the current name pre-filled; it is not on the undo
stack (rename it back to undo). The root is never renameable, mirroring delete.

Removing a path (a delete, a rename, or undo of a create) emits `FileDeletedMsg`,
which the root model handles by closing any editor still open on that file (or,
for a directory, any file beneath it) — so a deleted or renamed-away path never
lingers in an open pane (the editor has no way to follow a rename in place).
Unlike the other explorer messages, `FileDeletedMsg` is handled by the app, not
routed back into the explorer, so it deliberately does not implement `Msg`.

`Ctrl+Z` in the explorer context resolves to `explorer.undo`, mirroring the
editor's `Ctrl+Z` text undo but operating on files. After any operation the
affected directory is re-scanned (`refreshDir`) and `pendingSel` snaps the cursor
onto the new or restored entry once it reappears. This file-op undo stack is
entirely separate from the editor's text history.

## Scrolling & scrollbars

The explorer keeps a vertical (`offset`) and horizontal (`offsetX`) scroll
offset. `viewport()` resolves the inner text area, reserving a right column for
a vertical scrollbar and a bottom row for a horizontal one whenever the content
overflows the pane (two passes settle the mutual dependence). Rows are clipped
to the horizontal window with `ansi.Cut`, so long names and deep nesting scroll
sideways instead of wrapping.

Each bar is a dim track (`│` / `─`) with a brighter, heavier thumb (`┃` / `━`)
sized and positioned by `scrollThumb`, in the style of table TUIs. Bars are
hidden when the content fits.
