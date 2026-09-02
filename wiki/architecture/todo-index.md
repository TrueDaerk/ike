---
type: concept
title: TODO Index
description: "#61 — JetBrains-style TODO tool window: project-wide comment-tag index (TODO/FIXME/HACK/XXX, configurable) as a centered overlay over the locations list, own search.Service scan, per-file rescan on save, the shared list-filter row with tag/file/scope fields (ctrl+t and ctrl+o as its sugar), status-line count."
resource: internal/todoindex
tags: [architecture, todo, comment-tags, overlay, search, filter]
timestamp: 2026-09-02T00:00:00Z
---

# TODO Index (#61)

`internal/todoindex` is the JetBrains TODO tool window: every comment tag in
the project (`TODO`, `FIXME`, `HACK`, `XXX` by default), listed grouped by
file and navigable. It is the second consumer of the reusable
[locations list](/architecture/search.md) component after find-in-path.

## Opening and navigation

`todo.list` (palette, `cmd+6` — JetBrains' TODO-window chord) opens a
centered overlay, the same floating pattern as
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

Filters are applied in-memory over the retained entry set — nothing a filter
does ever rescans. Since #2156 they are one shared filter expression
([List Filter Syntax](./list-filters.md)), typed into the `internal/filterbar`
row under the chips and focused with `/` — or the shared find chord
`cmd+f` / `ctrl+f` (#2409), answered inside the overlay because it owns the
keyboard ahead of the keymap layer — like in every other list pane:

| Field | Takes |
| --- | --- |
| `tag:` | one of the configured pattern words (`TODO`, `FIXME`, …) — repeatable (OR) |
| `file:` | a path glob or substring |
| `scope:` | `file` (the active editor's file at open time) or `project` |

Bare words are the fuzzy match text, run over the tag's source line.

The two single-key filters are sugar that writes into that same expression:
`ctrl+t`/`alt+t` (or clicking the label) steps the `tag:` term through the
configured patterns and back to none, `ctrl+o`/`alt+o` toggles `scope:file`.
The chips row above the input renders those two terms — it is a view of the
filter, not state beside it — so a typed `tag:FIXME` lights the same chip the
key would. `esc` in a focused filter clears it; `esc` with the filter blurred
closes the overlay as before. The status row shows filtered counts and
truncation; `Count()` stays the unfiltered total.

## Rendering

Every overlay row hard-clips to the box's text area — one row is always one
terminal line (#1379). The text budget is `boxW - 6`: the lipgloss box style
renders at `Width(boxW-2)` *including* its border and padding, so the old
`boxW - 4` budget was two cells too wide and full-width rows wrapped, shifting
the whole layout. List rows and group headers clip in the shared locations
component (`ansiClip`); the filter and status rows clip through the local
`clipRow` — both are `ansi.Truncate`, never lipgloss `MaxWidth`, which wraps
overlong content instead of clipping (precedent: #971). Rune-count budgets
(`truncateRunes`) are safe against double-width runes (CJK/emoji) because the
cell-width `ansiClip`/`clipRow` pass is always the last step.

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
