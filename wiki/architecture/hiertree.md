---
type: concept
title: Hierarchy Tree
description: The lazily-expanding tree the call-hierarchy and type-hierarchy overlays and the Agent Trace tool window share — node shape, expand/collapse/parent-walk keys, stale-reply bookkeeping and the row renderer live once in internal/hiertree; the in-memory side (Static fetch, Refresh keeping expansion and selection by key, ExpandDeep, mouse helpers) was added for the trace window (#2840); each host keeps only its messages and direction.
resource: internal/hiertree/hiertree.go
tags: [architecture, lsp, overlay, tree, reusable, consolidation, agents]
timestamp: 2026-09-30T22:00:00Z
---

# Hierarchy Tree

Issue #2465 (roadmap 0500, consolidation sweep). The call-hierarchy overlay
(`internal/callhier`, #173) and the type-hierarchy overlay
(`internal/typehier`, #1454) were forked from each other and never diverged:
the node struct, the depth-first visible walk, the key switch including the
parent walk on `left`, the pending-request map that drops stale expansion
replies, and the sixty-line row renderer with its scroll window were
identical modulo the LSP item type. `internal/hiertree` is the leaf package
that holds them once.

## Shape

- **`Entry`** is what a row shows and where `enter` jumps: `Name`, `Detail`,
  `Path`, `Line` (0-based, rendered 1-based), `Col`.
- **`Row[T]`** is one node: its `Entry`, the protocol item `T` the host
  expands it with (`protocol.CallHierarchyItem` / `protocol.TypeHierarchyItem`),
  and the private tree state — depth, children, `expanded`, `loaded` (a
  completed fetch; an empty child set is then a leaf) and `loading` (in
  flight).
- **`Tree[T]`** is the overlay's tree state: the roots it was opened on, the
  current node set, the cursor and scroll window, and `pending`, the map from
  in-flight request id to the row awaiting children.

The tree is generic over the item type so a host's fetch continuation stays
typed instead of round-tripping through `any`.

## Lifecycle

`Open(roots, fetch)` resets onto the roots and expands the first one, so the
first paint is useful. `Rebuild()` — the direction-toggle path — rebuilds
fresh root nodes from the same roots and expands the first again; the
`pending` map is replaced, so late replies to orphaned requests find no row
and fall on the floor (the request counter keeps climbing, an orphaned id is
never reused). `Clear()` drops nodes and requests when the overlay closes.

`Expand(row)` re-shows a loaded row, does nothing for one already in flight
(or when there is no fetch), and otherwise allots the next request id, marks
the row loading and runs the host's `Fetch(reqID, item)`.

`Apply(reqID, stale, children)` consumes one reply. A request no longer
pending is dropped; one the host flags `stale` is retired but not applied;
otherwise the row unfolds onto the children at depth+1.

### In-memory trees (#2840)

The Agent Trace tool window has its whole tree in memory, so it needs
neither a round trip nor a rebuild that forgets state:

- `Static(tree, children)` builds a `Fetch` that answers from the item and
  calls `Apply` inside `Expand`, so a row unfolds before the caller's next
  line runs and no command travels through the Update loop. `SetFetch`
  (re)installs it — a host whose `Tree` lives by value inside a model that
  may be moved (a tool window detached into a tab) re-points the closure
  before use.
- `Refresh(roots, key)` replaces the roots and keeps what the user did:
  every row whose key was expanded is expanded again (synchronously with a
  `Static` fetch, else the returned command carries the fetches), the
  cursor lands on the row carrying the previously selected key — or stays
  clamped where it was when that row is gone — and the scroll window is
  kept. New rows come up collapsed; the host expands the ones it wants
  shown.
- `ExpandDeep(row)` expands a row and, as far as the fetch answers
  synchronously, every descendant.
- `Toggle(row)` folds an expanded row and expands a collapsed one — the
  enter action of a row that has nothing to open.
- `Roots`, `HasNodes`, `SetCursor`, `Top`, `Wheel(delta, height)` (through
  `ui.WheelWindow`), and `Row.Expanded` / `Depth` / `Leaf` give a pane host
  what its mouse handling and tests need.

## Keys

`Key(key, page, onEnter, onToggle) (tea.Cmd, bool)` routes one key and
reports whether the tree consumed it:

- the [shared list navigation](./list-navigation.md) first (`ui.ListNav`,
  `NavFull`; `page` is the render window's row count, so page keys jump one
  screenful — #1666);
- `enter` hands the selected row to `onEnter`;
- `right` / `l` / `space` expand the selected row;
- `left` / `h` collapse it, or on an already-collapsed row walk the cursor up
  to the nearest shallower row — the JetBrains-tree parent jump;
- `tab` calls `onToggle`.

`esc` and `q` come back unhandled: closing the overlay is the host's.

## Rendering

`RenderRows(width, height, pal, displayPath, empty)` lays the visible rows out
into a `height`-row window: the cursor is clamped first (`ui.ClampIndex`, so a
tree that shrank under the cursor keeps a selection), then the window follows
it (`ui.ScrollToShow`). Markers: `…` loading, `▾` expanded, `·` a loaded
leaf, `▸` otherwise; two spaces of indent per depth; name bold, marker,
detail and `path:line` faint; the cursor row on `SelectionMuted`; every row
clipped to `width`. A row without a `Path` prints no location and one with
a negative `Line` prints the path alone (#2840: turn and decision rows, a
file whose line the parser could not resolve). An empty tree renders the
host's `empty` text (`no calls`, `no types`) faint as its single row.

## Hosts

Each host keeps what differs and nothing else:

- its LSP messages — `CallHierarchyMsg` / `CallHierarchyCallsMsg` versus
  `TypeHierarchyMsg` / `TypeHierarchyItemsMsg` — and a `rows()` converter
  from the bridge's entry type to `Row[T]`;
- its direction flag (`incoming`, `supertypes`). The fetch closure the host
  hands `Open` reads the flag at request time, so `tab` only flips it and
  calls `Rebuild`; `Apply` marks a reply stale when its direction no longer
  matches;
- its overlay chrome: heading, hint row, box width — and in `callhier` the
  [code-preview column](./lsp.md) (#2053) beside the tree, which reads the
  selected row through `Tree.Current`.

The [Agent Trace tool window](./agent-trace.md#trace-tool-window-2840)
(`internal/tracepanel`, #2840) is the first non-LSP host: a pane rather
than an overlay, `Static` over `agenttrace.Node` children, `Refresh` on
every incremental read, `ExpandDeep` on the newest turn, and mouse handling
through `ui.RowAt` / `ui.ClickTracker` against `Top`, `SetCursor` and
`Wheel`.

Both hosts carry golden tests that pin the rendered overlay byte-for-byte;
the tree mechanics (expand/collapse, parent walk, scroll window, stale
replies, markers) are tested once in `internal/hiertree`.
