---
type: concept
title: File Explorer
description: Expandable file-tree pane rooted at a fixed project base that emits an open-file message.
resource: internal/explorer/explorer.go
tags: [architecture, explorer, tree]
timestamp: 2026-06-20T00:00:00Z
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

Hidden files are filtered from `rows` unless `show_hidden` is on; toggling just
rebuilds (no re-scan), since all children — hidden included — are cached on the
node.

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
