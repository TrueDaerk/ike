---
type: concept
title: Gz Viewer
description: "#1763 — a plain .gz opens transparently decompressed in a read-only editor buffer with the inner file's language and highlighting; the decompressed-byte cap is the bomb guard, tarballs route to the archive viewer instead."
resource: internal/gzfile
tags: [architecture, gzip, viewer, read-only, large-file, open-in-browser]
timestamp: 2026-08-28T00:00:00Z
---

# Gz Viewer (#1763)

A plain `.gz` holds **one** compressed file, and its natural viewing form is
the decompressed text. So it gets no pane of its own: opening `app.log.gz`
decompresses into memory and shows the content in a **read-only** editor
buffer, with the inner file's language, highlighting and features — log mode
and its repeat folding included.

Two files carry it:

- `internal/gzfile` — the format layer: the routing decision, the inner-name
  resolution, the capped read. Standard library only (`compress/gzip`).
- `internal/app/gzfiles.go` — the plugin, the buffer install, the metadata
  notice and the watcher refresh.

## Routing: gz vs. tarball

The compile-in `gzip` plugin claims files through its handler's `Match` and
registers **no extensions at all**. That is deliberate: the registry matches
`filepath.Ext`, which reads `.tar.gz` as `.gz`, so claiming the extension
would steal every compressed tar from the [archive
viewer](./archive-viewer.md).

`gzfile.IsPlain` is the single decision both viewers agree on, and it is the
exact complement of `archive.IsArchive`:

1. no gzip magic (`1f 8b`) → not ours;
2. a name ending in `.tar.gz`, `.tgz`, `.tar.bz2`, `.tbz`, `.tbz2`, `.tar.z` →
   not ours, whatever the payload turns out to be: the name is what the user
   asked for;
3. a stream whose first decompressed 512-byte block holds a tar header → not
   ours (that check lives in `archive.Detect` and costs one block, not the
   archive).

Everything else is a plain gzip. Exactly one of the two handlers ever answers,
so `backup.tar.gz` lists its members while `app.log.gz` opens decompressed.

That split is about *files*. A gzip stream that is an archive **member** is
not routed by a handler at all — the [archive viewer](./archive-viewer.md)
opens it itself, through `gzfile.ReadBytes` and this package's caps and
inner-name rules, so `backup.tar!logs/app.log.gz` reads as decompressed text
(#1948). The nested case is the same decision under a different name:
`gzfile.IsNestedArchive` answers it with `HasTarSuffix` plus
`archive.LooksLikeTar`, and the pane declines rather than opening tar blocks.

## The inner name decides the language

The buffer's path is virtual, in the same shape the archive viewer uses:
`<archive>!<inner>`, e.g. `/var/log/app.log.gz!app.log`. Its tail is the inner
file's own name, so the tab title, the language lookup and the highlighting
resolve from `app.log` with no special casing — `.log.gz` gets log mode,
`.json.gz` gets json.

`gzfile.InnerName` resolves that name, extension stripping **first**:

- strip a trailing `.gz`/`.gzip` from the file's base name (`app.log.gz` →
  `app.log`), case-insensitively;
- otherwise fall back to the gzip header's optional original-name field,
  reduced to its base name so a recorded absolute path cannot escape;
- otherwise keep the file's own base name.

The header field comes second on purpose: it is optional and routinely records
whatever the compressing tool happened to be looking at.

The one case where it wins anyway (#1853): stripping left **no extension**.
`dump.gz` reduces to `dump`, which names no language at all, so a header
naming `dump.sql` is the only thing that can give the buffer one. A header
carrying no extension of its own never wins — it would trade one anonymous
name for another.

## Caps: the decompression bomb guard

The ceiling is counted in **decompressed bytes**, never compressed size — a
gzip bomb is a few kilobytes on disk and unbounded in memory. `gzfile.Read`
takes the limit as `io.LimitReader(zr, limit+1)`, so the read stops one byte
past the cap and `Truncated` distinguishes "exactly at the cap" from "more to
come". A capped read costs one limit-sized buffer whatever the stream claims.

Both [large-file thresholds](./editor.md) (#149) apply, the same ceilings a
file on disk gets:

- `files.large_file_kb` caps the bytes during the read;
- `files.large_file_lines` caps the lines afterwards — a log compresses far
  below the byte cap and can still be unusably long.

Either cap firing shows the truncation notice as a warning toast; the content
that *was* read still opens. A corrupt or half-written tail is treated the same
way: the bytes decompressed so far are kept and flagged truncated, because a
partially written log is worth reading. Only a stream yielding nothing at all
is an error.

## Binary content degrades to metadata

A NUL byte in the leading 8 KB (the same cheap test `grep` uses, shared with
the archive viewer) means a text buffer has nothing useful to show. Instead of
mojibake the buffer holds a metadata notice — the archive name, the inner
name, the compressed and decompressed sizes, and the ratio:

```
Binary content — no preview.

Archive:       blob.bin.gz
Content:       blob.bin
Compressed:    1.2 KB
Decompressed:  8.0 KB
Ratio:         6.7×
```

The decompressed size comes from the gzip footer's `ISIZE` field, read from
the last four bytes of the file. It is **advisory only** — the field records
the length modulo 2^32, so a multi-member or over-4 GiB stream reports the
wrong number — which is why no allocation is ever sized from it and a
truncated read with no trustworthy size shows no ratio at all rather than a
wrong one.

## Read-only and reload

The buffer goes in through `editor.ShowReadOnly`
([archive-viewer](./archive-viewer.md#read-only-buffers) describes the seam):
every mutation refuses with `E45: buffer is read-only`, `saveAs` fails, so the
virtual path can never become a file. The pane title reads
`app.log (app.log.gz) [RO]` and the tab label carries the same `[RO]`. Like
any read-only preview it is session-local — skipped when persisting a pane's
tabs, kept out of the reopen ring.

Reload needs its own wire. The buffer's path names content *inside* the
archive, so the editor's own watcher matching never fires for the file that
actually changed. The root model handles it: on a `FileChanged`/`FileCreated`
event, `refreshGzipBuffers` re-sniffs the path, finds every read-only tab
under the `<path>!` prefix and re-installs the freshly decompressed content.
Nothing can be lost — the buffer is read-only, so it is never dirty. A read
that fails mid-write keeps the previous content. Opening a `.gz` also
`Track`s the **outer** file with the watcher, so the poll fallback compares
the thing that exists on disk.

The refresh returns the **reparse command** for every buffer it re-installed,
and the root model batches it into the event's own commands. `ShowReadOnly`
drops the cached spans and advances the document version, and a read-only
buffer can never schedule a parse the way an edit does — so without that
command the preview would go plain the first time the file changed and stay
plain for the rest of the session (#1853).

## Open in Browser unpacks, not previews (#2298)

"Open in Browser" (`internal/app/openinbrowser.go`, #1429) reaches into a
plain gzip file the same way as the preview above, but a browser cannot
render gzip bytes directly, so instead of a read-only buffer it decompresses
into a real file and opens that. `gzipArchiveOf` resolves the target back to
the on-disk `.gz` — directly, or by stripping the `<archive>!<inner>` suffix
off an already-open preview buffer's own path — then reuses `gzfile.IsPlain`
and `gzfile.Read` unchanged: a compressed tar is still declined (routed to
the archive viewer instead), and the same `files.large_file_kb` cap guards
against a decompression bomb, refused with a toast rather than opened
partially.

The unpacked copy lands under an ike-owned scratch directory
(`$TMPDIR/ike-open-in-browser`), in a subdirectory keyed by the archive's own
absolute path (a SHA-1 hex digest, to dodge path-length and character
issues) — so reopening `report.html.gz` overwrites the same unpacked file
instead of piling up a new one every time. The whole directory is swept on a
clean exit and again at the next startup, so a kill or a crash never leaves
unpacked copies behind for good.

## Boundaries

- **Read-only, always.** No write-back into a `.gz`; recompressing on save is
  not planned.
- **Single-member gzip only.** A multi-member stream reads through as
  concatenated content (what `gzip -dc` does); its footer size is wrong, which
  only ever costs the metadata notice its ratio.
- **bzip2, xz, zstd, `.Z`** are out of scope here. bzip2 is readable by the
  stdlib but only reaches IKE as a tarball today; xz and zstd have no
  standard-library reader and the no-new-dependency rule holds.
