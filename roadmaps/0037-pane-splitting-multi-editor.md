# Roadmap 0037 — Pane Splitting, Multiple Editors & Open-in-New-Pane

Turn IKE's fixed two-pane layout into a **dynamic, splittable workspace**. Today
the root model hard-codes exactly two component instances — one `explorer.Model`
and one `editor.Model` — and `corePanes()` returns the immutable set
`{explorer, editor}`. Roadmap 0036 lifted the geometry into a pure split tree
(`internal/layout`) and made it mouse-draggable, but that tree can only **move**
and **resize** the existing leaves; it never grows or shrinks. This roadmap
delivers the other half of the pane manager 0035/0036 deferred — the
**create/close half**:

- **Split** — split a focused leaf into two, creating a brand-new pane (a new
  editor instance) beside it, via a binding-agnostic op (or a drag-to-edge of the
  *current* pane that spawns rather than relocates).
- **Multiple editors** — the root holds **N** pane instances, each its own
  component with its own file / buffer / cursor, replacing the two hard-coded
  fields with a **pane registry**.
- **Close** — close a focused leaf; its sibling collapses up to take its place,
  mirroring `layout.Move`'s removal but as a first-class op.
- **Open in a new pane** — when opening a file the user can choose to open it in
  a **new split** instead of replacing the current editor's buffer; the intent
  rides the existing open path (`OpenFileMsg` / `host.OpenFileRequest`).
- **Persist** — the per-project store from 0036 grows to save dynamically
  created panes and enough per-leaf identity (kind + which file) to restore them.

It is the broader **pane manager** 0035 named and 0036 began: 0036 operates on a
fixed pane set; **0037 makes the pane set itself dynamic.**

## Prerequisites / Dependencies

- **Roadmap 0036 (Pane Drag):** the direct predecessor. Its `internal/layout`
  split tree, `Compute`/`Rects`/`Move`, hit-testing, and the per-project layout
  **state store** are the foundation. 0037 **extends** that engine (new `Split`
  and `Close` tree ops) and that store (richer per-leaf identity) — it does not
  redesign either. The `internal/app` drag state machine and `renderNode` walk
  are reused; the leaf→component mapping in `renderPane` is the seam that changes
  most.
- **Roadmap 0010 (Foundation):** the root model owns focus, layout, and the
  open-file routing (`openPath`). Today focus is a two-value `focus` enum and
  `openPath` loads into the single `m.editor`. Both generalise here: focus
  becomes "the focused leaf in the tree", and `openPath` consults an
  open-target intent.
- **Roadmap 0035 (Floating Shell):** unchanged and still composited **above** the
  tiled layout. New editor instances are tiled leaves, never floating; the shell
  does not participate in splitting.
- **Roadmap 0040 (Settings):** read-only here. Optional tuning (default split
  orientation, drop-zone thickness, max pane count) reads through `host.Config`
  exactly as 0036 reads `overlay.*`. The layout **state store** reuses 0040's
  discovery/write seam when p√resent (same as 0036); layout remains *runtime UI
  state*, not user-authored config.
- **Roadmap 0080 (Keybindings):** owns the keymap later. Like 0036 did for
  resize/move, this roadmap exposes split / close / focus-move as
  **binding-agnostic ops** on the model (`SplitFocused`, `CloseFocused`,
  `FocusDir`); 0080 binds keys to them. Mouse gestures (drag-to-edge to spawn)
  are additive and reach the same ops.
- **Roadmap 0020 (Plugins):** the open-file contract
  (`explorer.OpenFileMsg`, `host.OpenFileRequest`, `host.API.OpenFile`, plugin
  `FileHandler`) carries the **open-target** intent. The new field is additive
  and defaults to today's behaviour (replace current), so existing plugins and
  the explorer are unaffected unless they opt in.

> **No leaf may be empty.** A `Close` that would remove the last remaining leaf
> is a no-op (the workspace always shows at least one pane), exactly as
> `layout.remove` already refuses to empty the tree.

## Architecture

The central change is that a layout **leaf is no longer a bare global pane id**
(`"explorer"`, `"editor"`). It becomes a **stable pane instance key**, and the
root holds a **registry** mapping that key to a live component instance. The
explorer stays a singleton; editors become many.

```
internal/layout/
  tree.go        EXTEND: Split tree leaf still carries a string Pane (now an instance key, opaque to layout)
  split.go       NEW: Split(root, target, newPane, zone) -> grow a leaf into a split with a fresh leaf
                      Close(root, pane) -> collapse a leaf, sibling takes its place (first-class remove)
  move.go        REUSE: remove/insert already do the collapse + re-parent; split.go shares them
  state.go       EXTEND: nodeData already round-trips arbitrary leaf strings; pane identity (kind+file)
                      moves to a side table keyed by instance key (see Persistence)
  layout_test.go EXTEND
internal/pane/   NEW: the pane registry + instance lifecycle (justified below)
  registry.go    Registry: map[key]*Instance; Add/Get/Close/Focused/Keys; allocates unique keys
  instance.go    Instance: kind (explorer|editor) + the component (explorer.Model | editor.Model) +
                      its advertised context id; Update/View/SetSize/SetFocused dispatch by kind
  target.go      OpenTarget enum (Replace | NewPane | side hint) — the "where to open" intent
  pane_test.go
internal/app/
  app.go         REWRITE focus + pane fields: focus becomes the focused instance key; the two component
                      fields become a *pane.Registry; renderPane maps a leaf key -> registry instance;
                      openPath consults OpenTarget; new SplitFocused/CloseFocused/FocusDir ops
internal/host/
  host.go        EXTEND: OpenFileRequest gains a Target field; API.OpenFile gains a variant / option
internal/explorer/
  explorer.go    EXTEND: OpenFileMsg gains a Target field; a modified open action (e.g. with a modifier)
                      requests NewPane instead of Replace
cmd/ike          unchanged (mouse already enabled by 0036)
```

Why a new `internal/pane` package (not just more fields in `internal/app`):

- The two-field model (`m.explorer`, `m.editor`) does not generalise to N; a map
  of `(key) -> instance` with a focus key is the natural replacement, and it is
  large enough (lifecycle: allocate key, create, focus, close; dispatch by kind)
  to own its own unit tests independent of bubbletea wiring.
- It keeps `internal/app` as the thin **host** (drag state machine, key routing,
  compositing) and pushes instance bookkeeping behind a tested seam, matching
  0036's "pure engine, stateful host" split. `internal/pane` is *almost* pure
  (it holds components but no I/O); `internal/layout` stays fully pure.

Data flow:

```
split op (key / drag-to-edge) ─► app.SplitFocused ─► pane.Registry.Add(editor) -> newKey
                                          └────────► layout.Split(tree, focusedKey, newKey, zone) ─► *Tree
                                                              │
close op (key)               ─► app.CloseFocused  ─► layout.Close(tree, focusedKey) ─► *Tree
                                          └────────► pane.Registry.Close(focusedKey); focus -> sibling

open file (OpenTarget)       ─► openPath ─► Replace: load into focused editor instance
                                          └► NewPane: Registry.Add(editor)+layout.Split, load into it

Rects(tree, viewport) ─► per-key SetSize ─► renderPane(key) = Registry.Get(key).View()
focus key ─► Registry.Get(key).SetFocused(true), all others false
on release / op commit ─► state.Save (tree + per-leaf identity side table, per project)
launch ─► state.Load ─► rebuild Registry from identities ─► *Tree (fallback: default)
```

## Layout model changes

- **`Split(root, target, newPane, zone)`** grows the leaf whose key is `target`
  into a `Split` pairing the existing leaf with a fresh `Leaf{Pane: newPane}`,
  oriented/ordered by `zone` (the same four `Zone` values 0036 already defines).
  This is exactly `move.splitFor` + `insert`, but the inserted leaf is *new*
  rather than *removed-from-elsewhere*. `split.go` reuses `insert`/`splitFor`;
  the only new behaviour is "the leaf comes from the caller, not from a prior
  `remove`."
- **`Close(root, pane)`** is `move.remove` promoted to a public op: detach the
  leaf, its parent split is replaced by its sibling. Closing the **only** leaf
  (root is that leaf) returns the root unchanged and reports `ok=false`, so the
  workspace never empties. The caller then drops the instance from the registry.
- The layout package stays oblivious to *what* a leaf is — `Pane` remains an
  opaque string. The shift from "global pane id" to "instance key" is purely a
  convention on the host side; no layout type changes. This keeps
  `internal/layout` pure and its existing N-pane `Compute`/`Move` untouched.

## Pane registry & focus model

- **Instance keys.** Each leaf carries a unique key. The singleton explorer keeps
  the stable key `"explorer"` (so context resolution and the default tree are
  unchanged). Editors get allocated keys (`"editor"` for the first, then
  `"editor:2"`, `"editor:3"`, … — monotonic, never reused within a session) so
  the tree, the registry, and persistence all agree on identity.
- **Registry lifecycle.** `pane.Registry` owns: `Add(kind) key` (create a
  component instance, allocate a key), `Get(key) *Instance`, `Close(key)`,
  `Focused() key` / `SetFocused(key)`, and `Keys()` for iteration. Each
  `Instance` knows its kind and dispatches `Update`/`View`/`SetSize`/`SetFocused`
  and its advertised **context id** (explorer panes → `ctxExplorer`, editor panes
  → `ctxEditor`) so `focusContext()` keeps working for command/keymap resolution.
- **Focus is the focused leaf.** The `focus` enum is replaced by a focused
  **instance key** held on the model. `toggleFocus` (tab) becomes "cycle to the
  next leaf in tree order"; `FocusDir(dir)` (a binding-agnostic op for 0080)
  moves focus to the spatially adjacent leaf using the computed `Rects`. Mouse
  click in a pane interior focuses that leaf (additive). `syncFocus` iterates the
  registry, marking exactly the focused instance.
- **Routing.** `routeKey` and `editorCapturing` consult the focused instance's
  kind instead of the two-value enum; an editor instance still captures text in
  insert/command mode and shadows global single-letter keys.

## Split / close / focus ops (binding-agnostic)

Mirroring 0036's treatment of resize/move, these are plain model methods so 0080
can bind any keys and the mouse can reach them too:

- **`SplitFocused(zone)`** — add a new editor instance and `layout.Split` the
  focused leaf toward `zone`; focus moves to the new pane. Default zone is a
  configured orientation (e.g. split-right).
- **`CloseFocused()`** — `layout.Close` the focused leaf, drop its instance,
  focus the sibling; no-op on the last leaf. An editor's existing `CloseMsg`
  (today resets the single editor) is rerouted to close *that leaf's* pane.
- **`FocusDir(dir)` / focus-cycle** — move focus between leaves without changing
  structure.
- **Mouse spawn (additive).** Dragging a pane's title bar to the **edge of the
  same pane** (a thin drop-zone band) spawns a split there instead of relocating;
  dragging onto *another* pane keeps 0036's move/swap. The drag state machine
  gains a "spawn vs. move" decision at release based on the drop target.

## Open-in-new-pane intent

The "where to open" intent is an explicit, additive field on the open path so it
threads cleanly from any source to `openPath`:

- **`pane.OpenTarget`** enum: `Replace` (today's behaviour — load into the
  focused editor, or the most-recent editor if the explorer is focused),
  `NewPane` (split off a fresh editor and load there), with an optional `Zone`
  hint for which side.
- **Explorer.** `explorer.OpenFileMsg` gains `Target`. The plain open action
  (`enter` / `l`) stays `Replace`; a **modified** open action (e.g. a distinct
  key, later owned by 0080) emits `NewPane`. The explorer sets the field; it does
  not decide layout.
- **Host / plugins.** `host.OpenFileRequest` gains `Target`. `API.OpenFile` keeps
  its current signature (defaults to `Replace`) and gains a sibling
  `OpenFileIn(path, target)` (or an options variant) so a `FileHandler` or
  command can request a new pane. Defaulting to `Replace` keeps every existing
  plugin and the in-process registry source-compatible.
- **`openPath`** reads `Target`: `Replace` loads into the focused/active editor
  instance (unchanged path, incl. `FileHandler` resolution and
  `EventFileOpened` hooks); `NewPane` first `Add`s an editor + `layout.Split`s,
  then loads into the new instance. Handler-claimed files still get first refusal
  regardless of target.

## Persistence

Extends 0036's per-project layout store; the tree wire format is already
tolerant of arbitrary leaf strings, so the structure round-trips for free. What
is new is **per-leaf identity** so a restored editor reopens its file:

- **Identity side table.** Alongside the encoded tree, the store saves a map
  `instanceKey -> {kind, path}` (explorer has no path; an editor records the file
  it held, or empty for a scratch buffer). On load, the tree is decoded first,
  then each editor leaf's instance is recreated and its file reloaded.
- **Validation against the live pane set generalises.** 0036's `Decode` requires
  the saved leaves to *exactly* match `corePanes()`. With dynamic panes that
  invariant changes: the singleton explorer must still be present exactly once,
  but editor keys are validated structurally (well-formed, unique) and their
  files are reloaded **best-effort**.
- **Tolerant decode implications (call-outs):**
  - A saved editor leaf whose **file no longer exists** restores as an *empty*
    editor at that leaf (the split is preserved, the buffer is blank) rather than
    dropping the pane or crashing.
  - A saved tree **missing the explorer**, with a **duplicate explorer**, or
    **structurally malformed**, falls back to the 0036 default tree — the same
    "never hide a pane, never crash" guarantee.
  - **Unknown leaf kinds** (a future pane type the running build doesn't know)
    are dropped and their split collapses, the workspace staying coherent.
- **Save** stays debounced to op/drag commit (split, close, move, resize,
  open-in-new-pane), not per motion frame.

## Design rules

- **Extend, don't redesign.** Build on 0036's `internal/layout` and store;
  `Split`/`Close` reuse `insert`/`remove`; the layout package stays pure and
  leaf-as-opaque-string.
- **One instance per leaf.** Every leaf maps to exactly one live component; the
  registry is the single source of instance truth. The explorer is a singleton;
  editors are many.
- **Never empty the workspace.** Closing the last leaf is a no-op; there is
  always a focused leaf.
- **Intent is explicit and additive.** Open-target rides one new field that
  defaults to today's behaviour, so the explorer, host API, and plugin
  `FileHandler` contract stay backward-compatible.
- **Ops are binding-agnostic.** Split / close / focus-move are model methods;
  0080 binds keys, the mouse reaches the same methods. No action is mouse-only.
- **Restore safely.** Stale or partially-invalid layout never crashes or hides a
  pane; missing files restore as empty editors; structural breakage falls back to
  the default tree.
- **Overlays stay on top.** The floating shell (0035) and status line composite
  after tiling; new panes are tiled, never floating.

## Milestones

- [x] `internal/layout/split.go`: `Split(root, target, newPane, zone)` grows a leaf into a split with a fresh leaf, reusing `insert`/`splitFor`.
- [x] `internal/layout/split.go`: `Close(root, pane)` first-class leaf removal (sibling collapses up; no-op on the last leaf), promoting `remove`.
- [x] `internal/pane/instance.go`: `Instance` wrapping an explorer/editor component, dispatching `Update`/`View`/`SetSize`/`SetFocused` and advertising its context id by kind.
- [x] `internal/pane/registry.go`: `Registry` with `Add`/`Get`/`Close`/`Focused`/`SetFocused`/`Keys` and monotonic unique key allocation (explorer singleton keeps key `"explorer"`).
- [x] `internal/pane/target.go`: `OpenTarget` enum (`Replace`/`NewPane`) + optional `Zone` hint.
- [x] `internal/app`: replace `m.explorer`/`m.editor`/`focus` with `*pane.Registry` + focused-key; `renderPane` maps a leaf key to its instance; `syncFocus`/`routeKey`/`editorCapturing`/`focusContext` consult the registry.
- [x] `internal/app`: `SplitFocused(zone)`, `CloseFocused()`, `FocusDir(dir)` and focus-cycle (tab) binding-agnostic ops; reroute `editor.CloseMsg` to close that leaf.
- [x] `internal/app`: mouse spawn — drag-to-own-edge spawns a split (vs. 0036 move-to-other-pane); release-time spawn-vs-move decision; click-to-focus.
- [x] `internal/host`: add `Target` to `OpenFileRequest`; add `OpenFileIn(path, target)` (or options variant) to `API`, defaulting `OpenFile` to `Replace`.
- [x] `internal/explorer`: add `Target` to `OpenFileMsg`; a modified open action emits `NewPane` while the plain open stays `Replace`.
- [x] `internal/app`: `openPath` honours `OpenTarget` — `Replace` into the active editor, `NewPane` via `Add`+`layout.Split`; `FileHandler` resolution and `EventFileOpened` hooks unchanged.
- [x] `internal/layout/state.go` + store: persist the per-leaf identity side table (kind + path); generalise validation (explorer singleton; editor keys structural; best-effort file reload).
- [x] Persistence: restore — rebuild the registry from identities, reload editor files best-effort (missing file → empty editor), fall back to the default tree on structural breakage.
- [x] Tests: `layout` split/close (incl. last-leaf no-op); `pane` registry lifecycle + key allocation + close-refocus; `app` split/close/focus-move regression; open-in-new-pane (Replace vs NewPane); state round-trip with multi-editor + missing-file restore + tolerant decode.
- [x] Wiki: document the pane registry / instance lifecycle, the split/close layout ops, the focused-leaf focus model, and open-in-new-pane intent + persistence under `wiki/`; refresh timestamps and add a `log.md` entry.

## Out of scope

- **Free-floating / detached OS windows** — panes are tiled leaves only; floating
  is the 0035 shell's job.
- **Tabbed pane groups within a single leaf** (multiple buffers stacked behind
  one pane with a tab strip) — a separate feature that layers on top of this
  registry; this roadmap is one instance per leaf.
- **Keyboard binding *choices*** for split/close/focus-move — 0080 owns the
  keymap; the ops here are binding-agnostic.
- **Drag animations / transitions** when spawning or closing a pane.
- **Cross-pane buffer sharing / split views of the *same* file** (two leaves
  editing one shared buffer) — each editor instance owns an independent buffer
  here.
- **Maximise / zoom a pane**, save/restore named layout presets, and per-pane
  pinning — later pane-manager additives.
- **Folding layout/identity state into `settings.toml`** — it stays runtime state
  in the dedicated store, consistent with 0036.
