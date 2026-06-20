# Roadmap 0110 — Themes / Color Schemes

Give IKE a single, typed **theme** system so every pane reads its colors from a
shared palette instead of hard-coding lipgloss color literals. A user selects a
theme by name in `settings.toml`; switching it re-colors the whole IDE without a
restart. We ship a set of well-known community palettes and follow an existing,
proven model rather than inventing one.

**Standard we follow.** We mirror the theme model used by
[sqlit](https://github.com/Maxteabag/sqlit), which itself reuses
[Textual's `Theme`](https://textual.textualize.io/guide/design/): a small flat
set of **semantic color slots** plus a `variables` extension map and a `dark`
flag — *not* a per-widget color sheet. Concretely a theme is:

```
name        string         // unique id, e.g. "tokyo-night"
primary     color          // main brand / selection / active accent
secondary   color          // secondary accent
accent      color          // links, focus rings, highlights
foreground  color          // default text
background  color          // app background
surface     color          // panel / pane body background
panel       color          // raised panel (title bars, status bar)
border      color          // pane borders, dividers
success     color
warning     color
error       color
dark        bool           // is this a dark theme?
variables   map[string]string  // extra named slots for downstream roadmaps
```

This is deliberately the same slot set as sqlit's built-ins (sqlit, gruvbox,
nord, tokyo-night, rose-pine, catppuccin, …), so their palettes port over almost
verbatim and users coming from sqlit/Textual find the names familiar.

## Prerequisites / Dependencies

- **0040 Settings** — owns the `[theme]` section and its selector
  (`name = "default"`, `dark = true`). This roadmap fills the **content** behind
  that selector: it does not own config loading, only registers theme defaults
  and reads `config.Get().Theme.Name`. A theme change surfaces as a `tea.Msg`
  (re-using 0040's `ConfigReloadedMsg` path) so the root model re-themes live.
- **0020 Plugins** — `internal/plugin` `Capabilities` and `internal/registry`.
  Themes are registered like every other capability (additive
  `Capabilities.Themes []Theme` field, same pattern `Command.Scope` set for
  07/08). Built-in palettes ship as a compile-in plugin; third-party plugins can
  add their own. `internal/registry` exposes a theme lookup by name.
- **0010 Foundation** — the root model in `internal/app` hosts every pane and is
  where the active `*theme.Theme` is threaded down. Re-theme is a routed
  `tea.Msg`, consistent with how the foundation routes other model updates.

Downstream **consumers** (do not block this roadmap, they adopt the palette):
all rendering code currently holding literals — `internal/app`,
`internal/explorer`, `internal/editor`, `internal/help`, `internal/ui`,
`internal/overlay`.

## Architecture

```
internal/theme/
  theme.go         Theme struct (semantic slots + variables + dark) + Color type
  palette.go       resolved Palette handed to renderers (lipgloss styles cached)
  builtins.go      built-in themes (default, tokyo-night, nord, gruvbox,
                   rose-pine, catppuccin-mocha, … ) ported from the sqlit set
  registry.go      name -> Theme lookup; merges registry-provided themes
  syntax.go        editor/TextArea syntax slot map (keyword/string/comment/…)
  load.go          (optional) decode a user theme file into a Theme
  theme_test.go    table tests: every builtin defines every required slot,
                   name lookup, fallback-to-default, contrast sanity
```

Data flow:

```
builtins.go ─┐
plugin themes ┼─► registry (by name) ─► theme.Resolve(name) ─► *Palette ─┐
user theme   ┘                                                           │
                                                                         ▼
config.Theme.Name ───────────────────────► internal/app root model ► panes
                                              (ReThemeMsg on change)
```

- `internal/theme` is **leaf-level** — it depends on lipgloss and nothing else
  in IKE, so any pane package can import it without a cycle (same rule as
  `internal/config`).
- Renderers receive a `*theme.Palette` (or pull semantic colors from it); they
  **never** write `lipgloss.Color("69")` literals anymore. The palette caches
  the lipgloss styles so re-render is cheap.
- Theme names cross the plugin/Wasm boundary as plain data (just the palette
  map), keeping 9900's Wasm bridge trivial.

## Design rules

- **Semantic, not literal.** Code asks for `palette.Accent()` /
  `palette.Border()`, never a raw color number. The current hard-coded literals
  (`"69"`, `"236"`, `"240"`, `"39"`, `"215"`, …) map onto semantic slots during
  the migration; the literal disappears.
- **Every theme defines every required slot.** A theme missing a required slot
  is a validation error → it falls back to `default` with a non-fatal
  diagnostic (bad theme must never crash the IDE — mirrors 0040's clamp-and-warn
  rule). `variables` are optional extras; a missing variable falls back to a
  sensible base slot.
- **One selector, lower-cased names.** `[theme] name = "tokyo-night"`. Unknown
  name → `default` + warning. `dark` in config is advisory; the resolved theme's
  own `dark` flag wins for any light/dark-dependent decision.
- **24-bit with graceful degradation.** Palettes are authored as hex
  (`#1A1B26`); lipgloss adapts to the terminal's color depth. Do not pre-bake
  256-color fallbacks by hand.
- **Live switch, no restart.** Changing the theme (config reload or a future
  command) emits a re-theme `tea.Msg`; the root rebuilds the palette once and
  re-threads it. No global mutable singleton read mid-render.
- **Extension without edits.** New palettes are added by registering a `Theme`
  through the plugin registry, not by editing `builtins.go`. Built-ins are just
  the first registered provider.
- **Syntax colors are a sub-slot, not a second system.** Editor token colors
  (keyword/string/comment/number/type/function) live in `syntax.go` as a named
  slot group derived from the active theme, so a theme switch recolors syntax
  too. Concrete tree-sitter/LSP token wiring stays with the editor roadmap (0060)
  — this roadmap owns the slot names + default mapping only.

## Built-in palettes (initial set)

Port from the proven sqlit / Textual set so users get good defaults day one:

- `default` — IKE's current colors, re-expressed as semantic slots (the
  migration target; visually unchanged).
- `tokyo-night` (dark) — bg `#1A1B26`, surface `#24283B`, panel `#414868`,
  fg `#a9b1d6`, accent `#7FA1DE`.
- `nord` (dark).
- `gruvbox` (dark) + `gruvbox-light`.
- `rose-pine` (dark) + `rose-pine-dawn` (light).
- `catppuccin-mocha` (dark) + `catppuccin-latte` (light).

Each is one `Theme{}` literal in `builtins.go`; hex values taken from the
upstream palette definitions.

## Milestones

- [ ] `internal/theme` skeleton: `Theme` struct (semantic slots + `variables` + `dark`) and a `Color`/`Palette` type with cached lipgloss styles.
- [ ] `builtins.go`: `default` theme expressing today's literal colors as semantic slots — visually identical to current IKE.
- [ ] Additional built-ins: `tokyo-night`, `nord`, `gruvbox`(+light), `rose-pine`(+dawn), `catppuccin-mocha`(+latte), ported from the sqlit/Textual palettes.
- [ ] `registry.go`: name→`Theme` lookup with fallback-to-`default`; merge themes registered via the plugin registry.
- [ ] Plugin contract: add additive `Capabilities.Themes []Theme`; ship built-ins as a compile-in theme plugin registered in `init()`.
- [ ] Wire selection: read `config.Get().Theme.Name`, resolve to a `*Palette`, thread it from the `internal/app` root into every pane.
- [ ] Migrate renderers off literals: `internal/app`, `internal/explorer`, `internal/editor`, `internal/help`, `internal/ui`, `internal/overlay` consume `*Palette`.
- [ ] Live re-theme: emit/route a re-theme `tea.Msg` on config reload; root rebuilds the palette once and re-renders.
- [ ] `syntax.go`: editor syntax slot group (keyword/string/comment/number/type/function) derived from the active theme; default mapping committed.
- [ ] Validation: every built-in defines every required slot; missing slot → fallback + non-fatal diagnostic. Table-driven tests.
- [ ] Wiki: document the theme model (semantic slots, the sqlit/Textual lineage), how to select a theme, the built-in list, and how a plugin registers a custom theme.

## Out of scope

- Per-language tree-sitter / LSP token classification and highlight wiring — **Roadmap 0060** (this roadmap owns the syntax *slot names* + default mapping only).
- A theme-picker UI / live preview palette in the command palette — a later UX pass (palette infra is 0070); selection here is config-driven.
- User-authored theme files on disk as a first-class feature (`load.go` is a thin optional hook); the supported path is config name + registered palettes.
- Writing the selected theme back to config — config write path lives with 0040/0090.
- Image/true-color terminal capability detection beyond what lipgloss already provides.
