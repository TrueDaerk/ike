# Roadmap 0035 — Floating Shell (Reusable Overlay / Modal Component)

A reusable **floating pane** primitive: a centered, content-sized box composited
on top of the active layout that can host **any** `tea.Model`. It generalises the
one-off overlay built for the Help cheat sheet (Roadmap 0030) into a shared
component so modals, confirmation dialogs, plugin popups, and future pickers all
reuse the same chrome, sizing, scrolling, and dismissal behaviour instead of
re-implementing it. Help is refactored to be the first consumer, proving the API
against a real user.

## Prerequisites / Dependencies

- **01 Foundation** — bubbletea root model in `internal/app`; the shell is
  composited by the root `View` over the base layout, and the root forwards
  `tea.WindowSizeMsg` + keys to whichever shell is open.
- **03 Help Overlay** — `internal/help` is the existing implementation whose
  compositing (`overlayCenter`/`spliceLine`) and box/sizing/scroll logic are
  extracted upward. Help becomes a thin content provider plugged into the shell.
- **02 Plugins registry** — `internal/plugin` (`Pane` is a hostable `tea.Model`).
  A floating shell that hosts any `tea.Model` lets a plugin present its pane as a
  modal popup for free; this roadmap defines that seam, it does not add new
  plugin contract fields.
- **04 Settings** — `internal/config`, read-only, for optional shell tuning
  (default margin, max width/height fraction).

> **No new plugin contract.** The shell hosts the existing `tea.Model` /
> `plugin.Pane` shapes. Any host hooks (e.g. an "open as modal" request message)
> are additive and proposed against 0020 if needed.

## Architecture

```
internal/overlay/         pure compositing — no bubbletea state
  overlay.go              Center(base, top string, w, h int) string; ANSI-aware row splice (x/ansi)
internal/ui/              the floating shell component (tea.Model-shaped)
  floating.go             Floating: hosts a child view, owns chrome + open/close + dismiss + key routing
  sizing.go               content-size within terminal-minus-margin; max width/height clamps; title + indicator budget
  scroll.go               vertical scroll (wraps bubbles/viewport) when content overflows; position indicator
internal/help/            (03) refactored: Help provides content; Floating provides the shell
internal/app/             (01) root hosts/toggles a Floating, forwards size + keys, composites via overlay.Center
```

The split is deliberate:

- **`internal/overlay`** is pure string→string compositing (move `overlayCenter`
  / `spliceLine` out of `internal/app`, export, test in isolation).
- **`internal/ui.Floating`** is the stateful shell: border + padding chrome,
  content sizing, scroll-on-overflow, `esc` dismiss, `IsOpen`, key-swallow. It
  wraps a child (a `tea.Model` or a `View() string` provider) and is content
  agnostic.
- **`internal/help`** keeps only command snapshot + grouping + column layout
  (its content), and renders inside a `Floating`.

## Design rules

- **Host anything.** `Floating` accepts any child view; it never knows what it
  renders. Help, modals, plugin popups are all just children.
- **Content-sized, bounded.** The pane sizes to its content, clamped to the
  terminal minus a margin and optional max width/height fraction. Never covers
  the whole TUI.
- **Composite, don't replace.** The base layout stays visible around the pane;
  `overlay.Center` splices by visual column, preserving ANSI styling on both
  sides.
- **Scroll, never truncate.** Overflowing content scrolls (`bubbles/viewport`)
  with a position indicator; the pane never silently cuts content.
- **Swallow + dismiss.** While open, the shell consumes all keys; `esc` (and a
  configurable dismiss set) closes it. The host suppresses other routing.
- **One stacking owner.** The root decides what is open; v1 is single-level (one
  floating pane at a time). Stacking multiple modals is out of scope.
- **Presentation neutral.** The shell dispatches nothing of its own beyond
  open/close; children own their actions.

## Milestones

- [ ] `internal/overlay/overlay.go`: extract `Center` (+ row splice) from `internal/app`, export, ANSI-aware, with unit tests (fit/no-fit, centering math, style preservation).
- [ ] `internal/ui/sizing.go`: content-size within terminal-minus-margin; max width/height clamps; reserve title + indicator rows.
- [ ] `internal/ui/scroll.go`: vertical scroll wrapping `bubbles/viewport` with a position indicator (generalised from the help scroller).
- [ ] `internal/ui/floating.go`: `Floating` shell — host a child view, chrome, open/close, `IsOpen`, key-swallow, configurable dismiss keys, recompute on `tea.WindowSizeMsg`.
- [ ] Refactor `internal/help` to render its content inside `Floating`; delete the help-local chrome/sizing/scroll now owned by the shell. Behaviour and tests stay green.
- [ ] Root-model integration in `internal/app`: host a single active `Floating`, forward size + keys, composite via `overlay.Center`.
- [ ] Plugin seam: a plugin can present its `plugin.Pane` as a floating modal (host helper / request message); document the additive hook if one is needed.
- [ ] Optional config hooks (`internal/config`): default margin, max width/height fraction, dismiss key set.
- [ ] Tests: overlay centering + fit bounds, content sizing/clamping, scroll bounds, dismiss + key-swallow, child-view hosting, help-still-works regression.
- [ ] Wiki: document the floating shell, the overlay/shell/content split, and how modals + plugin popups reuse it under `wiki/`.

## Out of scope

- Stacked / nested modals (multiple floating panes at once) — v1 is single-level.
- Animations / transitions.
- Drag, move, or resize of the floating pane (windowing belongs to the broader
  pane manager, not this primitive).
- Specific modal content (confirm dialogs, pickers) — those are separate features
  that consume this shell.
- The plugin "open as modal" contract beyond the minimal additive seam (full
  plugin UX is owned by the plugin roadmaps).
