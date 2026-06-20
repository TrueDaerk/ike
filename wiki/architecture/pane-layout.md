---
type: concept
title: Pane Layout & Drag
description: Pure split-tree layout model driven by mouse drag — divider resize and title-bar move/swap — with per-project geometry persisted in a dedicated state store.
resource: internal/layout/tree.go
tags: [architecture, layout, panes, mouse, drag, resize, persistence, bubbletea]
timestamp: 2026-06-20T00:00:00Z
---

# Pane Layout & Drag

Roadmap 0036. The tiled pane layout is a pure **split tree** that the root model
manipulates with the mouse and persists per project. It replaces the foundation
slice's hard-coded two-pane tiling (`explorerWidth` + `JoinHorizontal`) with
rectangles computed from the tree, while staying additive — every action remains
reachable without a mouse, and a missing or stale saved layout never crashes or
hides a pane.

## The layout tree

`internal/layout` is **pure**: geometry and tree structure only, no bubbletea and
no I/O, so it is fully unit-testable.

- A **leaf** (`Leaf{Pane}`) is a single pane id (`"explorer"`, `"editor"`, later
  any plugin pane). A **split** (`Split{Orient, Ratio, A, B}`) divides a region
  between two children at a ratio in `(0,1)`: `Horizontal` puts A left / B right
  with a one-column vertical divider; `Vertical` stacks A top / B bottom with a
  one-row horizontal divider.
- `Default(width, explorerCols)` reproduces the historical layout: a horizontal
  split with the explorer on the left at roughly `explorerCols` columns.
- `Compute(root, viewport)` walks the tree and returns a `Layout`: a map of every
  leaf's integer `Rect` plus the live `Divider`s. Children always tile their
  parent exactly — one cell is reserved per divider and rounding is handled so
  there are no gaps or overlaps. `Split.Children(rect)` is the shared seam that
  both `Compute` and the renderer use, so geometry and drawing never diverge.

## Mouse drag model

`internal/app` owns the only mutable drag state and all I/O. The program enables
mouse reporting via `tea.WithMouseCellMotion` in `cmd/ike`; the root model's
`tea.MouseMsg` branch runs a small state machine:

- **Press** hit-tests the cached `Layout` (`Layout.Hit`). A divider gutter starts
  a **resize**; a pane's first row (its title bar) starts a **move**.
- **Motion** during a resize calls `Divider.ResizeTo`, which updates the owning
  split's ratio, **clamped** so neither child drops below a minimum cell size — a
  pane can never be dragged to zero.
- **Release** during a move resolves the drop target and `DropZone`
  (left/right/top/bottom of the target pane), then `layout.Move` re-parents the
  dragged leaf — swapping order or re-orienting the split. v1 only relocates the
  existing pane set; it never creates or destroys splits. Release commits and
  persists.

One gesture is active at a time. While a floating shell (Roadmap 0035) is open,
mouse input is ignored — overlays are composited above the tiling and are not
draggable. Wheel events are ignored by the drag machine.

**Live feedback.** During a move the drag tracks the latest mouse cell
(`dragState.curX/curY`, updated on every motion). The pane being carried is
tinted (and prefixed with `⤴`), the pane under the cursor is tinted as the drop
target with its title showing the resolved zone (`◧ left` / `right ◨` / `⬒ top`
/ `⬓ bottom`), and the status line narrates `MOVE <src> → <zone> of <target>`.
Resize feedback is the divider tracking the cursor in real time as the ratio
updates per motion frame.

## Persistence

Layout is runtime UI state, not user configuration, so it lives in its own
per-project state file rather than `settings.toml`:

- The store (`internal/app/store.go`) writes `layout.json`. The discovery seam
  mirrors what Roadmap 0040 will expose: `IKE_CONFIG_DIR` overrides the location
  (used by tests to redirect writes); otherwise it lives under the project's
  `.ike/` directory.
- `state.go` converts the tree to/from plain JSON (`Encode`/`Decode`). **Decode
  is tolerant**: it accepts a saved tree only when its leaves are exactly the
  live pane set (no unknown ids, duplicates, or missing panes), otherwise the
  caller falls back to the default. A stale layout is silently dropped.
- Save is **debounced to drag release**, never written per motion frame.

## Out of scope (v1)

Creating/destroying splits, detached/floating windows, tabbed pane groups,
keyboard-driven resize/move (a planned additive once Roadmap 0080 owns the
keymap; the geometry ops here are binding-agnostic and reusable by it), and drag
animations.
