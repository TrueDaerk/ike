---
type: concept
title: Pinned File Slots
description: Harpoon-style numbered file slots — pin the working set once, jump by number; per-project persistence and a modal picker for reorder/unpin.
resource: internal/app/pins.go
tags: [architecture, navigation, pins, harpoon, bookmarks, persistence]
timestamp: 2026-07-22T00:00:00Z
---

# Pinned File Slots

Harpoon-style pinned slots (#788, popularized by ThePrimeagen's harpoon for
Neovim): four numbered slots hold the current working set. Unlike Recent
Files — an MRU *history* whose order shifts with every open — slots are
stable: pin once, jump by number, muscle memory does the rest.

## Commands & keys

| Command | Default | What |
|---|---|---|
| `nav.pinGoto1..4` | `ctrl+shift+1..4` | jump to slot N |
| `nav.pins` | `cmd+2` | open the picker (JetBrains' Bookmarks chord) |
| `nav.pinSlot1..4` | — (palette / picker `p`) | pin the active file to slot N |

`ctrl+digit` is unavailable as the jump family: the `cmd+digit` tool-window
chords fold onto `ctrl+digit` on Linux (`NormalizeKey`), so the jumps sit on
`ctrl+shift+digit` — Kitty-protocol delivery like the other ctrl+shift
chords, with the palette as the documented fragile-escape
(`reachableAlternatives`). Digits are layout-identical on QWERTZ.

## Store

`internal/app/pins.go`: `pinStore` holds four absolute canonical paths and
persists to the per-project `.ike/pins.json` (IKE_CONFIG_DIR-redirectable,
like layout and winsize). Every mutation saves; read failures degrade to
empty slots. A path lives in at most one slot — re-pinning elsewhere moves
it. Project switches rebuild the model and re-load the target project's
store.

## Picker

`nav.pins` opens the modal shell (`ui.Floating`) with a four-row list; the
app owns the keys ahead of generic shell handling, like the save-conflict
prompt: `j/k` select, `enter` opens the selected slot, `1-4` jump directly,
`p` pins the active file to the selected slot, `x`/`d` unpins,
`shift+j/k` reorders (selection follows the moved slot), `esc`/`q` closes.

## Jump semantics

Jumps go through `openPath`, so a file open in any pane focuses that pane
(#930) and nav history records the departure (Roadmap 0220). An empty slot
is a friendly toast. A slot whose file vanished keeps the pin but raises a
warning and opens the picker with that slot selected — unpinning is the one
keystroke `x` away.
