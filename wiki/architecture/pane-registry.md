---
type: concept
title: Pane Registry & Multiple Editors
description: The registry mapping layout-leaf instance keys to live pane components — the explorer singleton plus N editors — with focus as the focused leaf and open-in-new-pane intent.
resource: internal/pane/registry.go
tags: [architecture, panes, registry, editors, focus, open-target, persistence]
timestamp: 2026-07-09T00:00:00Z
---

# Pane Registry & Multiple Editors

Roadmap 0037. Where [Pane Layout & Drag](./pane-layout.md) is the pure geometry
of the split tree, `internal/pane` owns the **instances** its leaves map to. It
turns IKE's fixed two-component root (one `explorer.Model`, one `editor.Model`,
a two-value focus enum) into a **dynamic pane set**: the explorer stays a
singleton, editors become many, and a layout leaf is now a **stable instance
key** resolved through the registry.

## Instances

An `Instance` wraps one component — an `explorer.Model`, or for editors an
ordered **tab list** of `editor.Model`s with one active tab (see
[Editor Tabs](./editor-tabs.md)) — behind a uniform surface. It dispatches `Update`, `View`,
`SetSize`, `SetFocused`, and `Init` by kind, and advertises its **context id**
(`explorer` panes → `explorer`, editors → `editor`) so the root's
context-scoped command/keymap resolution (`focusContext`) keeps working
unchanged. `Explorer()` / `Editor()` hand out the underlying model pointer for
kind-specific calls. `internal/pane` is *almost pure* — it holds components but
performs no I/O — so its lifecycle is unit-tested independently of bubbletea
wiring.

## Registry & instance keys

`Registry` maps keys to `*Instance` and tracks the focused key:

- **Keys.** The singleton explorer keeps the stable key `"explorer"`
  (`ExplorerKey`), so context resolution, the default tree, and persistence all
  agree. Editors get **monotonic** keys minted in order — `"editor"`, then
  `"editor:2"`, `"editor:3"`, … — never reused within a session.
  `AddEditorKey(key)` recreates an editor under an exact saved key and advances
  the minting counter past it, so restore never collides with a future
  `AddEditor`.
- **Lifecycle.** `AddExplorer`/`AddEditor`/`AddEditorKey` create, `Get`/`Has`
  look up, `Close` drops (clearing focus if the closed key held it), `Keys`
  iterates in insertion order, and `SetFocused`/`Focused`/`FocusedInstance`
  manage focus — `SetFocused` marks exactly the focused instance and blurs the
  rest; an absent key clears focus without panicking.

## Focus is the focused leaf

The root model (`internal/app`) replaces its `focus` enum and two component
fields with a `*pane.Registry` plus a `recentEditor` key:

- **Focus** is the registry's focused key, which always names a layout leaf.
  `setFocus` marks it and remembers it as `recentEditor` when it is an editor.
- **Tab** (`cycleFocus`) advances to the next leaf in tree-walk order
  (`layout.Leaves`). `FocusDir(dir)` moves focus to the spatially adjacent leaf
  using the computed rectangles, bound by default to **Ctrl+arrows** and
  overridable via `keymap.bindings.focus_{left,right,up,down}` (Cmd is not used —
  most terminals never deliver it to a TUI). A mouse click in a pane interior
  focuses that leaf (`paneClick`). `Ctrl+W` closes the focused editor pane.
- **Routing.** `routeKey`, `editorCapturing`, and the quit/`q` logic consult the
  focused instance's kind instead of an enum. An editor still captures text in
  insert/command mode and shadows global single-letter keys.
- **The active editor.** Many ops target "the editor that should act":
  `activeEditorKey` returns the focused editor, else `recentEditor`, else the
  first editor in tree order. The status line and editor `ActionMsg`s use it; a
  Replace open with no editor present spawns one.

## Open-in-new-pane intent

"Where to open" is an explicit, additive flag that defaults to today's behaviour:

- `explorer.OpenFileMsg` and `host.OpenFileRequest` each gain a `NewPane bool`
  (primitive flags rather than a `pane.OpenTarget`, to keep the `host`/`explorer`
  packages free of an import cycle with `internal/pane`). The canonical
  `pane.OpenTarget` / `pane.Open` enum is the app-internal representation.
- The plain explorer open (`enter` / `l`) stays Replace; the modified open
  (`o`, a placeholder until Roadmap 0080 owns the binding) emits `NewPane`.
- `host.API` keeps `OpenFile(path)` (Replace) and gains `OpenFileIn(path,
  newPane)`, so existing plugins stay source-compatible.
- `openPath` honours the flag: Replace loads into the active editor; NewPane
  `AddEditor` + `layout.SplitLeaf` then loads into the fresh instance. A claiming
  `FileHandler` still gets first refusal regardless of target, and
  `EventFileOpened` hooks fire either way.

## Persistence

Editor identity (which file each editor holds) rides the layout store's per-leaf
identity table — see [Pane Layout & Drag › Persistence](./pane-layout.md). On
restore the registry is rebuilt from the saved leaves: the explorer plus one
editor per non-explorer leaf, each reloading its file best-effort (a missing file
becomes an empty editor). Cursor/scroll framing for the focused editor remains
the job of [Session Restore](./session-restore.md).
