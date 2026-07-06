# Roadmap 0110 — Themes / Color Schemes

Give IKE **named color schemes**. Today `[theme].name` exists in config but is
**inert** — nothing reads it. Colors are set three separate ways: syntax capture
colors via `[theme].captures.*` (Roadmap 0100, `internal/highlight/theme.go`),
explorer file colors via `[explorer.colors]` (Roadmap 0050,
`internal/explorer/colors.go`), and a pile of hard-coded hex literals for chrome
(borders, status bar, selection, scrollbars) scattered across `internal/app`,
`internal/explorer`, and `internal/editor`. Each color subsystem also carries its
**own duplicated** `namedColors` map + token resolver.

This roadmap makes `[theme].name` mean something: selecting a **named palette**
(e.g. `tokyo-night`) recolors *all three* groups coherently in one move, and
unifies the duplicated color-token machinery into one leaf package.

**Standard we follow.** Palettes mirror the theme model of
[sqlit](https://github.com/Maxteabag/sqlit), which reuses
[Textual's `Theme`](https://textual.textualize.io/guide/design/): a small flat
set of **semantic color slots** (`primary/secondary/accent/foreground/background/
surface/panel/border/success/warning/error` + `dark`), not a per-widget sheet.
Their built-in palettes (tokyo-night, nord, gruvbox, rose-pine, catppuccin, …)
port over near-verbatim.

## What already exists (do not rebuild)

- **`[theme].name` + `[theme].dark`** — the selector slot (Roadmap 0040,
  `config/schema.go`). `name` is currently read by nothing; `dark` is advisory.
- **`internal/highlight` `Theme`** (0100) — resolves Tree-sitter capture names to
  lipgloss styles from `defaultCaptures` layered under `theme.captures.<name>`
  config keys. **Syntax highlighting works today** (see the running editor).
- **`internal/explorer` `colorTable`** (0050) — resolves file globs/extensions to
  colors from `defaultColors` layered under `[explorer.colors]`.
- Both packages carry a near-identical `namedColors` map + a `resolveColor`/
  `color` token resolver — the duplication this roadmap collapses.

**Known background bug this roadmap must fix.** `app.render` wraps the *whole
screen* in one `Background(appBackground)` style (`app.go:1512`), but pane bodies
(editor, explorer), the floating shell (`Esc Esc`), the command palette, and LSP
popups render their cells with **no background set**. lipgloss won't repaint
cells inner content already occupies, so only the frame/borders look dark — pane
interiors and overlays show the raw **terminal background**. Painting once at the
root is structurally insufficient; each surface must fill its own rectangle (see
the surface-fill rule below).

> **Naming caution:** `internal/palette` is already the **command palette**
> (Roadmap 0070). The color package is **`internal/theme`**; its resolved type is
> `theme.Palette` (package `theme`, so no import clash) — never a new
> `internal/palette`.

## Prerequisites / Dependencies

- **0040 Settings** — owns `[theme]` (`name`, `dark`) and the reload seam. This
  roadmap fills the **content** behind the name and reads
  `config.Get().Theme.Name`. Live re-theme rides the existing
  `config.ConfigReloadedMsg` / `Reload` cmd (`config/watch.go`) — no new plumbing.
- **0100 Highlight** — `highlight.NewTheme(get)` already layers config over
  built-in capture defaults. This roadmap feeds those **defaults from the selected
  palette** instead of the hard-coded `defaultCaptures`; the config-override path
  is unchanged.
- **0050 Explorer** — same relationship with `explorer` `colorTable` /
  `defaultColors` and `[explorer.colors]`.
- **0020 Plugins** — palettes register like any capability: additive
  `Capabilities.Themes []Theme`. Built-ins ship as a compile-in theme plugin;
  third-party plugins add their own. `internal/registry` exposes name→palette.
- **0010 Foundation** — the `internal/app` root resolves the active palette and
  threads it into panes; re-theme is a routed `tea.Msg`.

## Architecture

```
internal/theme/
  theme.go      Theme (semantic slots + captures + explorer colors + dark) + Palette
  resolve.go    ONE color-token resolver (name/hex/ANSI) — replaces the copies in
                highlight/theme.go and explorer/colors.go
  builtins.go   default, tokyo-night, nord, gruvbox(+light), rose-pine(+dawn),
                catppuccin-mocha(+latte)
  registry.go   name -> Theme lookup; merges plugin-registered themes; fallback
  theme_test.go slot completeness, name lookup, fallback, override precedence
```

A built-in `Theme` bundles the three color groups so one name sets everything:

```
name        "tokyo-night"
dark        true
ui          semantic chrome slots (background, surface, panel, border,
            selection, accent, foreground, success/warning/error, scrollbar …)
captures    map[capture]color   defaults for internal/highlight (keyword, string …)
files       map[glob|ext]color  defaults for internal/explorer (dir, go, md …)
```

Resolution / precedence (lowest → highest):

```
palette (named theme)  <  user config  <  project config
   ui.*                    (future chrome keys)
   captures.*        <     theme.captures.<name>
   files.*           <     [explorer.colors]
```

- `internal/theme` is **leaf-level** (lipgloss only) so `highlight`, `explorer`,
  `app`, `editor` all import it without a cycle — same rule as `internal/config`.
- `highlight.NewTheme` / explorer `colorTable` keep their config-override APIs;
  only their **default source** changes to the active palette's `captures`/`files`.

## Design rules

- **One name, coherent recolor.** `[theme].name = "nord"` sets ui + captures +
  files defaults together. Per-key config (`theme.captures.*`, `[explorer.colors]`)
  still wins on top, so existing user overrides keep working unchanged.
- **Unknown name → `default` + non-fatal warning.** A bad/missing palette must
  never crash or blank the IDE (mirrors 0040's clamp-and-warn).
- **One resolver.** Collapse the duplicated `namedColors` + `resolveColor`/`color`
  into `theme.resolve`; `highlight` and `explorer` call it. Named tokens, hex, and
  raw ANSI indices all keep working.
- **Chrome reads slots, not literals.** The scattered hex (`#121212`, `#585858`,
  `#303030`, `#5f87ff`, `#ffaf5f`, `#005f87`, diagnostic colors, …) maps onto
  semantic ui slots and disappears. `app.appBackground`/`appForeground` become the
  palette's `background`/`foreground`.
- **Backgrounds are painted per surface, never once at the root.** A single outer
  `Background(...)` wrapper does **not** color pane interiors or overlays —
  lipgloss leaves already-rendered inner cells at their default background, so the
  terminal background bleeds through (the bug above). Every surface fills its own
  rectangle with the palette background/surface: each pane body (editor, explorer)
  paints `surface`; dividers/gaps paint `background`; every overlay (floating
  shell, command palette, LSP popups, ghost/move preview) paints an opaque
  `surface`/`panel` before compositing, so nothing shows the terminal background.
  Pane bodies must pad every line to full width/height so no cell is left unpainted.
- **24-bit, graceful degrade.** Palettes authored as hex; lipgloss adapts to the
  terminal's depth. No hand-rolled 256-color fallbacks.
- **Live switch, no restart.** A config reload emits the existing
  `ConfigReloadedMsg`; the root rebuilds the palette once and re-threads it. No
  global mutable color singleton read mid-render.
- **Extension without edits.** New palettes register through the plugin registry,
  not by editing `builtins.go`. Built-ins are just the first provider.
- **`dark` follows the palette.** The resolved theme's own `dark` flag wins over
  the advisory config `dark` for any light/dark decision.

## Built-in palettes (initial set)

Ported from the proven sqlit / Textual set so users get good defaults day one:

- `default` — today's colors (current `defaultCaptures`, `defaultColors`, and the
  chrome literals) re-expressed as one palette; visually unchanged.
- `tokyo-night` (dark) — bg `#1A1B26`, surface `#24283B`, panel `#414868`,
  fg `#a9b1d6`, accent `#7FA1DE`.
- `nord` (dark).
- `gruvbox` (dark) + `gruvbox-light`.
- `rose-pine` (dark) + `rose-pine-dawn` (light).
- `catppuccin-mocha` (dark) + `catppuccin-latte` (light).

Each is one `Theme{}` literal in `builtins.go` supplying ui + captures + files.

## Milestones

- [ ] `internal/theme` skeleton: `Theme` (ui slots + `captures` + `files` + `dark`) and a `Palette` type with cached lipgloss styles.
- [ ] `resolve.go`: single color-token resolver (name/hex/ANSI); delete the duplicated `namedColors`+resolver from `highlight/theme.go` and `explorer/colors.go`, route both through it.
- [ ] `default` palette: capture today's `defaultCaptures`, `defaultColors`, and chrome literals verbatim — pixel-identical to current IKE.
- [ ] Additional built-ins: `tokyo-night`, `nord`, `gruvbox`(+light), `rose-pine`(+dawn), `catppuccin-mocha`(+latte).
- [ ] `registry.go`: name→`Theme` lookup with fallback-to-`default`+warning; merge plugin-registered themes.
- [ ] Plugin contract: additive `Capabilities.Themes []Theme`; ship built-ins as a compile-in theme plugin registered in `init()`.
- [ ] Wire the selector: read `config.Get().Theme.Name`, resolve a `*Palette`, thread from `internal/app` into panes.
- [ ] Feed existing color models: `highlight.NewTheme` and explorer `colorTable` take their **defaults** from the palette's `captures`/`files`; `theme.captures.*` and `[explorer.colors]` still override.
- [ ] Migrate chrome off literals: `internal/app`, `internal/explorer`, `internal/editor` (status bar, borders, selection, scrollbars, LSP popups/diagnostics) read ui slots.
- [ ] Fix the background bleed: paint each surface's own rectangle — pane bodies fill `surface` and pad lines to full width/height; dividers/gaps fill `background`; the floating shell, command palette, and LSP popups paint an opaque `surface`/`panel` before compositing. No cell shows the terminal background.
- [ ] Live re-theme: rebuild the palette on `ConfigReloadedMsg` and re-render.
- [ ] Validation + tests: every built-in defines every required ui slot; missing → fallback + diagnostic. Table tests for lookup, fallback, and override precedence (palette < config).
- [ ] Wiki: update `architecture/themes.md` — model, sqlit/Textual lineage, the three color groups, selection, built-in list, plugin registration.

## Out of scope

- Tree-sitter grammar / capture *classification* and the highlight pipeline — owned by **0100/0105**; this roadmap only supplies capture-color **defaults**.
- Explorer file-color *resolution logic* (glob/ext ordering) — owned by **0050**; this roadmap only supplies file-color **defaults** + the shared resolver.
- A theme-picker UI / live preview in the command palette (0070) — later UX pass; selection here is config-driven.
- User-authored theme files on disk as a first-class feature; supported path is a config name + registered palettes.
- Writing the selected theme back to config — write path lives with 0040/0090.
