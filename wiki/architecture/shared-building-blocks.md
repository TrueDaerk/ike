---
type: architecture
title: Shared Building Blocks
description: The catalog of reusable pieces every new pane, prompt, list, search line or tool window MUST be built from — one table per family with the helper, when it is mandatory, the guard test that enforces it, and the concept doc that explains it (0500 consolidation sweep, Epic #2458).
resource: internal/ui
tags: [architecture, ui, conventions, reuse, guard-tests]
timestamp: 2026-09-03T00:00:00Z
---

# Shared Building Blocks

IKE grew one pane at a time, and each pane re-implemented the same small
things: a one-line text field, a "/" search line, a cursor-and-window scroll
clamp, a click-to-select handler, a tool-window toggle. The 0500 sweep
(Epic #2458, 2026-09-03) collected those into shared helpers and removed
roughly a thousand duplicated lines. This page is the index of what exists,
so the next pane starts from the shared piece instead of from a copy of its
neighbour.

## The convention

> **A new surface MUST use the shared building block for anything listed
> below.** A hand-rolled version is allowed only when the block genuinely
> does not fit, and then the file carries an entry, with a reason, in the
> guard test's allowlist. The guard tests fail the build otherwise, and a
> second test fails when an allowlist entry has gone stale.

Two rules from the [text-input](/architecture/text-input.md) convention
apply to every family:

- **Caller chords win.** A surface runs its own bindings first and hands the
  rest to the helper.
- **Behaviour changes are documented.** Adopting a shared block that changes
  what the user sees (a caret appears, a list stops drawing blank rows) is
  recorded in the pane's concept doc and in `wiki/log.md`.

## Single-line text input

| Helper | Package | Use it for | Guard |
| --- | --- | --- | --- |
| `Field{Text, Cur}` with `Key`, `Paste`, `View`, `ViewSel`, `Set`, `Clear` | `internal/ui/field.go` | every one-line input: prompts, rename fields, filters, form fields | `internal/ui/inputsweep_test.go` |
| `EditKey`, `PasteText`, `CursorView`, `Typing` | `internal/ui/textinput.go` | the primitives behind `Field`; call directly only when the text lives in a struct you cannot change | same |
| `SpeedSearch` | `internal/ui/speedsearch.go` | type-ahead narrowing inside a modal picker | — |
| `filterbar.Model` | `internal/filterbar` | the permanent filter row of a list pane with a `filterexpr` schema | — |

Doc: [Single-Line Text Input](/architecture/text-input.md) (chord table, the
input-site audit), [Speed Search](/architecture/speed-search.md),
[List Filter Syntax](/architecture/list-filters.md).

## In-pane "/" search

| Helper | Package | Use it for | Guard |
| --- | --- | --- | --- |
| `LineSearch` (`Start`, `Key`, `Paste`, `Recompute`, `Apply`, `Step`, `Line`) | `internal/ui/linesearch.go` | a search that jumps between matches in a read-only or list view, opened with `/` or the find chord | `internal/ui/searchsweep_test.go` |
| `SmartCaseContains` | `internal/ui/linesearch.go` | the one matching rule (lowercase folds, any uppercase is exact) | — |
| `FindChord`, `MatchStepChord`, `MatchStep`, `StepWrap`, `StepOver`, `MatchCounter` | `internal/ui/findkey.go`, `matchstep.go` | the chords and the counter every search shares | — |
| `pane.Searchable` | `internal/pane/searchable.go` | the capability the root model dispatches cmd+f / cmd+g to | — |

Doc: [Search](/architecture/search.md) (in-pane search section, adopter and
deviation table).

## List panes: cursor, window, mouse, rendering

| Helper | Package | Use it for | Guard |
| --- | --- | --- | --- |
| `ClampWindow(cursor, top, n, height)` | `internal/ui/listnav.go` | the one scroll clamp: cursor into range, window follows, no trailing blank rows | `internal/ui/scrollsweep_test.go` |
| `ListNav(key, sel, n, page, keys)` | `internal/ui/listnav.go` | j/k, arrows, page keys, home/end, g/G | — |
| `WheelWindow`, `RowAt`, `ClickTracker.ClickRow`, `SelectClick` | `internal/ui/listmouse.go` | wheel, hit-test, click-to-select with double-click activate | — |
| `RenderWindow`, `ListPaneView`, `FileHeader`, `TargetRow[T]`, `StepFiltered` | `internal/ui/listpane.go` | the visible row window, the header/filter/rows/footer composition, header-row targeting, stepping over filtered rows | — |
| `locations.List` | `internal/locations` | a grouped location list with its own header-aware cursor (TODO index) | — |

Doc: [Selection-List Navigation](/architecture/list-navigation.md),
[Mouse Gestures](/architecture/mouse.md).

## Tool windows and viewers (`internal/app`)

| Helper | File | Use it for |
| --- | --- | --- |
| `togglePanel`, `togglePanelWith`, `ensurePanel`, `showPanel`, `setPanelReturn` | `internal/app/panelwiring.go` | the open / focus / return-focus state machine of a singleton tool window (one `panelReturnFocus` map, no per-panel fields) |
| `openToolPane(add, zone, after)` | `internal/app/panelwiring.go` | inserting a tool pane at `auxZone` or a fixed zone, with rollback |
| `openViewerPane`, `splitViewerPane`, `routeResult` | `internal/app/panelwiring.go` | content viewers (archive, hex, image, data, ES, remote): tab-host reuse, dedupe, split, routing a background result back |
| `armTick` | `internal/app/debouncetick.go` | a generation-guarded debounce tick |
| `renderCompletionPrompt` | `internal/app/completionprompt.go` | a path prompt with a candidate list |
| `overlayMouse` | `internal/app/overlaymouse.go` | click-outside-closes, centered hit-test and wheel for a floating overlay |
| `writeDirtyTabs(action)` | `internal/app/switch.go` | the save-all sweep (`write` vs `write_raw`) |

Doc: [Pane Registry](/architecture/pane-registry.md),
[Pane Layout](/architecture/pane-layout.md).

## Structured views

| Helper | Package | Use it for |
| --- | --- | --- |
| `hiertree.Tree[T]` | `internal/hiertree` | a lazily expanding hierarchy (call hierarchy, type hierarchy): rows, expand/collapse, parent walk, stale-reply bookkeeping, renderer |
| `gridview.DataRow`, `HeaderRow`, `Sidebar` | `internal/gridview` | a column grid with a sidebar (data viewer, Elasticsearch console) |
| `palette.FuzzyItems[T]`, `SortByScore` | `internal/palette/fuzzyitems.go` | a palette mode that fuzzy-matches a slice into items |
| `codepreview.TargetFrom[T]` | `internal/codepreview` | building a preview target from match ranges |
| `imgview.PlacedImage`, `SyncSeqs` | `internal/imgview` | inline images placed in a scrolling view |
| `ui.InputRow`, `ui.TogglesRow` | `internal/ui/formrow.go` | the find-in-path / all-projects form rows |

Doc: [Hierarchy Tree](/architecture/hiertree.md),
[Data Viewer](/architecture/data-viewer.md),
[Command Palette](/architecture/command-palette.md).

## Settings pages

| Helper | File | Use it for |
| --- | --- | --- |
| `fieldNav` (`newFieldNav`, `Update`, `Focus`) | `internal/settings/pagehelp.go` | tab / shift+tab / up / down between a form's fields |
| `pageClick` | `internal/settings/pagehelp.go` | click-to-select on a page's list, over `ui.RowAt` |
| `pageActionKey` | `internal/settings/pagehelp.go` | the a / enter / d switch with a confirm sentence |

Doc: [Settings UI](/architecture/settings-ui.md).

## LSP

| Helper | File | Use it for |
| --- | --- | --- |
| `call[R](m, ctx, path, capable, do)` | `internal/lsp/manager/manager.go` | every capability-gated request forwarder: server + document lookup, capability check, request timeout |

Doc: [LSP](/architecture/lsp.md).

## Leaf helpers

| Helper | Package | Replaces |
| --- | --- | --- |
| `linescan.Words`, `CommentStart` | `internal/linescan` | shell / crontab line tokenising (cron hints, permission hints) |
| `yamljson.Scalar`, `IsDecimal` | `internal/yamljson` | YAML scalar to JSON value (jq path, jq playground) |
| `pathglob.StarMatch` | `internal/pathglob` | star-only matching on non-path strings (unit names, secret keys); not `Match` |
| `excmd.ScanDelim` | `internal/editor/excmd` | delimiter scanning with backslash escapes (`:s`, `:g`) |
| `ui.HumanSize` | `internal/ui/sizing.go` | byte counts for humans |
| `registry.collect` | `internal/registry` | dedupe-then-sort of plugin contributions |

## The guards

Three sweep tests in `internal/ui` walk `internal/` on every `go test` and
fail on the shape of a hand-rolled copy outside `internal/ui`:

| Test | Catches |
| --- | --- |
| `inputsweep_test.go` | printable insertion straight off a key message, hand-written paste insertion, byte-sliced backspace |
| `scrollsweep_test.go` | the cursor-clamp-then-window-follow shape (`clampScroll`) |
| `searchsweep_test.go` | search-line state fields and the `searchKey` / `stepMatch` / `recomputeMatches` walkers outside `ui.LineSearch` |

Each carries an allowlist of `file → reason` and a companion test that fails
when an entry no longer matches, so an exemption cannot outlive the code it
was written for. Adding a new pane means either using the block or writing
the reason down; that review prompt is the whole point.

## Adding a block

A candidate for this page is code that exists in two packages with the same
shape. The sweep found its candidates with `dupl -t 80` over the non-test
sources plus a read of every `EditKey`, `clampScroll` and `case "/"` site;
running that again after a few months of pane work is cheap. A new block
goes into `internal/ui` when it is a value type or a rendering helper,
into a leaf package when it has no UI, and into `internal/app` only when it
needs the root model. It ships with tests, a row here, and, where a
hand-rolled copy could quietly come back, a guard.
