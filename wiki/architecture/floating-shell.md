---
type: concept
title: Floating Shell
description: Reusable centered overlay component — a content-sized box composited on the active layout that hosts any tea.Model-shaped content, owning chrome, sizing, scroll, and dismissal.
resource: internal/ui/floating.go
tags: [architecture, overlay, modal, floating, reusable, bubbletea]
timestamp: 2026-07-30T12:00:00Z
---

# Floating Shell

Roadmap 0035. A reusable **floating pane** primitive: a centered, content-sized
box composited on top of the active layout that can host **any** content. It
generalises the one-off overlay built for the Help cheat sheet (roadmap 0030)
into a shared component so modals, confirmation dialogs, plugin popups, and
future pickers all reuse the same chrome, sizing, scrolling, and dismissal
instead of re-implementing it. Help is the first consumer, proving the API.

## Structure

```
internal/overlay/
  overlay.go    pure string→string compositing: Center(base, top, w, h) + ANSI-aware row splice (x/ansi)
internal/ui/
  sizing.go     content budget: terminal-minus-margin, box chrome, title row, optional max width/height fraction
  scroll.go     vertical scroll wrapping bubbles/viewport + position indicator; adds g/G to the built-in keys
  floating.go   Floating shell: hosts a Content child, chrome + open/close + IsOpen + key-swallow + dismiss
  stack.go      Stack: z-ordered floating layers (#1237) — topmost owns input, composite bottom-to-top
internal/help/
  help.go       refactored: Help is now a ui.Content provider (snapshot + column layout), no chrome of its own
internal/app/
  app.go        root hosts the Floating stack, forwards size + keys to the topmost layer, composites bottom-to-top
```

The split is deliberate:

- **`internal/overlay`** is pure compositing — no bubbletea state. `Center`
  splices the box's rows into the base canvas by visual column, emitting reset
  sequences (`\x1b[0m`) on both sides so the box's styling never bleeds into the
  base and the base's styling survives around the box. Returns the base
  untouched when the box does not fit.
- **`internal/ui.Floating`** is the stateful shell: rounded border + padding
  chrome, an underlined title row with a dismiss hint followed by a blank
  spacer row, content sizing, scroll-on-overflow,
  `esc` (and a configurable dismiss set) to close, `IsOpen`, and key-swallow. It
  is content-agnostic.
- **`internal/help`** keeps only its command snapshot, grouping, and column
  layout (its content), rendered inside the shell.

## The Content seam

A shell hosts anything implementing `ui.Content`:

```go
type Content interface {
    Title() string          // heading shown at the top of the shell
    Render(width int) string // body laid out to fit width columns; the shell scrolls it
}
```

The shell computes a width budget (terminal minus margin, box chrome, and the
title row, clamped by an optional max width fraction), hands it to
`Content.Render`, then frames and scrolls the result. `ModelContent` adapts any
view-only model (`func() string`) into `Content`, ignoring the width budget — it
is the seam that lets a plugin float its `plugin.Pane` as a modal for free.

Two optional Content extensions refine key routing while the shell is open
(checked in this order: filter → dismiss → key handler → scroll):

- **`ui.Filterable`** (#271): printable keys become a live filter string
  instead of scroll keys; `esc` first clears an active filter.
- **`ui.KeyHandler`** (#655): keys that neither fed the filter nor matched a
  dismiss key are offered to the content via `HandleKey(key) bool` before
  scroll handling. Returning `true` consumes the key (the shell relayouts);
  `false` falls through to the scroller. This lets content own view toggles
  (help's essentials/all `tab` switch) or paging keys without the shell
  knowing about them. Dismiss keys never reach the content.

## Sizing & scrolling

- `budget(termW, termH, margin, maxWFrac, maxHFrac)` reserves `2*margin`, the box
  chrome (`frameH`/`frameV` = border + padding both axes), and two title rows
  (the heading plus its blank spacer, `titleRows`),
  then clamps by the optional max width/height fraction, flooring at 1.
- Overflowing content **scrolls, never truncates**: the scroller wraps
  `bubbles/viewport` (↑/↓, pgup/pgdn, ctrl+u/ctrl+d, plus g/G for top/bottom) and
  appends a position indicator (`▲ … ▼  NN%`) only when the content overflows.
  The pane therefore never grows past the terminal.
- **User resize** (#774): `cmd+shift+arrows (macOS; spelled shift+super) / ctrl+shift+arrows / alt+shift+arrows` (CSI-parameter-encoded, so
  delivered everywhere) adjust the open shell's content budget; the delta is
  persisted per content title in the per-project `winsize.json` store
  (`ui.WinSizes`, `IKE_CONFIG_DIR`-redirectable) and re-clamped against the
  live terminal budget on every layout, flooring at a readable minimum.
  Growth past the content's natural size is a no-op (the shell stays
  content-sized); shrinking engages the scroller. The same store also backs
  **mouse resize** (#933): pressing the shell's border ring — the outermost
  cell; edges resize one axis, corners both — starts a drag handled by the
  root model (`floatResizeDrag`), which nudges the store un-persisted per
  motion step and flushes it on release. Floats are centered, so one pointer
  cell maps to **two** size cells (#1243): the grabbed edge tracks the
  pointer exactly, the opposite edge mirrors outward. `ui.popup_max_width` (#932, default
  110, 0 disables) additionally caps the shell's outer width on large
  terminals; the resize delta applies on top of the capped base.
- **The body re-renders on every `View()`** (#409), preserving the scroll
  offset. Content that mutates its state in place after opening — a modal
  moving its cursor or dropping list items — shows the change on the very next
  frame; hosts never need to force a relayout (`SetSize`/`SetContent`) after
  handling a key. `SetContent`/`Open` still reset scroll to the top.

## The floating stack (#1237)

`ui.Stack` (`internal/ui/stack.go`) layers multiple shells in z-order while the
shell itself stays single-level and layering-unaware:

- **Base vs transient layers.** Layers passed to `NewStack` are persistent —
  they survive `Close` and their owner reopens them (the root's `shell` is the
  bottom layer). Layers added via `Push` are transient: `Push` threads the
  stack's shared state (terminal size, palette, `WinSizes` store, width cap)
  into the shell, opens it topmost, and the layer leaves the stack when it
  closes.
- **Input.** `Update` routes every message to the **topmost open layer only**
  (key-swallow as before); a dismiss key closes just that layer — one layer
  per keypress. `tea.WindowSizeMsg` resizes every layer.
- **Compositing.** `Composite(base, w, h)` draws every open layer
  bottom-to-top via `overlay.Center`, so the topmost is drawn last and fully
  readable over the lower ones.
- **Mouse.** The root routes mouse to `Top()`: a press outside the topmost
  layer `Pop`s only that layer (outside-click, #116); a border press starts
  the resize drag (#933) on the topmost layer.
- A stack of one behaves exactly like a bare `Floating`, so all existing
  single-shell consumers are unchanged.

## Root integration

`internal/app` holds one persistent `*ui.Floating` (`shell`) as the base layer
of a `*ui.Stack` (`floats`, #1237); extra dialogs stack on top via `Push`. On
`tea.WindowSizeMsg` the root forwards the size to the stack; while any layer is
open the topmost **swallows every key** and shadows all other routing; `View`
composites the open layers bottom-to-top so the base layout stays visible
around them.

- **Help:** `?` snapshots the registry into the `*help.Help` content, sets it on
  the shell, and opens. Dismiss set is `esc/?/q`.
- **Plugin modal:** a plugin dispatches `host.OpenModalRequest{Title, View}`
  (additive, in-process — no new plugin contract field); the root wraps it in
  `ui.ModelContent` and opens the same shell.

## Configuration

Optional tuning read from config (roadmap 0040) via `overlay.*` keys: `margin`,
`max_width_fraction`, `max_height_fraction`. Zero values select built-in
defaults, so the empty config is valid. `DismissKeys` and `Accent` are set by the
host per shell.

## Design rules

- **Host anything.** The shell never knows what it renders; Help, modals, plugin
  popups are all just `Content`.
- **Content-sized, bounded.** The pane sizes to its content, clamped to the
  terminal minus a margin and optional max fractions; it never covers the whole
  TUI.
- **Composite, don't replace.** The base layout stays visible around the pane.
- **Swallow + dismiss.** While open the shell consumes all keys; a dismiss key
  closes it.
- **One stacking owner.** The root decides what is open; z-order lives in
  `ui.Stack`, never in the shells themselves.

## Boundaries

- Animations and drag/move of the pane are out of scope (windowing belongs to
  the broader pane manager). Stacked modals are in scope since #1237 via
  `ui.Stack`.
- Specific modal content (confirm dialogs, pickers) are separate features that
  *consume* this shell.
- The **popup terminal** (#1398) deliberately does *not* use this shell: the
  shell's dismiss/filter/scroll key priority is the inverse of a PTY's raw
  pass-through (esc must reach vim inside the popup). It composites its own
  pane-style box via `overlay.Center` with its own funnel branch — see
  [Integrated Terminal](/architecture/terminal.md). It does reuse the shared
  size machinery: `ui.WinSizes` (key `popupterm`), `ResizeZone` and the
  resize chords — plus a second, user-scoped `WinSizes` store that carries its
  last chosen size into projects without a delta of their own (#1714, see the
  terminal doc). `WinSizes.Has`/`Set` exist for that cascade: `Has` separates
  "never resized here" from a stored zero delta, `Set` mirrors a delta instead
  of accumulating it.
- The plugin "open as modal" contract beyond the minimal additive
  `OpenModalRequest` seam is owned by the plugin roadmaps.
