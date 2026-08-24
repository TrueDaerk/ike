---
type: concept
title: Usages Tool Window
description: Singleton bottom-split pane holding the latest find-references result persistently — grouped by file, line:col + preview rows, enter/double-click jumps, 'r' re-runs the search; filled by lsp.referencesPanel while lsp.references keeps the quick palette (#1155), and by the palette overlays' Open in Find window hand-off (#2055).
resource: internal/usages/usages.go
tags: [architecture, lsp, references, find-usages, tool-window, pane]
timestamp: 2026-08-24T00:00:00Z
---

# Usages Tool Window (#1155)

JetBrains' Find Usages tool window scaled to the terminal: a persistent
worklist for find-references results. `lsp.references` keeps its transient
palette list (quick mode — see [LSP](./lsp.md)); the new
`lsp.referencesPanel` ("LSP: Find Usages (Panel)") runs the **same**
`textDocument/references` request but fills this singleton tool pane instead,
so the result survives navigation and can be worked through reference by
reference.

## Wire

The bridge (`plugins/lsp/bridge.go`) knows which command ran and picks the
delivery message:

- `lsp.references` → `ilsp.ReferencesMsg` → the palette refs mode
  (`internal/app/references.go`), unchanged.
- `lsp.referencesPanel` → `ilsp.UsagesMsg`, a parallel message carrying:
  - `Symbol` — the identifier under the cursor **at request time**
    (`identAt` over the synced document line), for the title;
  - `Path`/`Line`/`Col` — the request origin;
  - `Refs` — the shared `locationsToRefs` conversion (editor coordinates +
    trimmed preview line per location, declaration included);
  - `Refresh` — a bridge-built `tea.Cmd` continuation that re-runs the
    request at the stored origin, mirroring `CallHierarchyMsg.Fetch`.

The app handler (`fillUsagesPanel`, `internal/app/usages_panel.go`) opens the
pane if needed, fills it, and focuses it — an empty result fills the pane
with its found-nothing state rather than toasting.

## The pane

`internal/usages.Model` follows the Problems pane blueprint
([Problems](./problems.md), #1024): a value-type model embedded in a
`pane.Instance` (`pane.KindUsages`, singleton key `"usages"`, context id
`"usages"`), opened as an adaptive split of the active editor (`auxZone`,
#1588 — below, or right of a wide landscape host). The
`usages.toggle` palette command (no default chord — the budget is full) runs
the shared toggle state machine: open → focus → return focus.

Rows group by file in server order (headers accented, first-appearance file
order, within-file order untouched); each reference row shows 1-based
`line:col` plus the trimmed source-line preview. The header line — and the
`Title()` seam — carry the searched symbol and totals:
`Usages: Foo — 12 in 4 files`. The cursor starts on the first reference.

## Interaction

- `j`/`k`/arrows move, `g`/`G` home/end; `enter` opens the reference via
  `ilsp.DefinitionMsg` — the same open-location path the palette list uses.
- Mouse mirrors the siblings (#514): click selects, double-click within
  400 ms activates, wheel scrolls dragging the cursor along; the unfocused
  cursor row renders muted (#1034).
- `r` **refreshes**: it dispatches the carried `Refresh` continuation, which
  re-runs the references request for the stored `(path, position)` the
  result was created from. Best-effort by design: after edits the stored
  position re-resolves as-is (it may sit on a different token); the symbol
  name in the title stays the originally captured one.

The editor context menu (#1020) offers "Find Usages (Panel)" alongside the
quick "Find Usages" entry.

## Handed-over result sets (#2055)

The pane is also the target of "Open in Find Window": from the
search-everywhere overlay or the transient find-usages popup, `cmd+enter` /
`ctrl+enter` (`find.openInPanel`, `palette` context) tips the currently listed
hits in here and closes the overlay — see the [command
palette](./command-palette.md) for the seam that reads the rows.

The Usages pane is reused rather than a second "Find Results" pane added: both
list locations grouped by file with "enter jumps there", and one singleton
keeps the layout persistence, the toggle state machine, the mouse handling and
the tool-window menu entry single-sourced. What differs is the heading, so the
two sources stay distinguishable:

- `Set(symbol, refs, refresh)` — a find-references run: `Usages: Foo — …`.
- `SetTitled(title, refs)` — a handed-over set: the caller's heading
  (`Find: foo`, `Usages`) and **no** refresh continuation, since there is no
  stored origin position to re-run; the empty state reads `(no results)`.

A second hand-off replaces the content, the way JetBrains' Find tool window
reuses its tab. Entries navigate exactly like reference rows.

## Persistence

Like the Problems pane, the layout slot persists (`paneIdentity{Kind:
"usages"}` in `internal/app/store.go`) and **restores empty** — results are
session state; the next `lsp.referencesPanel` run re-fills it.
`window.hideAllTools` treats it as a tool window.
