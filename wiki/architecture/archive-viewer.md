---
type: concept
title: Archive Viewer
description: "#1762 — archive files (tar, tar.gz/.tgz, tar.bz2) open as a collapsible entry list instead of a raw text buffer; Enter extracts one member into a read-only editor buffer with syntax highlighting from the member's own file name."
resource: internal/archview
tags: [architecture, archive, tar, viewer, pane, read-only]
timestamp: 2026-08-10T00:00:00Z
---

# Archive Viewer (#1762)

Opening a `.tar` (plain, gzip- or bzip2-compressed) lands in a pane of kind
`KindArchive` that lists the archive's entries as a collapsible tree, not in a
text buffer full of header blocks and padding. `Enter` on a file row extracts
that one member into memory and shows it in a **read-only** editor tab.

Three packages carry it, none of them tar-shaped in its API:

- `internal/archive` — the format layer: sniff, list, extract one entry.
  Everything through the standard library (`archive/tar`, `compress/gzip`,
  `compress/bzip2`); no dependency was added.
- `internal/archview` — the pane model: the tree, the cursor, the key handling,
  the rendering.
- `internal/app/archives.go` — the plugin, the pane lifecycle, and the
  read-only entry open.

## Routing and the sniff

The compile-in `archives` plugin claims files via a `FileHandler`. Extensions
`.tar`, `.tgz`, `.tbz`, `.tbz2` are claimed outright; `.tar.gz` and `.tar.bz2`
are **not**, because the registry matches `filepath.Ext`, which reads
`.tar.gz` as `.gz`. They arrive through the handler's `Match` instead, which
sniffs content:

- gzip or bzip2 magic → decompress **one 512-byte block** and test that for a
  tar header. A 10 GB archive costs the same as a 10 KB one, and a compressed
  stream that holds no tar is not claimed — that is the coordination point
  with the [gz viewer](./gz-viewer.md), which keeps `app.log.gz`.
- otherwise → test the leading block directly: the POSIX/GNU `ustar` magic at
  offset 257, or, for magic-less v7 tars, a header checksum that verifies (the
  same test tar itself uses, so random 512-byte prefixes are not claimed).

The handler dispatches `OpenArchiveMsg`; `openArchivePane` splits the focused
leaf like the image preview, refocusing an existing pane already bound to the
same path instead of duplicating. Keys mint as `archive`, `archive:2`, …;
persistence records `{Kind: "archive", Path}` and restore re-lists the file (a
vanished or corrupt file restores as the pane's own error notice).

## The entry list

`archview` turns the flat header list into a tree, synthesizing the
directories an archive never names explicitly (a common tar shape). One level
sorts directories first, then files, each case-insensitively by name. Each row
shows the entry name with its fold glyph plus, right-aligned, the size (or a
`→ target` for a link), the mode and the modification time; a synthesized
directory has no header of its own and shows no columns.

Keys follow the shared list semantics
([list-navigation](./list-navigation.md), `ui.ListNav` with `NavFull`): `j`/`k`
step and wrap, page keys clamp, `g`/`G` jump to the ends. On top of that:

| Key | Effect |
| --- | --- |
| `enter` / `l` / `right` | file: open read-only · directory: toggle fold |
| `space` | toggle the fold under the cursor |
| `h` / `left` | collapse an expanded directory, else jump to the parent |

The pane advertises the `archive` context id, so bindings can scope to it.

## Read-only entry preview

`Enter` on a file row emits `archview.OpenEntryMsg`; the root model extracts
the member and installs it via `editor.ShowReadOnly`. Guards applied before
anything is buffered:

- **Large-file threshold** — the entry is refused when it crosses
  `files.large_file_kb` ([#149](./editor.md)), the same ceiling a file on disk
  gets. `archive.ReadEntry` also reads one byte past the limit rather than
  trusting the (untrusted) header size, so a corrupt archive cannot make it
  buffer a whole stream.
- **Binary members** are refused with a notice: a NUL byte in the leading 8 KB
  is the test. Routing archives away from the editor would be pointless if a
  `.so` inside one still reached a text buffer.

The buffer's path is *virtual*: `<archive>!<entry>`, e.g.
`/tmp/src.tar!cmd/main.go`. Nothing on disk answers to that string, which is
exactly why the buffer is read-only — but its tail is the member's own file
name, so the tab title, the language lookup and the syntax highlighting all
resolve from `main.go` with no special casing anywhere. Re-opening the same
entry activates its existing tab (`TabForPath` on the virtual path).

## Read-only buffers

`editor.ShowReadOnly` (`internal/editor/readonly.go`) is the general seam, not
an archive feature: content shown in a full editor — motions, search,
highlighting, folds — that can never be written back. It is deliberately
distinct from the dependency-file guard (#565), which blocks the *first* edit
and unlocks on confirmation: here there is nothing to unlock into, so:

- every mutation path refuses outright and leaves `E45: buffer is read-only`
  on the ex line (`mutate`, `beginInsertChange`, `startInsertWith`, the
  insert-entry keys), and `newRecorder` returns a locked recorder as the
  catch-all backstop;
- `saveAs` — the single funnel under `:w`, `:wq`, `:w other`, `SaveTo` and the
  focus-leave autosave — fails with the same reason, so the virtual path can
  never become a file;
- `Load` and `NewFile` clear the flag: reusing the view for a real file
  unlocks it.

The chrome marks it: the pane title reads `main.go (src.tar) [RO]` and the tab
label carries the same `[RO]` suffix.

Session state follows from the same fact — a read-only preview's path names no
file, so it is skipped when persisting an editor pane's tabs and never enters
the reopen ring (#158). Like a scratch tab, it is session-local.

## Boundaries

- **Read-only, always.** There is no write-back into an archive, and none is
  planned here; extraction to disk is the shell's job.
- **Zip is out of scope**, but nothing in the pane, the pane kind or the UI
  strings says "tar": `archive.Format` classifies, everything above speaks of
  archives, so a zip reader slots in beside `FormatTar`.
- **xz / zstd** are not supported — neither has a standard-library reader, and
  the no-new-dependency rule holds.
- A listing is capped at 50 000 entries; the cap is reported in the header
  (`truncated`), never silently applied.
