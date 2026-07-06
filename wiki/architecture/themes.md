---
type: concept
title: Themes / Color Schemes
description: Planned named-palette system — one theme name recolors syntax, explorer, and chrome together; unifies the duplicated color resolvers behind existing config models.
resource: roadmaps/0110-themes.md
tags: [architecture, themes, color, lipgloss, planned]
timestamp: 2026-07-06T00:00:00Z
---

# Themes / Color Schemes

> **Status: planned (Roadmap 0110).** Not implemented yet. Today `[theme].name`
> is inert; colors are set three separate ways (see below) and each carries its
> own duplicated color resolver. This stub fixes the model; update it when
> `internal/theme` lands.

Make `[theme].name` mean something: selecting a **named palette** (e.g.
`tokyo-night`) recolors the whole IDE — syntax, explorer, and chrome — in one
move, and collapses the duplicated color-token machinery into one leaf package.

## What already exists (do not rebuild)

- **`[theme].name` + `[theme].dark`** — selector slot (Roadmap 0040,
  `config/schema.go`). `name` is read by nothing today; `dark` is advisory.
- **`internal/highlight` `Theme`** (Roadmap 0100) — Tree-sitter capture → style,
  from `defaultCaptures` under `theme.captures.<name>` config. Syntax
  highlighting works today.
- **`internal/explorer` `colorTable`** (Roadmap 0050) — file glob/ext → color,
  from `defaultColors` under `[explorer.colors]`.
- Chrome (borders, status bar, selection, scrollbars, LSP popups) is scattered
  **hard-coded hex** in `internal/app`, `internal/explorer`, `internal/editor`.
- `highlight` and `explorer` each carry a near-identical `namedColors` map +
  token resolver — the duplication 0110 collapses.
- **Background bleed bug (0110 fixes it):** `app.render` sets one
  `Background(appBackground)` around the whole screen (`app.go:1512`), but pane
  bodies, the floating shell (`Esc Esc`), the palette, and LSP popups render
  cells with no background — lipgloss won't repaint occupied cells, so only the
  frame looks dark and interiors show the raw terminal background. Backgrounds
  must be painted **per surface**, not once at the root.

> **Naming caution:** `internal/palette` is the **command palette** (Roadmap
> 0070). The color package is **`internal/theme`**; resolved type `theme.Palette`
> (package `theme`, no clash) — never a new `internal/palette`.

## Standard we follow

Palettes mirror [sqlit](https://github.com/Maxteabag/sqlit), which reuses
[Textual's `Theme`](https://textual.textualize.io/guide/design/): a small flat
set of **semantic color slots** (`primary/secondary/accent/foreground/
background/surface/panel/border/success/warning/error` + `dark`), not a
per-widget sheet. Their built-ins (tokyo-night, nord, gruvbox, rose-pine,
catppuccin) port over near-verbatim.

## Model

One built-in `Theme` bundles three color groups so a single name sets everything:

```
name      "tokyo-night"
dark      true
ui        semantic chrome slots (background, surface, panel, border,
          selection, accent, foreground, success/warning/error, scrollbar …)
captures  map[capture]color   defaults for internal/highlight (keyword, string …)
files     map[glob|ext]color  defaults for internal/explorer (dir, go, md …)
```

Precedence (lowest → highest) — named palette sets defaults, per-key config wins:

```
palette.ui        <  (future chrome config keys)
palette.captures  <  theme.captures.<name>
palette.files     <  [explorer.colors]
                     user config  <  project config
```

## Structure (planned)

```
internal/theme/
  theme.go      Theme (ui slots + captures + files + dark) + Palette
  resolve.go    ONE color-token resolver — replaces the copies in
                highlight/theme.go and explorer/colors.go
  builtins.go   default, tokyo-night, nord, gruvbox(+light),
                rose-pine(+dawn), catppuccin-mocha(+latte)
  registry.go   name -> Theme lookup; merges plugin-registered themes; fallback
```

`internal/theme` is **leaf-level** (lipgloss only) so `highlight`, `explorer`,
`app`, `editor` import it without a cycle — same rule as `internal/config`.

## How it fits

- **Selector** is Roadmap 0040's `[theme]`; 0110 fills the content behind the
  name. Unknown name → `default` + non-fatal warning.
- **Existing color models keep their config-override APIs**; only their
  **default source** changes to the active palette's `captures` / `files`.
- **Registration** rides the Roadmap 0020 plugin contract: additive
  `Capabilities.Themes []Theme`. Built-ins ship as a compile-in theme plugin.
- **Consumption**: the `internal/app` root resolves the name to a
  `*theme.Palette` and threads it into panes; chrome renderers read ui slots
  instead of hex literals.
- **Live switch**: a config reload emits the existing `ConfigReloadedMsg`
  (`config/watch.go`); the root rebuilds the palette once and re-renders. No
  restart, no global mutable color singleton read mid-render.

## Surface fill

Backgrounds are painted **per surface**, never once at the root. Each pane body
(editor, explorer) fills the palette `surface` and pads every line to full
width/height; dividers/gaps fill `background`; every overlay (floating shell,
command palette, LSP popups, move/ghost preview) paints an opaque `surface`/
`panel` before compositing. This is what fixes the terminal-background bleed —
one outer `Background(...)` wrapper cannot, because lipgloss leaves
already-rendered inner cells at their default background.

## Boundaries

- Tree-sitter capture *classification* + highlight pipeline stay in 0100/0105;
  0110 supplies capture-color **defaults** only.
- Explorer file-color *resolution logic* stays in 0050; 0110 supplies file-color
  **defaults** + the shared resolver.
- Theme-picker UI / live preview is a later UX pass; selection here is
  config-driven.

See `roadmaps/0110-themes.md` for the full plan and milestone list.
