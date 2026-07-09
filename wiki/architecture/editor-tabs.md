---
type: concept
title: Editor Tabs
description: The per-pane tab model — each editor pane hosts an ordered document list with one active tab; opening routes into the focused pane's tab list, closing peels tabs before the pane.
resource: internal/pane/instance.go
tags: [architecture, panes, tabs, editors, shared-documents, close]
timestamp: 2026-07-09T12:00:00Z
---

# Editor Tabs

Roadmap 0190 (#156). An editor pane no longer hosts exactly one document: each
`pane.Instance` of `KindEditor` holds an **ordered tab list** (`[]*editor.Model`)
plus an **active index**, JetBrains-style. The layout tree is untouched — a leaf
is still one pane; tabs live entirely inside the instance.

## The tab model (`internal/pane`)

- Every editor instance starts with a single scratch tab and **never has zero
  tabs** — closing the only tab is refused (`CloseTab` returns false) and the
  caller closes the pane instead.
- `Editor()` returns the **active tab's** model, so the pane surface (render,
  key routing, status/title) keeps its one-document view of the world.
  `Editors()`, `TabEditor(i)`, `TabCount()`, `ActiveTab()`, `TabForPath(path)`
  and `EditorForPath(path)` expose the full list for "all documents of this
  pane" sweeps.
- Operations: `AddTab` (appends at the end, inheriting size/config/palette and
  activating), `ActivateTab`, `MoveTab` (reorder, the moved tab stays active),
  `CloseTab` (the right neighbour slides in and takes over; the last position
  falls back left).
- Focus lives on exactly one tab: `SetFocused`/`ActivateTab` re-assert per-tab
  focus flags so background tabs are always blurred. `SetSize` sizes every tab,
  so switching never renders through a stale viewport.
- Message routing: `Update` targets the active tab; `UpdateForPath(path, skip,
  msg)` reaches every tab showing path (background tabs included) and
  `UpdateTab(i, msg)` a specific one — the seams path-routed messages (sync,
  highlight, LSP results, save-all) travel through.

## Opening routes into the tab list (`internal/app`)

All open seams (explorer, palette `@`, find-in-path, `host.API`) already funnel
through `openPath`; it now lands the file in the focused pane's tab list via
`openInTab`:

- a tab already showing the file is **activated** (no duplicate tab);
- a pathless scratch tab is **filled in place** (fresh panes keep the old feel);
- otherwise a **new tab is appended**, after autosaving the document being left
  (#174) — tab switches autosave the same way (`activateTab`).
- **Open-in-new-pane keeps its split behaviour** unchanged.

Shared documents (#142) are reused verbatim: `loadOrShare` scans all tabs of all
panes, so one file open in two tabs — same pane or different panes — aliases one
buffer and one undo history. `editorKeysForPath` matches any tab;
`editorViewsForPath`/`editorForPath` resolve per-view editor models; sync
broadcasts skip only the originating tab, reaching background tabs of the same
pane.

## Closing peels tabs before the pane

`editor.closeTab` (cmd+w, `:q`, `CloseFocused`) closes the **active tab** and
only closes the pane when the last tab goes — single-tab panes feel exactly like
before. A closing tab applies the same unsaved-changes guard as a pane close:
its crash-backup snapshot (#165) is dropped unless another tab or pane still
shows the document. Externally deleted files close their tab (the pane survives
while other tabs remain); moved files re-path every tab that holds them.

## Tab bar rendering (#157)

The bar occupies the **pane's top row** — the same line the single-document
title used (`internal/app/tabbar.go`, hooked in `renderPane`), so showing tabs
costs no editor row. With one tab the classic title renders; with two or more
(or `editor.tabs.always_show = true` under `[editor.tabs]`, live-reloadable,
with a settings-page toggle) the tab list does:

- **Labels**: file basename (`untitled` for scratch tabs); duplicates
  disambiguate with the display-relative directory (`main.go — cmd/ike`); a
  dirty tab carries ` ●`, a stale one `!` (file changed on disk while dirty,
  0140 — externally deleted dirty files surface the same way).
- **Highlighting** reuses theme slots: the active tab renders `Accent` + bold,
  inactive tabs `Foreground`, separators (`│`) and end ellipses `Border`.
- **Overflow** never wraps: `tabWindow` grows a run of tabs around the active
  one (rightward, then leftward) while separators and end ellipses still fit;
  hidden tabs are marked with `…` at that end, and a lone oversized active
  label truncates.

## Deferred to sibling issues

Tab commands & keybindings (#158), mouse support on the bar (#159), and
per-pane tab persistence in `session.json`/`layout.json` (#160). Until #160
lands, the layout store keeps recording only each pane's active document.
