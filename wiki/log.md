# Log

## 2026-06-19

- Roadmap 0030 (Help Overlay) implemented: `internal/help` (`source.go` snapshot
  + binding join + scope grouping, `layout.go` responsive column-major packing,
  `viewport.go` vertical scroll wrapping `bubbles/viewport` with a position
  indicator, `help.go` overlay `tea.Model`). Root model hosts the overlay, opens
  it on `?`, forwards size + keys, and renders it on top. Binding resolver
  (roadmap 0080) consumed through a `BindingResolver` interface; not wired yet,
  so commands render title-only. The overlay renders as a content-sized floating
  pane centered over the layout (max two columns), composited via an ANSI-aware
  splice (`x/ansi`) so the base stays visible around it. Added the Help Overlay
  concept doc and the `bubbles` dependency.

- Roadmap 0020 (Plugins: Compile-in Registry) implemented: `internal/plugin`
  (Plugin interface + Command/Keymap/Pane/FileHandler/Hook capability types,
  Scope, ContextProvider), `internal/registry` (Register, conflict detection,
  deterministic ordering, enable/disable, lookups), `internal/host` (host.API +
  in-process impl). Root model now routes file opens through handlers, fires
  lifecycle hooks, resolves layered plugin keymaps, and exposes `RunCommand`.
  Added `plugins/example` reference plugin and the Plugin Extension Contract
  concept doc.

- Explorer reworked into an expandable tree rooted at a fixed project base:
  folders expand/collapse in place (`▾`/`▸`) instead of replacing the listing,
  and the explorer can no longer ascend above the root.
- Roadmap 0010 (Foundation) implemented: file explorer pane, modal vim editor
  pane, root model routing/focus/status line. Added concept docs for the
  foundation slice, explorer, and editor.
