---
type: concept
title: Hex Viewer
description: "#2420 — binary files open read-only as offset|hex|ASCII over windowed reads: byte cursor with inspector row, range selection with hex/raw copy, string and byte-sequence search, the files.binary_open routing setting and the Open File As… chooser."
resource: internal/hexview
tags: [architecture, hex, binary, pane, viewer, openas]
timestamp: 2026-09-03T00:00:00Z
---

# Hex Viewer (#2420)

`internal/hexview` renders any file read-only in the classic hex layout —
offset column, hex bytes, ASCII column — in a pane of kind `KindHex`. The
model never holds the file: it keeps a `(path, size, 256 KiB read window)`
triple and serves every render and inspection through `ReadAt` on that
window, so a multi-gigabyte file opens as fast as a small one. The buffer is
deliberately window-shaped rather than a byte slice so a later write mode can
grow an overlay of edited ranges without touching the read path.

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

`/` opens the search line; the pane implements the shared `Searchable`
capability (#2409/#2410), so cmd+f opens it and cmd+g / cmd+shift+g step
matches while it is open (`n`/`N` step after enter closed it). The query is
parsed by `ParseQuery`: a `0x` prefix reads hex digits (spaces allowed),
`\xNN` escapes mix bytes into a literal, anything else searches the string's
UTF-8 bytes. Applying enumerates match offsets by streaming the file in
overlapping 1 MiB chunks (capped at 100 000 matches, the counter shows
`N+`), jumps to the first match at or after the cursor, and highlights every
visible match in both the hex and ASCII columns; only an explicit search
pays that scan, never the open.

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
