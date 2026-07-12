---
type: concept
title: TODO Index
description: "#61 — JetBrains-style TODO tool window: project-wide comment-tag index (TODO/FIXME/HACK/XXX, configurable) as a centered overlay over the locations list, own search.Service scan, per-file rescan on save, tag/current-file filters, status-line count."
resource: internal/todoindex
tags: [architecture, todo, comment-tags, overlay, search]
timestamp: 2026-07-12T00:00:00Z
---

# TODO Index (#61)

`internal/todoindex` is the JetBrains TODO tool window: every comment tag in
the project (`TODO`, `FIXME`, `HACK`, `XXX` by default), listed grouped by
file and navigable. It is the second consumer of the reusable
[locations list](/architecture/search.md) component after find-in-path.

## Opening and navigation

`todo.list` (palette, `cmd+6` — JetBrains' TODO-window chord — or leader
`space D` / `ctrl+k D`) opens a centered overlay, the same floating pattern as
the finder. `up`/`down`/`j`/`k` and the page keys walk entries (file headers
are labels, not stops), `enter` opens the file with the cursor on the tag
(`OpenLocationMsg`), `esc` closes. Mouse: click selects, click-again opens,
wheel scrolls, click outside dismisses.

## Scanning

The index drives its **own** `search.Service` (the streaming find-in-path
scanner, so gitignore/hidden/binary rules match, spec #29) with the query
`(?:TODO|FIXME|…)` as a whole-word, case-insensitive regex — the match range
is exactly the tag word, which classifies each entry. Its streamed
`search.BatchMsg`/`DoneMsg` arrive **wrapped in `todoindex.ScanMsg`**: the
finder consumes the bare types filtered only by generation, and two
independent services count generations separately, so unwrapped messages could
cross-contaminate.

- **Full scan** runs at app `Init` (after the program sender is wired) and
  again after a project switch (the switch rebuilds the model and re-runs
  `Init`), plus on demand (`ctrl+r` in the overlay).
- **Incremental**: a buffer save emits `todoSavedMsg` from the editor emitter
  (goroutine-indirected like `SyncMsg`); the root model answers with the
  index's single-file rescan `tea.Cmd`, whose `FileScanMsg` splices that
  file's entries in place. Files outside the project root or under hidden
  path components are skipped; generation guards drop results that a newer
  full scan superseded.

## Filters

Filters are applied in-memory over the retained entry set — toggling never
rescans. `ctrl+t`/`alt+t` (or clicking the label) cycles the tag filter
(All → TODO → FIXME → …), `ctrl+o`/`alt+o` toggles current-file-only (the
active editor's file at open time). The status row shows filtered counts and
truncation; `Count()` stays the unfiltered total.

## Configuration

```toml
[todo]
patterns = ["TODO", "FIXME", "HACK", "XXX"]
```

Entries are literals (quoted into the regex), matched as whole words,
case-insensitively; an empty list falls back to the defaults
(`todoindex.DefaultPatterns`). The flattened key is `todo.patterns`
(comma-joined), read by the app at construction.

## Status line

A `todo` segment ([status line](/architecture/status-line.md)) renders
"12 TODOs" from the retained index — hidden until the first full scan
finishes or while the project is clean, live without the overlay open thanks
to the Init-time scan.
