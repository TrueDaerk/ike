---
type: concept
title: Themes / Color Schemes
description: Planned semantic-slot theme system — built-in palettes, the registry/selector, and how panes consume a shared Palette instead of color literals.
resource: roadmaps/0110-themes.md
tags: [architecture, themes, color, lipgloss, planned]
timestamp: 2026-06-20T00:00:00Z
---

# Themes / Color Schemes

> **Status: planned (Roadmap 0110).** Not implemented yet. Colors are still
> hard-coded lipgloss literals across the render code. This stub fixes the model
> so later work and the wiki stay in sync. Update it when `internal/theme` lands.

A single, typed theme system so every pane reads its colors from a shared
**palette** instead of hard-coding lipgloss color literals. A user selects a
theme by name in `settings.toml`; switching it re-colors the whole IDE without a
restart.

## Standard we follow

The theme model mirrors [sqlit](https://github.com/Maxteabag/sqlit), which reuses
[Textual's `Theme`](https://textual.textualize.io/guide/design/): a small flat
set of **semantic color slots** plus a `variables` extension map and a `dark`
flag — *not* a per-widget color sheet. Slots:

```
name        unique id, e.g. "tokyo-night"
primary     main accent / selection / active
secondary   secondary accent
accent      links, focus rings, highlights
foreground  default text
background  app background
surface     pane body background
panel       raised panel (title bars, status bar)
border      pane borders, dividers
success / warning / error
dark        is this a dark theme?
variables   map[string]string  extra named slots for downstream roadmaps
```

Same slot set as sqlit's built-ins, so their palettes port over near-verbatim.

## Structure (planned)

```
internal/theme/
  theme.go      Theme struct (semantic slots + variables + dark) + Color
  palette.go    resolved Palette handed to renderers (cached lipgloss styles)
  builtins.go   default, tokyo-night, nord, gruvbox(+light),
                rose-pine(+dawn), catppuccin-mocha(+latte)
  registry.go   name -> Theme lookup; merges registry-provided themes
  syntax.go     editor syntax slot group (keyword/string/comment/number/type/function)
```

`internal/theme` is **leaf-level** (lipgloss only, no other IKE imports) so any
pane package can import it without a cycle — same rule as `internal/config`.

## How it fits

- **Selector** lives in Roadmap 0040's `[theme]` section (`name`, `dark`); this
  system fills the **content** behind that name. Unknown name → `default` +
  warning.
- **Registration** rides the Roadmap 0020 plugin contract: an additive
  `Capabilities.Themes []Theme` capability. Built-in palettes ship as a
  compile-in theme plugin; third-party plugins add their own.
- **Consumption**: the `internal/app` root resolves the configured name to a
  `*theme.Palette` and threads it into every pane. Renderers ask for
  `palette.Accent()` / `palette.Border()` — never a raw color literal.
- **Live switch**: a config reload emits a re-theme `tea.Msg`; the root rebuilds
  the palette once and re-renders. No restart, no global mutable singleton read
  mid-render.

## Boundaries

- Syntax-token **slot names** + default mapping live here; tree-sitter/LSP token
  classification wiring stays in the editor work (Roadmap 0060).
- Theme-picker UI / live preview is a later UX pass; selection here is
  config-driven.

See `roadmaps/0110-themes.md` for the full plan and milestone list.
