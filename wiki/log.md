# Log

## 2026-06-20

- Roadmap 0040 (Settings / Configuration) implemented: new leaf-level
  `internal/config` package — typed `Config` sections (`schema.go`), in-code
  defaults (`defaults.go`), `~/.ike` + `{root}/.ike` discovery with
  `IKE_CONFIG_DIR` override (`discovery.go`), TOML decode isolated behind the
  package (`load.go`), deep map merge with scalar-replace / table-merge /
  list-replace semantics (`merge.go`), clamp-and-warn validation with non-fatal
  `Diagnostic`s and parse-error layer isolation (`validate.go`), an idempotent
  `Extension` registration hook (`extend.go`), `Load`/`Get`/`Set` accessors plus
  `Config.Flat` (`config.go`), a `ConfigReloadedMsg` reload seam (`watch.go`),
  and a typed setter seam `PushHistory` (`write.go`). `internal/host` now depends
  on `internal/config` via `host.FromConfig` (flat read-only view backing the
  plugin API); `internal/app.New` loads the merged config at startup. Backed by
  `BurntSushi/toml`. Tests cover precedence, table/list merge, clamp-and-warn,
  parse-error isolation, and extend round-trip (config 87% coverage).


- Roadmap 0036 (Pane Drag) implemented: new pure `internal/layout` split-tree
  (`tree.go` types + `Compute`/`Rects` exact tiling, `rect.go` hit-testing +
  drop zones, `resize.go` clamped divider drag, `move.go` drop-zone re-parent,
  `state.go` tolerant encode/decode). `internal/app` replaces hard-coded
  `explorerWidth`/`JoinHorizontal` with tree-driven `Rects`, adds a `tea.MouseMsg`
  drag state machine (press hit-test → resize/move, release commit), and a
  per-project layout store (`store.go`, `IKE_CONFIG_DIR`/`.ike/layout.json`,
  save-on-release, default fallback on stale state). `cmd/ike` enables
  `tea.WithMouseCellMotion`. New concept doc `architecture/pane-layout.md`.

- Roadmap 0110 (Themes) planned: added `roadmaps/0110-themes.md` and a stub
  concept doc `architecture/themes.md`. Semantic-slot theme model mirroring
  sqlit/Textual; built-in palettes (tokyo-night, nord, gruvbox, rose-pine,
  catppuccin); selector behind 0040's `[theme]`, registration via 0020. Stub is
  marked planned — not implemented yet.
- Roadmap 0035 (Floating Shell) implemented: extracted the one-off help overlay
  chrome into a reusable component. New `internal/overlay` (pure ANSI-aware
  `Center` compositing, moved out of `internal/app`) and `internal/ui`
  (`Floating` shell hosting any `ui.Content`; `sizing.go` content budget;
  `scroll.go` generalised scroller wrapping `bubbles/viewport`; `ModelContent`
  adapter to float a view-only model). `internal/help` refactored to a
  `ui.Content` provider (snapshot + column layout only); its local chrome,
  sizing, and scroll deleted. Root model now hosts one active `*ui.Floating`,
  forwards size + keys, and composites via `overlay.Center`. Added an additive
  in-process plugin seam, `host.OpenModalRequest{Title, View}`, so a plugin can
  present its pane as a floating modal; optional `overlay.*` config tuning
  (margin, max width/height fraction). Added the Floating Shell concept doc and
  updated Help Overlay.

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
