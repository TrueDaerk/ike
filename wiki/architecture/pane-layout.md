---
type: concept
title: Pane Layout & Drag
description: Pure split-tree layout model driven by mouse drag — pane-edge resize and title-bar move/swap — plus a sticky keyboard resize mode (#2150) stepping the same dividers with hjkl/arrows, plus numbered pane chrome with focus-by-number chords (#2407), with per-project geometry persisted in a dedicated state store and named user-scoped saved layouts that are the whole truth on apply (#2042), tools and multi-tool tab hosts included; slot templates (#1897) only govern runtime tool opens.
resource: internal/layout/tree.go
tags: [architecture, layout, panes, mouse, drag, resize, split, close, keyboard, focus, numbers, persistence, bubbletea]
timestamp: 2026-09-03T12:00:00Z
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
  between two children at a ratio in `(0,1)`: `Horizontal` puts A left / B right;
  `Vertical` stacks A top / B bottom. The children tile the region exactly with
  no gutter between them (#761) — each pane's own rounded border forms the
  visible seam.
- `Default(width, explorerCols)` reproduces the historical layout: a horizontal
  split with the explorer on the left at roughly `explorerCols` columns.
- `Compute(root, viewport)` walks the tree and returns a `Layout`: a map of every
  leaf's integer `Rect` plus the live `Divider`s. Children always tile their
  parent exactly — rounding is handled so there are no gaps or overlaps.
  A `Divider` is no longer a reserved gutter cell but a **two-cell resize hit
  band** over the pane borders meeting at the children's shared edge (A's
  right/bottom border plus B's left/top border, #761). `Split.Children(rect)` is
  the shared seam that both `Compute` and the renderer use, so geometry and
  drawing never diverge.
- **Slot templates** (`slots.go`, #1897): `ParseTemplate` turns an ASCII grid
  of named slot rectangles (with a reserved `E` editor region) into a slot
  tree by guillotine-cut decomposition, every ratio a cell fraction; templates
  that cannot be sliced (pinwheels) are rejected. `Template.BuildTree` prunes
  the slot tree to the currently open slots — a closed slot's space is
  absorbed by its surviving sibling — substitutes resident pane ids for slot
  leaves and grafts the live editor subtree at `E`; `RemoveLeaves` is the
  set-generalized `Close` the host uses to peel slotted panes off first. The
  host-side semantics live in
  [Custom TUI Tool Panes → Slot templates](./tool-panes.md).

## Mouse drag model

`internal/app` owns the only mutable drag state and all I/O. The program enables
mouse reporting via `tea.WithMouseCellMotion` in `cmd/ike`; the root model's
`tea.MouseMsg` branch runs a small state machine:

- **Press** hit-tests the cached `Layout` (`Layout.Hit`). The shared pane edge —
  the two border cells straddling a split boundary — starts a **resize** (#761);
  a pane's title bar starts a **move**. The resize band wins over the title band
  where they overlap (a pane's top border row on a split boundary), so the move
  handle there is the title *text* row just inside the border. A move (or
  tab drag) stays latent until the pointer travels an **engage threshold** from
  the press cell — one row vertically or `moveEngageCols` columns sideways
  (#559): before that, no move feedback (status hint, source marker, ghost)
  renders and a release is a plain click that only focuses.
- **Right press on the title band (#1128)** opens the pane context menu in the
  shared floating shell (`menu.Context`, #1020) instead of starting a move:
  the pane is focused first, then **Split Right** / **Split Down**
  (`pane.splitRight` / `pane.splitDown`), **Maximize** (`pane.maximize`) and
  **Close Pane** (`pane.close`, new — closes the leaf with *all* its tabs
  behind the unsaved-changes guard; `closePane` is the shared unguarded close
  the dead tool pane's ✕ uses too). A right press on a tab-bar *segment*
  opens the tab menu instead (see
  [Editor Tabs](/architecture/editor-tabs.md)).
- **Motion** during a resize calls `Divider.ResizeTo`, which updates the owning
  split's ratio, **clamped** so neither child drops below a minimum cell size — a
  pane can never be dragged to zero. Motion events are folded by the input
  coalescer (#602) — only the latest position per adaptive flush applies, so a
  drag costs at most one relayout per rendered frame — and terminal panes
  debounce the expensive PTY/emulator resize (leading + trailing, #804; see
  `/architecture/terminal.md`), so a divider drag over a terminal no longer
  triggers a SIGWINCH redraw storm in the child per step.
- **Outer-edge docking (#811).** During a whole-pane move, the workspace
  body's outermost row/column (`dockBand` = 1 cell) is a dock strip: releasing
  there re-roots the tree via `layout.Dock` so the pane spans the **full
  width** (outer top/bottom) or **full height** (outer left/right). Its share
  along the dock axis keeps the pane's current extent when that is already
  modest, capped at `dockMaxShare` (⅓ of the workspace) — a full-height pane
  docked to the bottom becomes a tool-window-sized strip, not a 90% slab.
  Hovering the strip previews the full-span target as a ghost plus a
  `dock <edge> (full …)` status hint. One cell inside the strip, the normal
  pane-relative zones (including self-edge spawn) apply unchanged; corners
  prefer top/bottom. Tab drags never dock — they carry a document, not a pane.
  The same edge slots double as configured **home positions** for tool panes
  (#1889): `layout.DockNew` is `Dock`'s create counterpart, attaching a
  brand-new leaf full-span against an edge, and `layout.EdgeLeaf` probes
  which lone leaf currently occupies an edge slot (following splits of the
  dock orientation toward that edge; a shared or subdivided edge counts as
  free). See [Tool Panes](/architecture/tool-panes.md) for the open
  semantics.
- **In-tree regions (`internal/layout/region.go`, #2191).** Docking asks about
  the workspace's outer edges; placing a pane inside a nested layout asks about
  a leaf's own neighbourhood. `layout.Hops(root, leaf)` walks the leaf's
  ancestor path **innermost first**, yielding each ancestor split's other
  subtree plus the side it occupies relative to the leaf (`layout.Opposite`
  flips a side) and the split's ratio; `layout.EdgeLeafIn(node, zone)` names
  the leaf pinned against a *region's* own edge, descending all the way — a
  strip already subdivided across the dock axis still yields the leaf on
  zone's side, where `EdgeLeaf` would report the slot free. The tool-region
  placement (#2191) and the saved-layout editor anchor (`anchorFromLayout`,
  #1989) both ride these.
- **Release** during a move resolves the drop target and `DropZone`
  (left/right/top/bottom of the target pane), then `layout.Move` re-parents the
  dragged leaf — swapping order or re-orienting the split. v1 only relocates the
  existing pane set; it never creates or destroys splits. Release commits and
  persists.

One gesture is active at a time. While any layer of the floating stack
(Roadmap 0035, #1237) is open, the drag machine is inert: mouse input routes to
the topmost floating pane instead (outside-click close, border resize — see
[Floating Shell](/architecture/floating-shell.md)); floating panes are
composited above the tiling and are not draggable. Wheel events are ignored by
the drag machine.

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

**Center merge zone (#318).** During a move or tab drag a **tab-capable**
target — an editor pane, a **terminal/tool pane** (#836) or a **viewer pane**
(#1778) — whose drag carries tab content shows five zones, resolved by
`layout.DropZoneWithCenter`: the outer `CenterBand` (30%) of either axis is
the four edge zones (split/relocate exactly as before), the interior is
`ZoneCenter`, which **merges as tab** JetBrains-style. A whole-pane title
drag released there moves every file of the source editor into the target's
tab list (`openInTab` dedupes onto existing tabs) and closes the emptied
source pane (`mergePaneTabs`); a tab drag released there joins the target's
tab list with just that file. A whole-pane drag of a **terminal pane** also
shows the center zone (#708): releasing there moves the live shell session
into the target's tab list as a terminal tab — the model is detached via
`Instance.DetachTerminal` so closing the vacated pane does not end the
session (`adoptTerminalPane`); edge drops keep the plain relocate.

A **terminal, tool or viewer target** converts on the first center drop (#836,
#1778): `Instance.ConvertToTabHost` flips the pane to an editor-kind tab host
with its running session or live viewer content as the first tab (no restart,
no reload) — the pane kind describes the initial content, not the tab
capability (`canHostTabs`/`ensureTabHost` in `internal/app`). Two terminals can
stack as tabs in one panel this way, or a tool and a file, or a preview and the
file it renders. Drags with nothing to merge — an explorer pane or an empty
editor — keep the four-zone relocate behaviour everywhere.

**Universal tabs (#1778).** Which kinds take part is one predicate,
`pane.KindTabbable`: editors and terminals natively, plus the viewer kinds
(markdown preview, image, diff, archive, data viewer). The **explorer**
and the **singleton tool windows** (VCS, Debug, Problems, Structure, Usages,
Breakpoints, **HTTP**) keep their fixed toggle-driven roles and stay
edge-only targets, as does the merge view, whose conflict workflow is
session-bound — their drags show edge zones only, never a silent no-op
center drop. The HTTP response viewer left the tabbable set in #2042: in the
layout model it is a **tool with a fixed position**, and nesting it as a
content tab made every later save/apply treat the response pane as anonymous
editor content (a legacy layout.json with a nested `http` tab restores
without that tab — the viewer restores empty anyway). A whole
viewer pane dropped in a center zone hands its live content over
(`dragCarriesContent` → `adoptContentPane`); the per-drag capability check is
the kind-agnostic `dragCarriesTab` (files, terminal session or viewer content)
that replaced the old `dragCarriesFiles`/`dragCarriesTerminal` pair.

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
editor holding just that file; a drop on a **non-tab-capable pane's edge
zone** (e.g. the explorer) likewise splits that pane and opens the file in
the fresh editor leaf (#317). A terminal/tool pane's interior now merges via
the conversion above (#836); only a non-tab-capable pane's interior stays a
no-op, its drag feedback (zone arrow, ghost, status hint) signalling a
target only in an edge zone. The ghost for a tab drag is labelled with the
dragged file's basename (a terminal tab uses its tab title).

**Terminal-tab drag (#707).** A grabbed **terminal tab** follows the same
release rules (`commitTerminalTabMove`): another editor's center zone moves
the live session into that tab list (`DetachTerminalTab` + `AddTerminalTab`
— no shell restart); every edge zone — an editor's, a non-editor pane's, or
the source pane's own — splits the session off as its own terminal pane
(`splitTerminalTabTo` via `Registry.AddTerminalPaneFrom`). The split pane
keeps its original session routing key under a fresh pane key, so the shell's
`ExitedMsg` is resolved through `terminalPaneForSession` rather than by pane
key alone.

**Content-tab drag (#1778).** A grabbed **content tab** (preview, diff, image,
archive, data viewer, HTTP response) releases by the same rules
(`commitContentTabMove`): another tab host's center zone moves the live nested
instance into that tab list (`DetachContentTab` + `AddContentTab` — no reload,
the target converting first when needed); every edge zone — a host's, a
non-host pane's (#317 semantics) or the source pane's own — splits the content
off as its own viewer pane again (`splitContentTabTo` via
`Registry.AddContentPaneFrom`). A refused split or re-registration re-adopts
the tab rather than dropping it, and a pinned tab keeps its pin across the move
(#1172).

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

**Where a viewer lands.** The viewer opens — data (`openDataPane`), image
(`openImagePreview`), archive (`openArchivePane`) — do not split the *focused*
leaf, because a file is usually opened from the explorer and splitting there
pushes the viewer to the far side of the window. `viewerSplitTarget`
(`internal/app/app.go`) picks the target the way `fileEditorKey` does for a
file open: the focused pane when it hosts content (editor, terminal, another
viewer), else `m.recentEditor` — the pane focused before the explorer took
over — else the first content leaf in walk order. The explorer and the
singleton tool windows (VCS, Debug, Problems, Structure, Usages, HTTP,
Breakpoints) never qualify; only when nothing else is left does the focused
leaf serve as the fallback, so a tool-windows-only workspace still gets its
pane (#1779). Re-opening a path already shown in a viewer pane just refocuses
it and splits nothing.

A viewer opened **from the palette** skips the split entirely: it nests as a
content tab in the focused pane (#1825), the same place a plain file picked in
the palette lands. `openPathFocused` records that pane in `m.viewerTabHost`
and the viewer open consumes it (`takeViewerTabHost` / `openContentTab`,
`internal/app/panecontent.go`); a focused pane that cannot host tabs — the
explorer, a tool window — falls back to `viewerSplitTarget`.

The **explorer's default open** (enter, `l`, double-click) skips it too, but
targets the editor rather than the focused pane: `openPathInEditor` records
what `fileEditorKey` resolves, so *every* file the explorer opens — plain or
viewer-handled — becomes a tab in the last-focused editor pane and never a
split beside it (#1851). With no editor pane left, one is spawned to host the
tab. Only the explorer's explicit **open in split** (`o`, `OpenFileMsg{NewPane:
true}`) still goes through `viewerSplitTarget`.

All of it is **one** helper (#2463): `openViewerPane(kind, id, matches, add)`
in `internal/app/panelwiring.go` runs the refocus check, the content-tab nest
and the split in that order, and returns the fresh pane's `Init` — the command
the viewers that load in the background (#1795) ride on. Its tail,
`splitViewerPane(add)`, is the split-focus-persist-Init sequence on its own,
which the remote browser (#1997) uses because it dedups by connection rather
than by path. The matching **result routing** shares `routeResult(msg, match,
discard)`: a background result reaches the pane that asked for it, dedicated or
tab-nested, and an unroutable one is `Discard()`ed rather than dropped, so the
database handle or connection it carries is released.

**Where a spawned editor lands (#1989).** `spawnEditor` anchors at the pane
`fileEditorKey` resolves — never at a pane whose tabs are all tool sessions (a
**tool-tab host**, `toolTabHost`): that pane is the layout's tools area, not
an editor slot. When *no* live pane edits files (last editor closed, only
explorer/terminals/tool hosts remain), the **designated default layout**
decides the position (`editorSlotAnchor`): its first real editor slot is
located, the path to the root is walked inside-out, and the first layout
sibling with a live counterpart (matched by identity kind — singletons by
fixed key, tools by name, tool hosts by shape) becomes the split anchor, on
the side the slot occupied and with the saved split share — so with all
editors closed, opening a file recreates the editor in its original layout
slot. Without a usable default layout the built-in explorer+editor
arrangement anchors at the explorer; the focused leaf stays the last resort.

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
seam). Resize feedback is the shared pane edge tracking the cursor in real time
as the ratio updates per motion frame.

## Pane numbers & focus by number (#2407)

Pane focus is the most frequent layout event, and before this the keyboard
could only *cycle* (`pane.switcher`, `ctrl+tab`): reaching the third of four
panes meant pressing the chord until it happened, which is why the telemetry
found most focus changes going through the mouse. The panes are numbered and
the number is addressable.

- **Numbering is derived, never stored.** `paneNumberOrder`
  (`internal/app/panenumbers.go`) sorts the *visible* panes — the keys of
  `m.lay.Panes` — by their computed rectangle in reading order: top row first,
  left to right within a row. Reading over the rectangles rather than over the
  tree keeps the numbers geometric, so nested splits stay numbered the way
  they look. Because it reads `m.lay`, every layout change renumbers with no
  invalidation step: a split, move, close or restore is visible in the badges
  on the next frame, a zoomed pane (#358) is the only numbered one, and a tool
  window counts exactly while it is on screen. The popup terminal and the
  floating panels are not layout leaves and never take a number.
- **The badge is chrome.** `renderPaneBox` prefixes the title row with
  `paneNumberBadge` — lazygit's `[1] EDITOR` — in the focus border colour for
  the focused pane and the dim border colour for the rest. When the pane holds
  several tabs the tab bar takes the title row over; it is measured against
  the width the badge leaves, so the box still fills its rect exactly. Only
  the first nine panes carry a badge: those are the ones a chord can address,
  and a number nobody can press would be a lie.
- **Commands.** `pane.focus1`…`pane.focus9` (`PaneFocusIndexMsg`) focus the
  pane carrying that number; out of range is a no-op **with a notification**,
  never a silent dead chord (#275). `pane.focusByIndex` ("Focus Pane by
  Number…") is the typed flavour in a shell prompt, for the panes past nine
  and for terminals the digit chords do not reach.
- **`ctrl+1`…`ctrl+9`, macOS only.** The chords are delivered — plain ctrl
  chords, free of macOS system shortcuts — and deliberately *not* `cmd+digit`,
  which is the JetBrains tool-window numbering (`cmd+1` toggles the project
  tree). They live in `keymap.darwinRows`: off macOS the Cmd→Ctrl fold lands
  `explorer.toggle`, `nav.pins` and `vcs.panel` on exactly these chords, so
  there `pane.focusByIndex` is the doorway, recorded in the keybind audit
  ledger.
- **`layout.pane_numbers`** (Settings → Appearance) is `on` (default), `off`,
  or `focus-only`. `focus-only` draws the badges only while the *which-pane
  hint* is up: a keyboard pane switch — the switcher, a focus-by-number chord,
  the prompt — raises it and schedules its own expiry message, generation-
  tagged so a faster second switch outruns the older timer. The numbers are
  therefore on screen exactly while panes are being switched.

## Keyboard resize mode (#2150)

Resizing by keyboard used to mean nothing at all — the divider drag was the
only route. `pane.resizeMode` (`ctrl+alt+r`, pane context menu → **Resize…**,
palette) arms a **sticky mode** instead of spending one chord per step:

- **Enter once.** The command sets `m.resizeMode` (`internal/app/resizemode.go`).
  It refuses — with a toast, not a silently armed mode — when there is no
  focused pane, while a pane is maximized, or when the workspace has no
  divider at all: every key would be a no-op there.
- **Step repeatedly.** While armed, the mode is the *first* consumer in the
  `tea.KeyPressMsg` branch, ahead of overlays, the keymap layer and pane
  routing. `h`/`j`/`k`/`l` and the arrow keys move the focused pane's edge one
  cell per press — key repeat covers long distances, which is the point of a
  sticky mode. Direction means **where the edge travels**: `l` moves it right.
  A pane that owns its trailing edge (right/bottom) therefore *grows* with
  `l`/`j` and shrinks with `h`/`k`; a pane that only owns its leading edge —
  the rightmost or bottom pane — reads the other way round, which keeps both
  grow and shrink reachable with the same four keys.
- **Which divider.** `Layout.EdgeDivider(pane, zone)` (`internal/layout/resize.go`)
  picks the split: the divider band sitting on the pane's trailing edge along
  the requested axis and spanning the pane completely, else the leading-edge
  one. Where several bands meet the same edge the **innermost** (smallest
  cross extent) wins — the pane's nearest enclosing split. The step itself is
  `Divider.ResizeStep(delta)`, the keyboard sibling of the drag's
  `Divider.ResizeTo`: it reads the ratio back into a cell offset so repeated
  single-cell steps accumulate exactly, and clamps against the same `minCell`,
  so a pane can no more be keyed to zero than dragged there.
- **Leave.** `esc`, `enter` or `q` disarm the mode and persist the tree with
  `saveLayout`, exactly what a released drag commits. **Every other key is
  inert** — it neither resizes nor reaches an editor, so a mode left armed can
  never cause a stray edit.
- **Visible while active.** The status line replaces its segments with a
  `RESIZE <pane>  hjkl / arrows move the edge · esc to finish` banner in the
  drop-target colour, the same slot an engaged move drag uses (see
  [Status Line](/architecture/status-line.md)).

The chord is a single modifier chord, not a `cmd+k` sequence — the sequence
family is capped at five (#711) — and sits on the `terminalGlobalCommands`
allowlist, so the mode also arms from a focused terminal or tool pane.

## Maximize / zoom (Roadmap 0290, #358)

`pane.maximize` (`cmd+k z`, View menu, palette) is a tmux-style zoom toggle:
the focused pane renders alone over the whole body rect while the split tree
stays untouched underneath. Implementation is one substitution point — while
zoomed, `layout()` builds `m.lay` as a single-pane Layout with no dividers,
and since `m.lay` is the sole source of pane geometry (rendering, mouse
hit-testing, focus navigation), no other subsystem branches on zoom; `render`
draws the one pane via `renderPane` instead of walking the tree. Any change
to the tree's **leaf set** (split, close, drag relocation) auto-unzooms: the
zoom records a sorted leaf signature and `layout()` — the choke point every
mutation already runs through — drops the zoom when the signature no longer
matches or the pane vanished. Resizes keep the leaf set, so a zoom survives
a terminal resize. Zoom is deliberately not persisted; a restart restores
unzoomed.

**Hide All Tool Windows (#791).** `window.hideAllTools` (cmd+shift+F12,
JetBrains verbatim) removes every visible tool leaf — explorer, terminals,
VCS and debug panels — after deep-copying the tree (`layout.Clone`);
instances stay registered, so terminal sessions keep running. The second
press restores the saved tree verbatim when nothing diverged (same leaf
signature, every hidden tool still registered and still hidden); otherwise
each still-hidden tool re-attaches at its conventional side (explorer as
the outer-left column, others as a bottom strip). Editor panes, splits and
focus are untouched — the complement of zen mode below.

**Zen mode (#359, #934).** `view.zenMode` (`ctrl+alt+f`, View menu, palette)
layers chrome-hiding on the zoom: the **focused pane** — editor, terminal,
or tool pane alike — is maximized and the tab bar and status line disappear
— the status row joins the body (`bodyRect`), the tab bar yields to the
plain title (`tabBar`), and the ex command line is unaffected (it renders
inside the editor pane). Leaving zen restores the chrome; the zoom survives
only when that same pane was already manually zoomed before zen. Tree mutations drop zen exactly like they drop
the zoom (one flag cleared in the same `layout()` check); zen is not
persisted either. The chord sits on the terminal global-command allowlist
(`terminalGlobalCommands`, #934), so it toggles zen from a focused terminal
or tool TUI pane instead of reaching the shell — and the same chord leaves
zen again while that pane keeps focus.

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
  explorer, everything else → a file-less editor). A tab host holding
  **nothing but tool tabs** saves as kind `tools` (#1989) so editor placement
  can tell it from a real editor slot; older files with the pre-#1989
  `editor`+`tools` shape restore identically (tolerant reader), and the next
  save migrates them.
- **Tolerant restore** (`internal/app`): the explorer must be present exactly
  once and every other leaf must be a well-formed editor key, else the default
  layout is rebuilt. A saved editor whose **file no longer exists** restores as an
  *empty* editor at that leaf — the split is preserved, never dropped.
- Save is **debounced to op/drag commit** (split, close, move, resize,
  open-in-new-pane), never written per motion frame.

## Saved window layouts (#1175)

`internal/app/layouts.go` + `layouts_ui.go` add JetBrains' **Window Layouts**:
named, user-scoped snapshots of the split tree. The mental model since #2042
is one sentence: **what you see is what is saved, and what is saved comes
back exactly** — a layout covers editor areas *and* the tool arrangement
(positions, dedicated tool panes, multi-tool tab groups, singleton panels),
and applying it reproduces that arrangement without any competing
slot-template rewrite.

- A **snapshot** is the tree with canonically re-keyed leaves plus a
  **kind-only** identity per leaf — no paths, tab lists or revisions. Content
  panes (editors, markdown previews, diff viewers, tab hosts) all become
  anonymous *editor slots*; a tool pane keeps only its tool name (#741) so
  apply can restart the program; the singleton panels keep their fixed keys.
- The store is `layouts.json` in the **user** layer (`~/.ike/`, or
  `IKE_CONFIG_DIR` like every other state file) — layouts are cross-project
  preference, unlike the per-project `layout.json`. Schema: named
  `persistedLayout` snapshots plus a `default` marker.
- **Commands:** `window.saveLayout` first opens a **pane-selection mini-map**
  (#1568) — a miniature rendering of the split tree filling the floating
  shell's content width (a custom `ui.Content` receives the width budget,
  #1570); hjkl/arrows move spatially between panes (the `focusTarget`
  scoring), space or a **left click on a cell** toggles, `a`/`n` select
  all/none, enter continues to the name prompt (save-as pattern; an existing
  name asks for a confirming second enter). Selection reads by **color**:
  pinned cells fill with the theme's selection colors plus a ✓, deselected
  ones render dim, the highlighted cell has an accent double border. A
  configured tool pane (#741) labels with its tool name instead of TERMINAL.
  Click mapping goes through `Floating.ContentOrigin`/`ScrollOffset` and
  `layout.Layout.PaneAt` over the geometry of the last render.
  `window.layouts` opens a locked palette picker — enter applies,
  shift+delete deletes in place (#1113's aux convention), the default row
  carries a `default` chip. `window.setDefaultLayout` opens the same picker
  with enter marking the default instead. `window.restoreLayout`
  (**shift+F12**, JetBrains' Restore Default Layout) re-applies the default —
  the built-in explorer+editor pair when none is designated.
- **Apply** re-shapes the **active workspace only** (parked workspaces, #777,
  are untouched) and never closes files: live content panes re-slot into the
  layout's editor slots in order; extra slots become scratch editors.
  Singleton tool panels absent from the layout lose their leaf but **stay
  registered** (the hide-all-tools precedent) — their toggles resurface them.
  Running terminals are never killed, and **no leftover pane collapses into a
  tab** (#1577): content panes and terminals the slots did not consume keep
  their own panes and graft into the layout's flexible region — the explicit
  placeholder of a selective layout, or an **implicit** one at the host slot
  of a full layout (`graftImplicit`: last editor slot, falling back to the
  last plain shell slot, then any terminal slot, then the last leaf). The
  host slot's pane joins the leftovers in their **pre-apply relative
  arrangement**; a fresh host instance instead splits the region 50/50 along
  its longer axis. A tool pane is never a merge target and is never converted
  to a tab host by an apply (the pre-#1577 behavior merged surplus shells as
  tabs into the last terminal slot, tool panes included). Plain terminal
  slots reuse live shells in order, then a live shell hosted as a tab
  detaches into the slot (#2124), then a fresh one spawns; per-project panels
  (problems, usages, VCS, debug, structure) restore empty exactly as they do
  on project restore.
- **A live tool is always reused, never duplicated (#2124):** a slot demanding
  a tool kind adopts a live session of that tool **across pane shapes** before
  restarting one (`adoptToolSession`). A dedicated `tool` slot drains the
  queue of dedicated tool panes first, then detaches a matching tool tab out
  of a live tab host or content pane and wraps it as the slot's dedicated pane
  (a host drained of its sole tab closes). Symmetrically, a `tools` host slot
  whose saved tool has no live tab (`restoreMissingToolTabs` /
  `restartToolTabs`) adopts a live dedicated pane's session — or a tab from
  another queued pane — as the tab before falling back to a restart. Before
  #2124 each slot kind only matched its own pane shape, so a tool open in the
  "wrong" shape (e.g. tab-hosted after a home-dock open) grafted into the
  flexible region while the slot restarted a second instance.
- **Selective layouts (#1568):** deselecting panes in the save step stores
  only the selected ones. The deselected leaves are pruned from the snapshot
  tree and the **largest deselected region** survives as a single flexible
  placeholder leaf (`flex`, kind-only identity `{Kind: "flex"}` — the key
  never collides with a registry key). With everything selected the snapshot
  is the full tree, exactly as before. On **apply**, the pinned slots resolve
  as usual, but nothing merges: every live pane the slots did not consume
  keeps its own pane and the whole group **grafts into the placeholder
  position preserving its pre-apply relative arrangement** (the live tree
  cloned, consumed leaves collapsed away). With nothing left over the region
  becomes one scratch editor. At **startup** (default-layout materialization)
  a placeholder has no live panes to graft and materializes as one scratch
  editor slot.
- **Tool tabs in snapshots (#1277, #1989, #2042):** a tab host (#836) whose
  tabs are exactly one tool session and no file-backed editors snapshots as a
  dedicated `tool` slot; a **pure multi-tool host** (tool tabs only, no
  editor or content tabs) snapshots as kind `tools` with the tool names in
  tab order. On apply a `tools` slot re-slots the live tool host whose tab
  composition **best matches the saved tool list** (`takeBestHost`: exact
  multiset match first, then largest overlap; a host sharing no tool is
  never adopted, so two tool areas cannot swap places), restores any saved
  tool missing from the matched host's tabs in place
  (`restoreMissingToolTabs` — a parked global session re-attaches, anything
  else restarts; surplus live tabs are kept, an apply never kills a
  session), or restarts all saved tools as tabs of a fresh host. A `tools`
  slot **never consumes an editor slot's content pane** (`implicitHostSlot`
  skips tool hosts too, so leftovers never graft into the tools area). A
  mixed host — files alongside tools — stays an editor slot that keeps the
  tool names, and a fresh editor slot restarts them as tabs on apply (the
  startup restore of `id.Tools` already did the same); a legacy snapshot's
  `editor`+`tools` identity re-slots a live tool host before any content
  pane. Plain terminal tabs stay session-local either way. The multi-tool
  pane invariant is: **save, apply, restart and project switch reproduce
  exactly the saved tab set in the saved position**.
- **The layout is the whole truth (#2042):** applying a saved layout applies
  it **verbatim** — tool panes, tool-tab hosts and singleton panels land at
  the layout's positions. The pre-#2042 reconciliation with `[tools.layout]`
  slot templates ("current slot config wins", #1899: snapshot leaves pruned
  and re-opened through the slot engine) is **gone**; apply now behaves
  exactly like the startup restore, which always materialized the saved
  tree as-is. The slot template remains the rule for **runtime opens** only:
  where a tool lands when it is opened fresh and the current layout has no
  pane for it (see [Tool Panes → Slot templates](./tool-panes.md)). One
  consequence is deliberate: a live **configured** tool pane (slot-assigned
  or with a home placement) that the applied layout does *not* mention
  re-places at its configured position after the apply
  (`leftoverConfiguredTools`/`replaceToolPane`) instead of grafting into the
  flexible region — an unmentioned tool behaves as if opened fresh, and
  unconfigured leftovers keep the #1577 graft.
- **New projects:** `restoreLayout` falls back to materializing the designated
  default layout when the project has no persisted `layout.json`; the built-in
  `layout.Default` pair stays the last resort. A project that saves its own
  layout owns it again from then on.

## Out of scope

Detached/floating OS windows, tabbed pane groups within one leaf, cross-pane
shared buffers, per-project named layouts, auto-applying a named layout on
project switch, keyboard binding *choices* for split/close/focus-move
(Roadmap 0080 owns the keymap; the ops here are binding-agnostic), and drag
animations.
