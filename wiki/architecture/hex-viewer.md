---
type: concept
title: Hex Viewer
description: "#2420 — binary files open as offset|hex|ASCII over windowed reads: byte cursor with inspector row, range selection with hex/raw copy, string and byte-sequence search, overwrite editing in the hex and text columns with in-place save (#2876), the files.binary_open routing setting and the Open File As… chooser."
resource: internal/hexview
tags: [architecture, hex, binary, pane, viewer, openas, editing]
timestamp: 2026-10-02T00:00:00Z
---

# Hex Viewer (#2420)

`internal/hexview` renders any file in the classic hex layout — offset
column, hex bytes, ASCII column — in a pane of kind `KindHex`. The model
never holds the file: it keeps a `(path, size, 256 KiB read window)` triple
and serves every render and inspection through `ReadAt` on that window, so a
multi-gigabyte file opens as fast as a small one. The buffer is deliberately
window-shaped rather than a byte slice: the overwrite edit mode (#2876, see
[Editing](#editing)) is an overlay of edited bytes on top of it, and the
read path only gained one patch step.

## Routing

The compile-in `hex` plugin (`internal/app/hexfiles.go`) claims files by
content only — no extensions: its `Match` fires when the `readHead` buffer
(8 KiB since #2420) contains a NUL byte and the image sniff does not
recognise the head. Handler order is alphabetical by id, so the archive,
data and gzip sniffs run *before* `hex.view` and keep their files; the image
sniff runs after, which is why `Match` excludes it explicitly. The handler
dispatches `OpenHexMsg`; `openHexPane` opens as a content tab in the pane the
open asked for (#1825/#1851) and otherwise splits the leaf
`viewerSplitTarget` picks, refocusing an existing pane bound to the same
path. Keys mint as `hex`, `hex:2`, …; persistence records
`{Kind: "hex", Path}` and restore re-opens the file for windowed reads (a
vanished file restores as the pane's own error notice).

`files.binary_open` (Settings UI → Files & Session, enum `hex`/`editor`,
default `hex`) redirects the sniffed-binary open: `editor` restores the
pre-#2420 behaviour — a text buffer with code insight forced off
(`editor.MarkInsightOff`: no highlighting, no LSP). The explicit chooser pick
(`OpenHexMsg.Forced`) ignores the redirect.

## Layout & navigation

The row width adapts to the pane: the widest classic row of 8, 16 or 32
bytes whose rendered line fits (`rowBytes`), with a gap after every 8-byte
group and an offset column of at least 8 hex digits (wider past 4 GiB). The
last two lines are the inspector row and the footer (status + key hints, or
the open search line / copy menu).

Navigation is the usual list set: `j/k` rows, `h/l` bytes, `pgup/pgdn` and
`ctrl+d/ctrl+u` (half) pages, `g/G` ends, mouse wheel. The byte cursor's
offset shows dec and hex in the footer.

## Inspector row

`Inspect` decodes the bytes at the cursor: u8/i8, u16/u32/u64 each as
little/big endian pairs, f32/f64 (little-endian IEEE) and the UTF-8 rune
starting there; readings the file tail cannot fill render as `—`.

## Selection & copy

`v` anchors a range to the cursor (`esc` drops it). `y` (or cmd+c) opens a
two-row copy menu — **hex string** (`41 42 43`) or **raw bytes** — emitting
`hexview.CopyMsg`, which the root model routes through `copyToClipboard`
(system clipboard + clipboard history) with a "copied …" toast. A selection
copy is capped at 1 MiB raw and says so.

## Search

`/` opens the shared in-pane search line (`ui.LineSearch`, #2461 — see
[Project Search § In-pane search](./search.md)); the pane implements the
shared `Searchable` capability (#2409/#2410), so cmd+f opens it and cmd+g /
cmd+shift+g step matches while it is open and after `enter` closed it
(`n`/`N` step at rest). The query is parsed by `ParseQuery`: a `0x` prefix
reads hex digits (spaces allowed), `\xNN` escapes mix bytes into a literal,
anything else searches the string's UTF-8 bytes — exact bytes, the one
adopter without the smartcase rule. Matches are enumerated by streaming the
file in overlapping 1 MiB chunks (capped at 100 000 matches, the counter
shows `N+`) and highlighted in both the hex and ASCII columns. The line
narrows **live** up to `liveScanMax` (16 MiB); past it a scan per keystroke
would stall typing, so the line reads `enter to search` and the enumeration
waits for `enter` or a match step — only an explicit search pays that scan,
never the open. `enter` lands on the first match at or after the cursor; a
query that does not parse shows its complaint in the line and keeps it open.
`esc` while the line is open drops the search; at rest it drops the applied
set (after a selection, which `esc` clears first).

## Editing

Overwrite only (#2876): editing replaces bytes in place and never inserts or
deletes, so the file size never changes.

**Two columns, one active.** The cursor lives in the hex or the text column
(`column`: `colHex`, `colText`); `tab` switches, and the footer leads with
`HEX`, `TEXT` or `TEXT INSERT`. The one rule for which keys type: the hex
column types hex digits directly — no `0-9a-fA-F` key is a viewer key, so
no toggle is needed and every other key (`h/j/k/l`, `g/G`, `v`, `y`, `/`,
`n/N`, `u`) keeps its meaning while non-hex printable keys are ignored — and
the text column, where every letter is also a navigation key, types only
between `i` and `esc` (`i` from the hex column switches to text insertion
too). While inserting, printable keys write and only `esc`, arrows, page
keys and `tab` (which also ends the insertion) act otherwise; the app
counts the state as text-capturing (`Model.Capturing`, read by
`editorCapturing`), so plain keys bypass the keymap layer, `?` help, the
esc-esc palette and `q`, exactly as for an editor in insert mode. A paste in
text insertion overwrites like typing.

- **Hex column:** the first nibble replaces the cursor byte's high nibble
  at once (the half-edit shows: `12` → `a2`) and the cursor stays; the
  second completes the byte and advances. Moving away — any non-digit key,
  the wheel, a search step — commits the half-edit as is.
- **Text column:** a key writes its UTF-8 bytes consecutively and advances
  past them. A character that would run past the end of the file is refused
  with a footer notice.

**Overlay.** `overlay map[int64]byte` holds every edited byte that differs
from the disk; `setByte` drops an entry written back to its on-disk value,
so the overlay — and `Dirty()` — only ever holds real differences.
`readAt` returns the raw window slice when the overlay is empty and a
patched copy otherwise (`readRaw` is the disk view); `findAll` patches each
streamed chunk. Rendering, the inspector row, copy and search therefore all
see the edits.

**Undo.** `u` undoes the last write — one completed (or committed half)
hex byte, one text keystroke — and `ctrl+r` redoes; a plain stack of
`byteEdit{off, old, new}` runs, a new write clearing the redo stack. The editor's chords work too: `hex.undo`
(`cmd+z` / `ctrl+z`) and `hex.redo` (`cmd+shift+z` / `ctrl+shift+z`) in the hex
context (#2888).

**Highlighting.** `classify(off, col)` ranks the cursor first: the active
column's cursor (`classCursor`) wears the editor caret's mode colours —
`Accent` while navigating, `Success` (insert green) while typing text —
and the mirror byte in the other column (`classMirror`) the `Selection` /
`SelectionText` pair; then selection, search match, and an unsaved edit
(`classModified`, `Warning` foreground, also over a selection or match
background).

**Saving.** `hex.save` — `cmd+s` / `ctrl+s` in the hex context, the
editor's save chords; `editor.write` targets a buffer the pane lacks — runs
`Model.Save`: the file's size is re-checked (a file resized on disk is
refused, its offsets no longer mean the same), each contiguous overlay run is
written with `WriteAt` on the path opened `O_WRONLY`, then the overlay
clears and the window re-reads. The undo stack survives: an undo after a
save dirties the pane against the new disk state. The app stamps
`watcher.MarkSaved` before the write (no reload, no external-change
warning) and refreshes the VCS status.

**Dirty state in the app.** A dirty viewer shows `●` in its pane title
(`HEX blob.bin ●`) or tab label, like a dirty editor tab. Every guard that
protects unsaved buffers covers it: the close guard (`dirtyOnClose`, for a
hex pane and a hex content tab — `s` saves via `saveHex`, `d` discards), the
quit / workspace / project-close guards (`collectActivity` lists it,
`saveWorkspaceDirty` writes it) and Save All (`writeDirtyTabs`).

## Open File As… chooser

`file.openAs` (`cmd+alt+shift+o` — the issue's proposed `cmd+alt+o` folds to
`ctrl+alt+o` off macOS, which `lsp.organizeImports` owns; also in the
palette, the explorer context menu and the editor tab context menu) opens a
locked palette mode (`internal/app/openas.go`) over the current subject —
the explorer's selection or the focused editor's file. Its rows are every
registered viewer plus the two paths every file supports: **Text editor**
(forces the editor with the binary guard bypassed for that buffer — insight
off, no LSP), **Hex**, **Image**, **Archive**, **Data**
(SQLite/DuckDB/Parquet), **Markdown preview**, **Gzip**. Targets with a
content contract validate the file head first — an invalid pick (`not a
SQLite, DuckDB or Parquet file`) is a notification and the current tab stays
untouched.
