# Roadmap 0036 — Pane Drag: Mouse Move, Resize & Layout Persistence

Make the pane layout **directly manipulable with the mouse** and **durable
across sessions**. Today the root model tiles a fixed two-pane layout (explorer
`30` cols + editor) with hard-coded sizes in `internal/app`. This roadmap lifts
the geometry into a small, pure **layout model** (a split tree) and drives it
from `tea.MouseMsg`:

- **Resize** — drag the divider between two panes to change their split ratio.
- **Move** — drag a pane (by its title bar) onto another region to relocate /
  swap it, re-parenting the leaf in the tree.
- **Persist** — the resulting structure + ratios are saved and restored on the
  next launch, keyed per project.

It is the first concrete step of the broader **pane manager** that Roadmap 0035
deferred ("drag, move, or resize of the pane belongs to the broader pane
manager, not the floating primitive"). v1 operates on the existing pane set; it
does not yet create or destroy splits.

## Prerequisites / Dependencies

- **Roadmap 0010 (Foundation):** the root model in `internal/app` owns the
  layout and renders panes. Today `layout()` and `View()` hard-code
  `explorerWidth` and `JoinHorizontal`; this roadmap replaces that with rects
  computed from the layout tree and routes a new `tea.MouseMsg`.
- **Roadmap 0035 (Floating Shell):** unaffected and composited *above* the tiled
  layout — `overlay.Center` still draws the active shell over whatever the pane
  manager renders. The shell does not participate in tiling.
- **Roadmap 0040 (Settings):** provides the typed config + the **write path**
  used to persist layout. 0040 is read/merge-only for config; layout geometry is
  *runtime UI state*, not user-authored configuration, so this roadmap owns a
  small dedicated **state store** (see Persistence) rather than living in
  `settings.toml`. If 0040 lands first, the store reuses its discovery + write
  seam; if not, move/resize ship without persistence behind a feature flag and
  persistence is wired when the store exists.
- **Mouse input:** the bubbletea program must enable mouse reporting
  (`tea.WithMouseCellMotion`) in `cmd/ike`; the root model gains a
  `tea.MouseMsg` branch. Mouse stays additive — every action remains reachable
  without it (keyboard resize is a future additive, see Out of scope).

## Architecture

```
internal/layout/
  tree.go       split tree: Leaf(paneID) | Split{Orient, Ratio, A, B}; Rects(viewport) -> map[paneID]Rect
  rect.go       Rect geometry + hit-testing: which pane / which divider a point (x,y) falls in
  resize.go     drag a divider -> adjust the owning Split.Ratio, clamped to per-pane minimums
  move.go       drag a pane -> drop zones (left/right/top/bottom of a target) -> re-parent the leaf
  state.go      serialize/deserialize the tree (structure + ratios) <-> plain data for persistence
  layout_test.go
internal/app/   root holds the *layout.Tree, translates tea.MouseMsg into resize/move, renders into Rects
cmd/ike         enable mouse reporting (tea.WithMouseCellMotion)
```

Data flow:

```
tea.MouseMsg ─► app drag state machine ─► layout.{Resize,Move} ─► *layout.Tree
                                                   │
                          Rects(viewport) ─────────┘─► per-pane SetSize + View placement
                                                   │
                       on release ────────────────►  state.Save (per-project store)
launch ─► state.Load ─► *layout.Tree (fallback: built-in default tree)
```

- **`internal/layout` is pure** (no bubbletea, no I/O): geometry + tree ops only,
  fully unit-testable. `state.go` converts to/from plain data; the actual file
  read/write lives at the `internal/app` / store boundary.
- **`internal/app`** owns the drag state machine: on press it hit-tests
  (divider → resize, title bar → move), on motion it updates the tree, on release
  it commits and persists. Between drags it just renders `Rects` into the panes.

## Layout model

- A **leaf** is a pane id (`"explorer"`, `"editor"`, later any `plugin.Pane`).
  A **split** has an orientation (horizontal/vertical), a **ratio** in `(0,1)`,
  and two children (each a leaf or a nested split). The default tree reproduces
  today's layout: a vertical split, ratio ≈ `30/width`, explorer left.
- `Rects(viewport)` walks the tree and assigns each leaf an integer cell
  rectangle, reserving one column/row for each divider and the status line, with
  rounding handled so children exactly tile the viewport (no gaps/overlap).
- **Hit-testing** maps a mouse cell to either a divider (the gutter between two
  children of a split) or a pane interior (and, within it, the title-bar row used
  as the move handle).

## Design rules

- **Pure geometry, stateful host.** All tree math is pure and tested in
  isolation; only `internal/app` holds mutable drag state and does I/O.
- **Clamp, never collapse.** Resize clamps the ratio so neither pane drops below
  a minimum cell size; a pane can never be dragged to zero width/height.
- **One drag at a time.** A single active gesture (resize *or* move); v1 is
  single-level, matching the floating shell's stacking rule.
- **Move re-parents, doesn't create.** v1 relocates an existing leaf to a drop
  zone (swap sides / reorder); it does not split a pane into new panes or close
  panes. Creating/destroying splits is a later additive.
- **Persist per project, restore safely.** Saved layout is keyed by project root.
  On load, an unknown or invalid pane id is dropped and the tree falls back to
  the default for the current pane set — a stale saved layout must never crash or
  hide a pane.
- **Mouse is additive.** Disabling mouse (or a terminal without it) leaves a
  working default layout; no action is mouse-only forever (keyboard resize is a
  planned additive).
- **Overlays stay on top.** The floating shell (0035) and status line are
  composited after tiling and are not draggable here.

## Persistence

- Layout is **session/UI state**, not user configuration, so it is stored
  separately from `settings.toml` — e.g. a per-project state file under the IKE
  state dir (`{project_root}/.ike/layout.json` or a global state dir keyed by
  project path). Format is plain data produced by `state.go`.
- The store exposes `Load(projectKey) (*Tree, ok)` and `Save(projectKey, *Tree)`.
  It honors the same discovery/override seam as 0040 (`IKE_CONFIG_DIR` / explicit
  path) so tests can redirect it.
- Save is **debounced to drag-release**, not per motion frame, to avoid churning
  the file during a drag.

## Milestones

- [ ] `internal/layout/tree.go`: split-tree types (`Leaf`/`Split`), default tree, `Rects(viewport)` exact tiling with divider + status reservation.
- [ ] `internal/layout/rect.go`: `Rect` + hit-testing (point → pane / divider / title-bar handle), table-tested.
- [ ] `internal/layout/resize.go`: divider drag → ratio update, clamped to per-pane minimums.
- [ ] `internal/layout/move.go`: pane drag → drop-zone resolution → leaf re-parent (swap/reorder).
- [ ] `internal/layout/state.go`: tree ⇄ plain data; tolerant decode (drop unknown/invalid pane ids, fall back to default).
- [ ] `cmd/ike`: enable mouse reporting (`tea.WithMouseCellMotion`).
- [ ] `internal/app`: replace hard-coded `explorerWidth`/`JoinHorizontal` with tree-driven `Rects`; render each pane into its rect.
- [ ] `internal/app`: mouse drag state machine — press hit-test, motion update, release commit; resize vs. move dispatch.
- [ ] Persistence wiring: per-project layout store with `Load`/`Save`, save-on-release; safe fallback on missing/stale state.
- [ ] Tests: tiling exactness, hit-testing, resize clamp, move re-parent, state round-trip + tolerant decode, app drag-to-resize/move regression, default-on-stale.
- [ ] Wiki: document the layout tree, mouse drag model, and the layout state store / persistence under `wiki/`.

## Out of scope

- **Creating or destroying splits** (split a pane into two, close a pane) — a
  later pane-manager roadmap; v1 only moves/resizes the existing pane set.
- **Free-floating / detached windows** and tabbed pane groups.
- **Dragging the floating shell** (0035) or the status line.
- **Keyboard-driven resize/move** bindings — a planned additive once 0080 owns
  the keymap (geometry ops here are binding-agnostic and reusable by it).
- **Animations / transitions** during drag.
- Folding layout state into `settings.toml` — layout is runtime state with its
  own store; merging the two is a deliberate non-goal.
