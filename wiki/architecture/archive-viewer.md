---
type: concept
title: Archive Viewer
description: "#1762 — archive files (tar, tar.gz/.tgz, tar.bz2) open as a collapsible entry list instead of a raw text buffer; Enter (or a double-click) extracts one member into a read-only editor buffer with syntax highlighting from the member's own file name; gzip members open decompressed (#1948); e/E write members or the whole archive to a directory on disk under path, overwrite and size guards (#2249); ctrl+r re-lists the file in place (archive.reload, #2314)."
resource: internal/archview
tags: [architecture, archive, tar, viewer, pane, read-only, mouse, extract, reload]
timestamp: 2026-09-02T00:00:00Z
---

# Archive Viewer (#1762)

Opening a `.tar` (plain, gzip- or bzip2-compressed) lands in a pane of kind
`KindArchive` that lists the archive's entries as a collapsible tree, not in a
text buffer full of header blocks and padding. `Enter` on a file row extracts
that one member into memory and shows it in a **read-only** editor tab.

Three packages carry it, none of them tar-shaped in its API:

- `internal/archive` — the format layer: sniff, list, read one entry into
  memory, and write members out to disk (`PlanExtract`/`Extract`, #2249).
  Everything through the standard library (`archive/tar`, `compress/gzip`,
  `compress/bzip2`); no dependency was added.
- `internal/archview` — the pane model: the tree, the cursor, the key handling,
  the rendering.
- `internal/app/archives.go` — the plugin, the pane lifecycle, and the
  read-only entry open.
- `internal/app/archextract.go` — the extraction UI: target-directory prompt,
  overwrite guard, summary toast (#2249).

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

The handler dispatches `OpenArchiveMsg`; `openArchivePane` opens as a content
tab in the pane the open asked for — the focused pane for a palette pick
(#1825), the last-focused editor for the explorer's default open (#1851, see
[data viewer](./data-viewer.md)) — and otherwise splits the leaf
`viewerSplitTarget` picks (see [pane layout](./pane-layout.md)) like the image
preview, refocusing an existing pane already bound to the same path instead of
duplicating. Keys mint as `archive`, `archive:2`, …; persistence records `{Kind: "archive", Path}` and restore re-lists the file (a
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
| `e` | extract the row under the cursor (a directory row: its whole subtree) |
| `E` | extract the whole archive |
| `/`, `cmd+f` / `ctrl+f` | put the cursor in the filter row (#2409) |
| `ctrl+r` | reload the listing from disk (`archive.reload`, #2314) |

The pane advertises the `archive` context id, so bindings can scope to it —
`ctrl+r` is the one default that does (JetBrains' Rerun chord, #2314). It
resolves through the keymap layer to `archive.reload`, which asks the focused
pane for `Reload()`: the archive is listed again, collapsed directories keep
their state by path and the cursor is clamped into the new row list, so a
re-packed file shows its new members without the pane being closed. A listing
error replaces the entries the way opening a broken archive does, and the
model reports the entry count (or the error) as a notification — a reload that
found nothing new must not look like a dead key.

### Filter row (#2409)

The pane wears the shared filter row ([list-filters](./list-filters.md),
`internal/filterbar` over `internal/filterexpr`), permanent like in every other
list pane so a filter appearing never shifts the entries by a line. `/` and the
shared find chord (`cmd+f`, `ctrl+f`) focus it; `enter` applies and leaves,
`esc` clears and leaves.

| Field | Takes |
| --- | --- |
| `name:` (alias `path:`) | a path substring or glob, matched against the entry path |
| `type:` (alias `kind:`) | `file` or `dir` |

Free match text is the same fuzzy gate the other panes use, run over the entry
path. The gate runs over the **tree** (`Model.keeps`, `internal/archview/filter.go`)
rather than the flat header list, so the directories a tar never named
explicitly filter like the ones it did, and a directory survives exactly when
it — or one of its members — matches. The tree itself never changes: a filter
change only re-derives the rows, so folds and the cursor survive it.

`OpenSearch` is the pane's `pane.Searchable` implementation, which is how the
Global `search.open` command reaches the row (see
[Keybindings](./keybindings.md)).

### Mouse (#1852)

The pane takes mouse input the way the explorer tree does; the root model
translates the absolute cell into pane-content-local coordinates and calls
`Wheel` / `Click` (`internal/archview/mouse.go`) — the archive model itself
never sees a `tea.MouseMsg`, so `Update` stays key-only.

| Gesture | Effect |
| --- | --- |
| wheel up/down | scroll the entry list; clamps at both ends |
| left click on the filter row | put the cursor in the filter (#2409) |
| left click on a row | select it |
| left click on a directory's fold glyph | toggle the fold |
| double click | activate: file → open read-only, directory → toggle the fold |

Row hit-testing reads the same `top` offset the renderer scrolls by, so a
click lands on the row the user sees: content-local `y` 0 is the header line,
`y` 1 the filter row (#2409) and the rows start at `y` 2. The fold glyph occupies two cells at
`1 + 2·depth`, matching `renderRow`'s indentation. Wheel scrolling moves the
window and drags the cursor along, keeping the selection inside it —
`clampScroll`'s invariant for the keyboard.

Double-click activation shares `activate()` with `enter`, so both emit the same
`OpenEntryMsg` and reach the same read-only preview below; the window is the
400 ms the explorer uses.

## Extraction to disk (#2249)

The pane stays read-only in both directions — it never writes into an archive,
and it never writes *out* of one either: `e`/`E` (or the palette's
`archive.extractEntry` / `archive.extractAll`, which act on the focused viewer)
only emit `archview.ExtractMsg`, naming the archive and the members. Everything
else is the root model's, in three steps:

1. **Target-directory prompt** — one path line with tab completion, the same
   shape as the HTTP response save (#2059). It is prefilled with a directory
   *next to the archive*, named after it without its archive suffix
   (`backup.tar.gz` → `./backup`), so the default never scatters members beside
   the file. A relative path is project-relative, `~` expands, and the
   directory is created if it does not exist.
2. **Plan** — `archive.PlanExtract` reads headers only and reports what would
   happen: the members selected (a directory name stands for its subtree), the
   ones refused with a reason, the targets that already exist, and the declared
   total size. Existing targets raise the overwrite guard
   (`[s/enter]` skip them and extract the rest · `[o]` overwrite · `[esc]`
   cancel); the answer is per run, not per file, and skipping is the primary
   option so `enter` never destroys files the user already had.
3. **Write and report** — `archive.Extract` streams the archive a second time
   and writes the plan's members. The summary toast names the file count, the
   bytes and the destination, plus `— n skipped (reasons)` when anything was
   declined; a run with skips is a warning, not an info.

The safety rules all live in `internal/archive/extract.go`, so they hold for
any caller, not just the pane:

- **Path sanitization.** `SafeTarget` refuses absolute member names, any `../`
  component (after `path.Clean`, so `deep/../../x` is caught too) and Windows
  volume names, and then re-checks that the joined path is still inside the
  destination. A refused member is *skipped and reported* (`unsafe path`),
  never silently clamped into the target dir.
- **Links are never materialized.** A symlink or hard-link member is skipped
  (`link entry`): a link pointing out of the tree is the second half of every
  traversal escape. Device and fifo entries are skipped as `special file`. An
  existing *symlink* at a target is replaced rather than written through, for
  the same reason.
- **Byte cap.** `DefaultExtractLimit` (1 GiB) bounds one extraction — the
  tar-shaped twin of the gz bomb guard, because a few kilobytes of archive can
  unpack to whatever it likes. It is enforced twice: the plan refuses a
  declared total over the ceiling before anything is written, and the write
  itself counts the *actual* bytes (a header size is untrusted), removing the
  partial file and stopping with a message that names the cap.
- **Overwrites are confirmed**, and a target that is a directory is skipped
  rather than replaced.

## Read-only entry preview

`Enter` on a file row emits `archview.OpenEntryMsg`; the root model extracts
the member and installs it via `editor.ShowReadOnly`. Guards applied before
anything is buffered:

- **Large-file threshold** — the entry is refused when it crosses
  `files.large_file_kb` ([#149](./editor.md)), the same ceiling a file on disk
  gets. `archive.ReadEntry` also reads one byte past the limit rather than
  trusting the (untrusted) header size, so a corrupt archive cannot make it
  buffer a whole stream.
- **Gzip members** are decompressed first — see below. Compressed bytes *are*
  binary, so without that seam a `logs/app.log.gz` would be refused as a blob
  nobody can look at.
- **Binary members** are refused with a notice: a NUL byte in the leading 8 KB
  is the test. Routing archives away from the editor would be pointless if a
  `.so` inside one still reached a text buffer.

The buffer's path is *virtual*: `<archive>!<entry>`, e.g.
`/tmp/src.tar!cmd/main.go`. Nothing on disk answers to that string, which is
exactly why the buffer is read-only — but its tail is the member's own file
name, so the tab title, the language lookup and the syntax highlighting all
resolve from `main.go` with no special casing anywhere. Re-opening the same
entry activates its existing tab (`TabForPath` on the virtual path).

### Gzip members open decompressed (#1948)

The two viewers compose: a `.gz` *inside* an archive is shown the way the [gz
viewer](./gz-viewer.md) shows a lone one. The seam sits in `openArchiveEntry`
right before the binary check — the member's bytes start with the gzip magic
`1f 8b`, so `openArchiveGzipEntry` takes over and hands them to
`gzfile.ReadBytes`, the in-memory twin of `gzfile.Read`. Nothing is
duplicated: inner-name resolution, the caps, the footer metadata and the
buffer install are the gz viewer's own code.

- **Naming.** The decompressed name goes back where the member lived:
  `logs/app.log.gz` → the virtual path `backup.tar!logs/app.log`. It is the
  same `archiveEntryPath` scheme as any other member, so the tab title reads
  `app.log (backup.tar)`, the language lookup answers `log`, and re-opening
  activates the existing tab.
- **Caps.** The limit is applied twice, and it has to be: `archive.ReadEntry`
  holds the *compressed* member to `files.large_file_kb`, and the read of the
  decompressed stream is capped again — that second one is the bomb guard,
  because a kilobyte inside a tar unpacks to whatever it likes. An oversized
  member opens truncated with a notice, never as a hang.
- **Still binary underneath** (a gzipped PNG): the buffer holds the gz
  viewer's metadata notice — archive, inner name, sizes, ratio — instead of
  mojibake.
- **A nested archive** — a member named `.tar.gz`/`.tgz`, or one whose
  decompressed first block holds a tar header — is refused with `nested
  archive — extract not supported`. There is no archive-inside-an-archive
  view; the point is that the user is told which case they hit instead of
  getting a blanket "binary archive entry". The name is checked *before*
  decompressing, so an inner tarball costs nothing to decline.

Refresh-on-change is *not* inherited: `refreshGzipBuffers` re-sniffs the path
it is given and a `.tar` is not a plain gzip, so it declines. Like every other
extracted member, the buffer is a snapshot of the archive as it was read.

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

Highlighting is the one thing a read-only buffer cannot arrange for itself
(#1853). `ShowReadOnly` clears the span index and advances the document
version, and the ordinary trigger — an edit bumping that version — can never
fire here. So **every caller of `ShowReadOnly` owns the parse**: it returns
`ed.Reparse()` (or batches it) into the command it hands back. That holds for
the entry open, the gz open, the schema view and the gz watcher refresh alike.
The parse resolves against the virtual path like any other, and its
`highlight.SpansMsg` routes back by that exact string. Tree-sitter is the
whole story: no LSP `didOpen` is ever fired for a virtual path, because no
server could open the document it names.

The chrome marks it: the pane title reads `main.go (src.tar) [RO]` and the tab
label carries the same `[RO]` suffix.

Session state follows from the same fact — a read-only preview's path names no
file, so it is skipped when persisting an editor pane's tabs and never enters
the reopen ring (#158). Like a scratch tab, it is session-local.

## Boundaries

- **Read-only, always.** There is no write-back *into* an archive, and none is
  planned. Extraction out of one is supported (#2249) and is one-way.
- **No per-file overwrite dialog.** The guard answers for the whole run;
  cherry-picking is what extracting a single member is for.
- **Zip is out of scope**, but nothing in the pane, the pane kind or the UI
  strings says "tar": `archive.Format` classifies, everything above speaks of
  archives, so a zip reader slots in beside `FormatTar`.
- **xz / zstd** are not supported — neither has a standard-library reader, and
  the no-new-dependency rule holds.
- A listing is capped at 50 000 entries; the cap is reported in the header
  (`truncated`), never silently applied.
