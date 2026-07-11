---
type: concept
title: Pane Layout & Drag
description: Pure split-tree layout model driven by mouse drag — divider resize and title-bar move/swap — with per-project geometry persisted in a dedicated state store.
resource: internal/layout/tree.go
tags: [architecture, layout, panes, mouse, drag, resize, split, close, persistence, bubbletea]
timestamp: 2026-07-11T00:00:00Z
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

- A **leaf** (`Leaf{Pane}`) carries an opaque string. Under Roadmap 0036 that was
  a global pane id; Roadmap 0037 reinterprets it as a **pane instance key** (see
  [Pane Registry](./pane-registry.md)) — the layout package stays oblivious to
  what a leaf means. A **split** (`Split{Orient, Ratio, A, B}`) divides a region
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

**Wheel coalescing (#238).** Wheel events do not apply immediately: the root
model folds them into a pending batch (consecutive events with the same cell,
button and modifiers merge into one counted entry) and schedules a single
`wheelFlushMsg` through the command queue. Because that flush message queues
behind whatever input is already backed up, a fast scroll burst lands in the
batch before the flush arrives and the whole burst applies in **one** update
pass — one render instead of one per event, so the UI never visibly "catches
up" on stale scrolls. Any non-wheel message flushes the pending batch first,
preserving ordering against clicks, keys and motion; a stale flush after an
inline flush is a no-op.

**Center merge zone (#318).** During a move or tab drag an **editor** target
whose drag carries files shows five zones, resolved by
`layout.DropZoneWithCenter`: the outer `CenterBand` (30%) of either axis is
the four edge zones (split/relocate exactly as before), the interior is
`ZoneCenter`, which **merges as tab** JetBrains-style. A whole-pane title
drag released there moves every file of the source editor into the target's
tab list (`openInTab` dedupes onto existing tabs) and closes the emptied
source pane (`mergePaneTabs`); a tab drag released there joins the target's
tab list with just that file. Drags without files to merge — an explorer or
terminal pane, or an empty editor — keep the plain four-zone relocate
behaviour everywhere.

**Self-edge spawn (Roadmap 0037).** A title-bar drag dropped on *another* pane
relocates (above). A drag dropped on the **source pane's own edge** — within an
outer band (`edgeBand`) of the resolved zone — instead **spawns** a fresh editor
split there via `layout.SplitLeaf`, so a pane can be cloned by dragging it to its
own side. A drop in the source pane's interior is a no-op. The release-time
spawn-vs-move decision is `commitMove`; the ghost preview labels it `new pane`.

**Tab-label drag (#305, #317).** In a multi-tab editor, pressing a tab-bar
segment grabs just that file (`dragTab`); the title row and the bar outside the
segments keep starting a whole-pane move. On release (`commitTabMove`): a drop
in **another editor's center zone** merges the document into that pane's tab
list, while its **edge zones** split a fresh editor next to it holding just
that file (#318); a drop on the **source pane's own edge** splits off a fresh
editor holding just that file; a drop on a **non-editor pane's edge zone**
(e.g. a terminal) likewise
splits that pane and opens the file in the fresh editor leaf (#317). A drop in a
non-editor pane's interior is a no-op — there is no tab list to join — and the
drag feedback (zone arrow, ghost, status hint) only signals a target there when
the cursor is in an edge zone. The ghost for a tab drag is labelled with the
dragged file's basename.

## Create / close ops (Roadmap 0037)

`split.go` adds the **create/close half** of the pane manager, both reusing
0036's `insert`/`remove`:

- `SplitLeaf(root, target, newPane, zone)` grows the `target` leaf into a split
  pairing it with a brand-new `Leaf{newPane}`, ordered by `zone` — structurally
  identical to the second half of `Move`, but the inserted leaf is fresh rather
  than removed from elsewhere. (Named `SplitLeaf`, not `Split`, because `Split`
  is the split-node type.)
- `Close(root, pane)` promotes `move.remove` to a first-class op: the leaf is
  detached and its parent split collapses so the sibling takes its place. Closing
  the **only** leaf returns the tree unchanged with `ok=false`, upholding the
  never-empty invariant.

The root model exposes these as binding-agnostic ops (`SplitFocused(zone)`,
`CloseFocused`, `FocusDir(dir)`, plus tab focus-cycle), so Roadmap 0080 binds
keys and the mouse reaches the same methods. `Leaves(root)` returns the leaf keys
in walk order for the focus cycle.

**Directional focus.** `FocusDir(dir)` (default Ctrl+arrow) resolves the target
through `focusTarget`, which scores every other pane by the computed
`layout.Compute` rectangles — *not* tree order. A candidate must lie in the
travel direction (centre past the current centre); among those it ranks panes
whose **perpendicular span overlaps** the current pane first, then nearest along
the travel axis, then best perpendicular alignment. The overlap rank stops a
tall full-width pane below from stealing a focus-right that should land on the
pane directly to the side.

**Live feedback.** During a move the drag tracks the latest mouse cell
(`dragState.curX/curY`, updated on every motion). The pane being carried is
tinted (and prefixed with `⤴`), the pane under the cursor is tinted as the drop
target with its title showing the resolved zone (`◧ left` / `right ◨` / `⬒ top`
/ `⬓ bottom` / `⧉ merge as tab` for the center zone), and the status line
narrates `MOVE <src> → <zone> of <target>`.
On top of that a **translucent ghost box** (a matte, dimmed shade of the
drop-target accent) is composited over the exact region the pane would occupy on
release — the relevant half of the target pane per the resolved zone, or the
**whole** target pane for the center merge zone (#318), whose ghost carries the
merge label — labelled with the dragged pane. It is drawn with `overlay.Place`, the arbitrary-position
sibling of `overlay.Center` (both splice ANSI-aware rows so styling survives the
seam). Resize feedback is the divider tracking the cursor in real time as the
ratio updates per motion frame.

## Persistence

Layout is runtime UI state, not user configuration, so it lives in its own
per-project state file rather than `settings.toml`:

- The store (`internal/app/store.go`) writes `layout.json`. The discovery seam
  mirrors what Roadmap 0040 will expose: `IKE_CONFIG_DIR` overrides the location
  (used by tests to redirect writes); otherwise it lives under the project's
  `.ike/` directory.
- `state.go` converts the tree to/from plain JSON (`Encode`/`Decode`). The
  original `Decode(data, valid)` accepts a tree only when its leaves are exactly
  a fixed pane set. Roadmap 0037 adds `DecodeTree(data)` which validates only
  **structural** soundness and leaf-id uniqueness and returns the leaf ids, so a
  dynamic host applies its own identity rules.
- With dynamic panes the store grows from a bare tree to a `{tree, panes}`
  wrapper: alongside the encoded tree, a **per-leaf identity table** maps each
  instance key to `{kind, path}` so a restored editor reopens its file. Old
  bare-tree files still load — their leaves are inferred (`explorer` → the
  explorer, everything else → a file-less editor).
- **Tolerant restore** (`internal/app`): the explorer must be present exactly
  once and every other leaf must be a well-formed editor key, else the default
  layout is rebuilt. A saved editor whose **file no longer exists** restores as an
  *empty* editor at that leaf — the split is preserved, never dropped.
- Save is **debounced to op/drag commit** (split, close, move, resize,
  open-in-new-pane), never written per motion frame.

## Out of scope

Detached/floating OS windows, tabbed pane groups within one leaf, cross-pane
shared buffers, maximise/zoom, named layout presets, keyboard binding *choices*
for split/close/focus-move (Roadmap 0080 owns the keymap; the ops here are
binding-agnostic), and drag animations.
