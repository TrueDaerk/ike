---
type: concept
title: Themes / Color Schemes
description: Named-palette system — one [theme].name recolors syntax, explorer, and chrome together; one shared color resolver; plugin-extensible built-ins.
resource: internal/theme
tags: [architecture, themes, color, lipgloss]
timestamp: 2026-07-11T00:00:00Z
---

# Themes / Color Schemes

**Status: implemented (Roadmap 0110).** `[theme].name` selects a **named
palette** (e.g. `tokyo-night`) that recolors the whole IDE — syntax
highlighting, explorer file colors, and all chrome — in one move. The
previously duplicated color-token machinery lives in one leaf package,
`internal/theme`.

> **Naming caution:** `internal/palette` is the **command palette** (Roadmap
> 0070). The color package is **`internal/theme`**; its resolved type is
> `theme.Palette` (package `theme`, no clash).

## Standard we follow

Palettes mirror [sqlit](https://github.com/Maxteabag/sqlit), which reuses
[Textual's `Theme`](https://textual.textualize.io/guide/design/): a small flat
set of **semantic color slots**, not a per-widget sheet. The IKE slot set
(`theme.UI`): `Background`, `Foreground`, `Surface`, `Panel`, `Border`,
`BorderFocus`, `Selection`, `SelectionText`, `SelectionMuted`, `Accent`,
`Primary`, `Secondary`, `Success`, `Warning`, `Error`, `Info`, `Hint`,
`MoveSource`, `DropTarget`, `Ghost`, `ScrollbarTrack`, `ScrollbarThumb`.

## Model

One `theme.Theme` bundles three color groups so a single name sets everything:

```
Name      "tokyo-night"
Dark      true
UI        semantic chrome slots (see above)
Captures  map[capture]color   defaults for internal/highlight (keyword, string …)
Files     map[glob|ext]color  defaults for internal/explorer (dir, go, md …)
```

`theme.NewPalette(t)` resolves a Theme into a `*theme.Palette` of concrete
`color.Color`s; empty ui slots and missing maps **backfill from the default
theme**, so a sparse third-party theme still yields a complete palette.

Precedence (lowest → highest) — the named palette sets defaults, per-key
config wins:

```
palette.Captures  <  theme.captures.<name>
palette.Files     <  [explorer.colors]
                     user config  <  project config
```

## Structure

```
internal/theme/
  theme.go      UI slots, Theme, Palette (resolved colors), DefaultPalette()
  resolve.go    theme.Resolve — the ONE color-token resolver (name/hex/ANSI);
                replaced the duplicated copies in highlight and explorer
  builtins.go   default, tokyo-night, nord, gruvbox(+light), rose-pine(+dawn),
                catppuccin-mocha(+latte)
  registry.go   theme.Select(name, extra) — lookup over builtins + plugin
                themes, fallback to default (found=false lets callers warn)
  theme_test.go slot completeness, unique names, lookup/fallback, overrides
```

`internal/theme` is **leaf-level** (lipgloss only), so `highlight`, `explorer`,
`app`, `editor`, `ui`, `palette`, and `help` all import it without a cycle.

## How it fits

- **Selection**: `internal/app.resolveTheme` reads `theme.name` from the merged
  config and calls `theme.Select` over `registry.Themes()` (the plugin-
  contributed themes). Unknown name → `default` + non-fatal status warning.
- **Registration**: additive `plugin.Capabilities.Themes []theme.Theme`.
  Built-ins ship as the compile-in `themeProvider` plugin
  (`internal/app/theme.go`, id `themes`); third-party plugins add more, and
  `registry.Themes()` dedupes by name (first owner by sorted plugin order).
- **Threading**: `pane.Registry.SetPalette` pushes the `*theme.Palette` into
  every pane instance (`editor.SetPalette`, `explorer.SetPalette`); the app
  also threads it into the command palette, floating shell, and help. Editor
  and explorer keep the config-override APIs — only their **default source**
  changed: `highlight.NewTheme(defaults, get)` takes the palette's `Captures`,
  the explorer merges `[explorer.colors]` over the palette's `Files`.
- **Chrome reads slots, not literals**: pane borders, dividers, status bar,
  ghost/move preview (`internal/app`), selection/scrollbars/prompt/hover
  (`internal/explorer`), visual selection, LSP popups and diagnostics
  (`internal/editor`), the floating shell + scroller (`internal/ui`), the
  command palette (`internal/palette`), and help (`internal/help`). No hex
  literal exists outside `internal/theme`.
- **Live switch**: the app handles `config.ConfigReloadedMsg` (emitted by
  `config.Reload`): it `config.Set`s the fresh config, re-resolves the
  palette, re-threads it (`applyTheme` + `pane.Registry.Reconfigure`), and
  re-renders. No restart, no global mutable color singleton read mid-render.
- **`dark` follows the palette**: `Palette.Dark` carries the theme's own flag,
  which wins over the advisory `[theme].dark` for light/dark decisions.

## Background painting

The screen-wide background is set at the **renderer level**
(`tea.View.BackgroundColor/ForegroundColor` in `app.View`), not by wrapping the
composed frame in a lipgloss `Background(...)` style. Inner styled spans emit
full SGR resets, which would clear a wrapped background and let the raw
terminal background bleed through pane interiors and overlays; setting the
renderer default makes every reset fall back to the palette background
instead. Raised surfaces (status bar, LSP popups, hover rows, selected rows)
additionally paint their own `Panel`/`Selection` backgrounds.

## Contrast rule (adding a theme)

Every built-in theme must pass **WCAG AA text contrast (≥ 4.5:1)** on the
fg/bg slot pairs the chrome actually renders; `TestBuiltinThemeContrast`
(`internal/theme/contrast_test.go`) enforces this table-driven over all
builtins, so a new theme with unreadable pairs fails CI. The checked pairs:
`Foreground` on `Background`/`Surface`/`Panel`; `SelectionText` on
`Selection` and on `Primary` (the completion selected row paints
`SelectionText` on `Primary`); `Accent` and `Secondary` on `Surface`
(`Secondary` also on `Panel`); `Success` on `Surface`; and each diagnostic
color (`Warning`/`Error`/`Info`/`Hint`) on both `Surface` and `Panel`.
Border/indicator slots (`BorderFocus`, `MoveSource`, `DropTarget`, `Ghost`,
scrollbars) are exempt — they never carry text. When designing a theme, pick
the darkest/lightest canonical shade of each accent that clears the bar
rather than inventing new hues.

Renderers must never pair a hardcoded color with a theme color: a
`Selection`/`Primary` background always sets `Foreground(SelectionText)`
explicitly (terminal-default text on a theme background was the source of
issue #384).

## Built-in palettes

`default` (today's colors; the low-contrast diagnostic/selection slots were
lifted to AA contrast in #384), `tokyo-night`,
`nord`, `gruvbox`, `gruvbox-light`, `rose-pine`, `rose-pine-dawn`,
`catppuccin-mocha`, `catppuccin-latte`. Select via:

```toml
[theme]
name = "tokyo-night"
```

## Switching at runtime

The `themes` plugin registers one global palette command per built-in
(`themes.select.<name>`, shown as "Theme: <name>" under `:`). It dispatches
`app.SelectThemeMsg`; the root resolves the name (built-ins + plugin themes)
and re-threads the palette via `applyTheme`. An unknown name falls back to
`default` with a status warning.

The runtime choice is **persisted in the session store** (`session.json`,
field `theme`), not in `settings.toml` — it is runtime UI state like the
layout, so `settings.toml` stays untouched (that write path belongs to Roadmap
0040/0090). `restoreSession` re-applies it on the next launch, overriding the
config-derived theme. Only an *explicit* palette pick is recorded
(`Model.themeOverride`); a purely config-driven theme leaves the field empty,
so editing `[theme].name` keeps working until a runtime pick overrides it.

Live config reloads respect the override (#241): `reloadConfig` re-resolves
the theme from config only when `[theme].name` itself changed in that reload
— an explicit theme edit wins and clears the override — while any unrelated
settings change leaves the runtime-selected palette in place.

## Boundaries

- Tree-sitter capture *classification* + the highlight pipeline stay in
  0100/0105; this system supplies capture-color **defaults** only.
- Explorer file-color *resolution logic* stays in 0050; this system supplies
  file-color **defaults** + the shared resolver.
- A dedicated picker UI with live preview is a later UX pass; today's runtime
  switching is plain palette commands (see above), persistence is config-only.

The original plan and milestones lived in the former `roadmaps/0110-themes.md` (planning moved to GitHub issues; the file remains in git history).
