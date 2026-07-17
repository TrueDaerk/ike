---
type: concept
title: Floating Shell
description: Reusable centered overlay component — a content-sized box composited on the active layout that hosts any tea.Model-shaped content, owning chrome, sizing, scroll, and dismissal.
resource: internal/ui/floating.go
tags: [architecture, overlay, modal, floating, reusable, bubbletea]
timestamp: 2026-07-17T00:00:00Z
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
internal/help/
  help.go       refactored: Help is now a ui.Content provider (snapshot + column layout), no chrome of its own
internal/app/
  app.go        root hosts one Floating, forwards size + keys, composites via overlay.Center
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
- **The body re-renders on every `View()`** (#409), preserving the scroll
  offset. Content that mutates its state in place after opening — a modal
  moving its cursor or dropping list items — shows the change on the very next
  frame; hosts never need to force a relayout (`SetSize`/`SetContent`) after
  handling a key. `SetContent`/`Open` still reset scroll to the top.

## Root integration

`internal/app` holds a single active `*ui.Floating` (v1 is **single-level** — one
shell at a time). On `tea.WindowSizeMsg` it forwards the size; while open the
shell **swallows every key** and shadows all other routing; `View` composites it
centered via `overlay.Center` so the base layout stays visible around it.

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
- **One stacking owner.** The root decides what is open; v1 is single-level.

## Boundaries

- Stacked/nested modals, animations, and drag/move/resize of the pane are out of
  scope (windowing belongs to the broader pane manager).
- Specific modal content (confirm dialogs, pickers) are separate features that
  *consume* this shell.
- The plugin "open as modal" contract beyond the minimal additive
  `OpenModalRequest` seam is owned by the plugin roadmaps.
