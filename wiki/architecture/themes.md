---
type: concept
title: Themes / Color Schemes
description: Named-palette system — one [theme].name recolors syntax, explorer, chrome and the integrated terminal ANSI palette together; one shared color resolver; plugin-extensible built-ins.
resource: internal/theme
tags: [architecture, themes, color, lipgloss]
timestamp: 2026-08-28T18:00:00Z
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
`OccurrenceRead`, `OccurrenceWrite`, `InlayHint`, `Whitespace`, `IndentGuide`,
`Ruler`, `Primary`, `Secondary`,
`Success`, `Warning`, `Error`, `Info`, `Hint`, `MoveSource`, `DropTarget`,
`Ghost`, `ScrollbarTrack`, `ScrollbarThumb`, `DiffAdded`, `DiffRemoved`,
`DiffChanged`, `DiffAddedEmph`, `DiffRemovedEmph`. The occurrence slots back the LSP
document-highlight marks (#172); left empty they fall back to the theme's own
`SelectionMuted` before the default theme's, so occurrences stay in-palette
for sparse themes. `InlayHint` colours the inline LSP inlay-hint text (#171);
left empty it falls back to the theme's own `Border` — already a
legible-but-dim foreground in every palette (the builtins set it to their
comment tone). `Whitespace` and `IndentGuide` colour the visible-whitespace
glyphs and indent guides (#64), falling back to the theme's own `Border`;
`Ruler` is the column-ruler background tint, falling back to the theme's own
`Panel`. `DiffAdded`, `DiffRemoved`, and `DiffChanged` are the diff viewer's
line backgrounds and the editor's `.diff`/`.patch` word highlight (#60); left
empty they derive from the theme's own `Success`/`Error`/`Warning` tinted
toward its `Surface` via `theme.Mix`, so sparse themes get in-palette diff
colors without declaring the slots. `DiffAddedEmph` and `DiffRemovedEmph`
(#2170) are the diff viewer's **intra-line** changed-range backgrounds, one
per side, so an emphasized range stays in the colour of the line carrying it;
left empty they derive from that theme's own `DiffAdded`/`DiffRemoved` pushed
toward `Success`/`Error` and pulled back until they sit within `emphHeadroom`
of the line background's own drift from `Surface` — the readability envelope
the contrast audit holds every overlay to.

## Model

One `theme.Theme` bundles three color groups so a single name sets everything:

```
Name      "tokyo-night"
Dark      true
UI        semantic chrome slots (see above)
Captures  map[capture]color   defaults for internal/highlight (keyword, string …)
Files     map[glob|ext|filename]color  defaults for internal/explorer (dir, go, Makefile …)
Terminal  16 ANSI colors + terminal default fg/bg (#1363, all optional)
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

`[theme.captures]` is a **slot map** in the typed schema (`config.Theme.Captures`),
merged key by key across layers and exposed by `Config.Flat()` as
`theme.captures.<name>`. Until #1318 the table had no schema field at all: it
decoded into nothing, produced an *"unknown setting (ignored)"* diagnostic and
never reached `highlight.NewTheme` — the override path was live code with a
permanently empty input, invisible to the tests because they stubbed the
lookup.

Two things follow from capture names containing dots (`constant.builtin`,
`rainbow.0`):

- The **write-back layer** treats everything after a slot-map prefix
  (`theme.captures.`, `explorer.colors.`, `keymap.bindings.`) as one leaf key,
  so `theme.captures.constant.builtin` round-trips instead of being written as
  `[theme.captures.constant] builtin = …`, which would no longer decode.
- `highlight.NewThemeKeys(defaults, get, keys)` enumerates the config, so an
  override may name a capture the active theme does not define itself — a
  grammar-specific `function.builtin`. `NewTheme` (no enumeration) is the
  fallback for consumers without a config, e.g. the response viewer.

`[theme.captures]` has a settings UI: **Settings → Syntax Colors** (#1238) lists
every capture with a swatch, marks the overridden ones and writes user-scope
overrides — see [Settings UI](./settings-ui.md).

**Colour tokens are validated.** `theme.ValidToken` accepts a name from
`theme.Names()`, a `#rgb`/`#rrggbb` hex literal or an ANSI index 0–255;
anything else is dropped from `theme.captures` with a diagnostic, because
lipgloss silently renders an unparseable token as the terminal default and the
capture would just look unstyled.

## Terminal palette (#1363)

A theme also owns the **integrated terminal's 16 standard ANSI colors** plus
its default foreground/background. Without them a shell painting with SGR
30–37 / 90–97 resolves against the **outer** terminal's palette while its
background comes from IKE's own wash — a combination that regularly lands
unreadable. `internal/terminal` rewrites the grid's indexed colors to the
values resolved here; see [Terminal](./terminal.md).

```
Theme.Terminal.ANSI[0..15]   black, red, green, yellow, blue, magenta, cyan,
                             white, then the eight bright_* variants
Theme.Terminal.Foreground    terminal default foreground (SGR 39)
Theme.Terminal.Background    terminal default background (SGR 49)
```

Every field is optional; resolution fills the gaps:

- **Defaults**: `Foreground`/`Background` fall back to the theme's own
  `UI.Foreground` / `UI.Background` — the colors the frame's palette wash
  already paints, so the terminal matches the rest of the IDE by default.
- **Derived palette** (`deriveANSI`): the six hues come from the semantic slots
  the theme already tunes — error→red, success→green, warning→yellow,
  info→blue, accent→magenta, secondary→cyan — the greys are a
  foreground↔background ramp, and each bright variant is its base pulled toward
  the foreground. Because everything is expressed relative to the theme's own
  two poles, the same derivation works for dark and light themes, and any
  third-party theme gets a coherent palette for free.
- **Contrast floor**: every entry is lifted toward the foreground until it
  clears **3.0:1** against the terminal background (**1.7:1** for the four grey
  slots — black/white and their bright variants, one end of that ramp always
  sits near the background by design). The floor applies to a theme's *own*
  values too: several upstream palettes ship a "normal" half that is genuinely
  too dim to read, which is the complaint #1363 started from. A value that
  already clears the floor is kept byte-exact.
- **Built-ins**: `ansi_builtins.go` carries the upstream palettes of the themes
  whose project publishes one (default, tokyo-night, nord, gruvbox(+light),
  dracula, solarized-dark(+light), catppuccin-mocha(+latte), one-dark,
  github-dark(+light)). The remaining built-ins derive theirs — which is why
  that table is allowed to be partial.

`[theme.terminal]` overrides single slots by name, layered on the resolved
palette by `theme.ApplyTerminalConfig` (called from `internal/app`, since the
theme package itself never reads configuration):

```toml
[theme.terminal]
red        = "#ff5f5f"
bright_red = "#ff8787"
background = "#101010"
```

Slot names are validated like colour tokens: an unknown slot (`brightblue`) or
an unparseable colour is dropped with a diagnostic rather than silently doing
nothing. Unlike theme-shipped values, a user override is honoured verbatim —
the contrast floor does not second-guess an explicit choice.

## Structure

```
internal/theme/
  theme.go      UI slots, Theme, Palette (resolved colors), DefaultPalette()
  resolve.go    theme.Resolve — the ONE color-token resolver (name/hex/ANSI);
                replaced the duplicated copies in highlight and explorer
  ansi.go       terminal palette (#1363): Terminal, the 16 slot names, the
                derived fallback and the contrast floor
  ansi_builtins.go  the upstream ANSI palettes of the built-ins that publish one
  builtins.go   default, tokyo-night, nord, gruvbox(+light), rose-pine(+dawn),
                catppuccin-mocha(+latte), kanagawa, one-dark,
                solarized-dark(+light), dracula, darcula, intellij-light,
                everforest-dark(+light), ayu-dark(+mirage, +light),
                github-dark(+light), oxocarbon, monokai-pro, zenburn,
                high-contrast-dark(+light)
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

Two tests in `internal/theme/contrast_test.go` enforce readability over all
built-ins, so a theme that ships a dark-on-dark or light-on-light pair fails
CI. `TestBuiltinThemeContrast` is the original chrome spot-check (#384);
`TestBuiltinThemeFullContrast` walks the **whole matrix** — every text color a
theme can paint against every background it can land on.

**Three rules, in the order they matter:**

1. **Text clears WCAG AA (≥ 4.5:1)** on the base surfaces it renders over.
   Chrome text (`Foreground`, `Accent`, `Secondary`, `Success`, `Warning`,
   `Error`, `Info`, `Hint`, and the five `VCS*` status colors) is checked
   against `Background`, `Surface`, **and** `Panel`. Syntax `Captures` are
   checked against `Surface` (the editor body is the only place they paint);
   explorer `Files` colors against `Surface` and `Panel` (the hover row).
   `SelectionText` is checked against `Selection` and `Primary`.
2. **Deliberately dim text clears 3.5:1** — `InlayHint`, `VCSDeleted`,
   `capture:comment`, `capture:punctuation`, and `file:lock` are low-emphasis
   by design, but still have to be legible.
3. **Overlay backgrounds stay near `Surface`.** `SelectionMuted`, `Ruler`,
   `OccurrenceRead`/`OccurrenceWrite`, and `DiffAdded`/`DiffRemoved`/
   `DiffChanged` must be within **1.35:1** of `Surface`; the two "strong"
   backgrounds, `Selection` and `Primary`, within **1.5:1**. This is what
   makes rules 1–2 compose: panels that paint an overlay *keep the row's
   semantic foreground* (explorer file/VCS colors, problems severity colors,
   the editor's syntax colors under a visual selection), so an overlay that
   drifted far from `Surface` would swallow every color tuned against it.
   Holding the cap leaves any AA-clear text at ≥ 4.5/1.5 = **3.0:1** on an
   overlay, and dim text at ≥ 2.3:1 — the floor the test asserts.

Border/indicator slots (`Border`, `BorderFocus`, `Whitespace`, `IndentGuide`,
`MoveSource`, `DropTarget`, `Ghost`, scrollbars) are exempt — they never carry
text. When designing a theme, pick the darkest/lightest canonical shade of
each accent that clears the bar rather than inventing new hues, and pick a
*hued but luminance-close* `Selection` rather than a bright one: hue alone
reads as "selected" in a terminal.

### The strict tier: `high-contrast-dark` / `high-contrast-light` (#1229)

The thresholds above are a **floor**. Two built-ins treat them as a design
target instead and are audited against a stricter tier — `contrastTier` in
`contrast_test.go`, selected per theme name by `tierFor`, defaulting to
`baselineTier` so adding a strict theme can never tighten the other 26:

| | baseline | high-contrast tier |
|---|---|---|
| text on `Background`/`Surface`/`Panel`, captures on `Surface`, file colors on `Surface`/`Panel` | AA, 4.5:1 | **AAA, 7:1** |
| the dim class (`InlayHint`, `VCSDeleted`, `capture:comment`, `capture:punctuation`, `file:lock`) | 3.5:1 | **7:1 — no dim class** |
| overlays vs `Surface` | 1.35:1 (1.5:1 for `Selection`/`Primary`) | **1.2:1 for all of them** |
| text over an overlay | 2.3:1 | **5.8:1** |

**Dropping the dim class is the deliberate difference from every other
built-in.** Comments, punctuation, inlay hints and deleted-file rows carry the
same weight as body text here: the visual hierarchy that low-emphasis text
buys is traded for legibility, for low-vision users, bright ambient light and
low-quality projectors. The tighter overlay cap follows from the same goal —
at 7:1 text over a 1.2:1 overlay the worst pair in the theme is still ≈ 5.8:1,
so a selection or a diff line costs almost nothing.

Design consequence: with lightness pinned near the extremes (`Surface`
`#000000` / `#ffffff`, `Foreground` the inverse), **hue is the only channel
left** to tell `Error` from `Warning` from `Info`, so the two themes pick
widely separated hues — red, amber, green, cyan, blue, magenta — rather than
shades of one accent. The dark tier needs every foreground at relative
luminance ≥ ~0.33, the light tier at ≤ ~0.084; that is what fixes each hue to
its pale (dark theme) or deep (light theme) end.

Renderers must never pair a hardcoded color with a theme color: a
`Selection`/`Primary` background either sets `Foreground(SelectionText)`
explicitly or keeps a semantic palette foreground (terminal-default text on a
theme background was the source of issue #384).

## Built-in palettes

`default` (today's colors; the low-contrast diagnostic/selection slots were
lifted to AA contrast in #384), `tokyo-night`,
`nord`, `gruvbox`, `gruvbox-light`, `rose-pine`, `rose-pine-dawn`,
`catppuccin-mocha`, `catppuccin-latte`, `kanagawa` (wave variant of
rebelot/kanagawa.nvim; the darkest diagnostic shades are swapped for their
lighter siblings per the contrast rule), `one-dark` (Atom's One Dark; the
Error slot lightens the scheme's red to clear the contrast rule),
`solarized-dark` / `solarized-light` (Ethan Schoonover's Solarized; the
scheme's low-contrast accents are lightened/darkened on the diagnostic,
secondary, and syntax slots to clear the contrast rule), `dracula` (the
official Dracula spec, with the same lift applied where needed),
`darcula` / `intellij-light` (the JetBrains IntelliJ pair, #1228; Darcula's
mid-luminance accents are lightened on the chrome slots, IntelliJ Light's
gold/red darkened, and the strong JetBrains list/selection backgrounds
(`#4b6eaf`, `#a6d2ff`) are pulled toward `Surface` per rule 3 — the canonical
syntax anchors `#cc7832`/`#6a8759`/`#6897bb` and `#0033b3`/`#067d17`/
`#1750eb` are kept or nudged in lightness only).

The #1230 batch broadened the color space beyond the blue/violet cluster:
`everforest-dark` / `everforest-light` (sainnhe/everforest medium — the
green-dominant family; the light variant's accents darken heavily for AA),
`ayu-dark` / `ayu-mirage` / `ayu-light` (ayu-theme/ayu-colors — the
warm/orange family, with mirage as the mid-dark ground between the tiers;
alpha-composited selection/comment values are carried as their solid
equivalents), `github-dark` / `github-light` (GitHub Primer; the upstream
values are themselves contrast-audited — `github-light` passed the full
matrix with **zero** corrections), `oxocarbon` (IBM Carbon on true black,
base16 slot conventions; the Carbon palette has no yellow, so warm slots
reuse its pinks, noted per slot), `monokai-pro` (the default filter), and
`zenburn` (deliberately low-contrast by design and therefore the sharpest
test of the rules — its greens/reds needed the largest lightness lifts of
the batch while keeping the muted zenburn hues).

Across all built-ins the full-matrix audit lifted the low-emphasis
foregrounds (`InlayHint`, `VCSDeleted`, `file:lock`, comments), darkened the
light variants' syntax accents, and pulled every overlay background —
`Selection`, `Primary`, `SelectionMuted`, `Ruler`, the occurrence marks, and
the three diff tints — back toward `Surface` so semantic row colors survive
underneath them.

`high-contrast-dark` / `high-contrast-light` (#1229) are the accessibility
tier described above — near-black/near-white surfaces, AAA everywhere, no dim
class, 1.2:1 overlays. Select via:

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

The choice is **persisted as a user setting** (#667): `selectTheme` applies
the palette immediately and writes `theme.name` to the user-scope
`~/.ike/settings.toml` — the same write the Settings → Appearance page
performs — so the theme follows the user across projects and restarts. The
pre-#667 per-project session override (`session.json` field `theme`,
`Model.themeOverride`) is gone; a stale session entry is ignored at startup.
Config is the single source of truth: `reloadConfig` re-resolves and
re-threads the palette on every reload, so both the palette pick and a manual
`[theme].name` edit land the same way.

## Auto light/dark sync (#1480)

With `[theme].auto = true`, the theme follows the terminal background:
`Init` sends `tea.RequestBackgroundColor` (an OSC 11 query) and the reply
arrives as `tea.BackgroundColorMsg`, classified by `theme.IsDarkColor`
(linearised Rec. 709 relative luminance, dark below 0.5) into
`Model.termDark`. `resolveTheme` then picks `[theme].light` or
`[theme].dark` instead of `[theme].name`; both pair members resolve with the
same unknown-name fallback. A terminal that never answers simply never sends
the message — no timeout, no hang — and `termDark` stays nil, so
`[theme].name` keeps applying (startup renders it until the reply lands).
`themes.syncTerminal` ("Theme: Sync with Terminal Background") re-queries on
demand, e.g. after flipping the terminal's appearance. Explicit selection
wins: `themes.select.*` / the settings picker applies the choice and also
writes `theme.auto = false` in the same user-scope mutation batch. The
Appearance page exposes the switch plus the pair enums, partitioned by each
theme's `Dark` flag (`themeNamesByDark`). Auto is off by default
(`light = "intellij-light"`, `dark = "default"`); `theme.dark` repurposes the
key of a never-read pre-#1480 bool.

## Boundaries

- Tree-sitter capture *classification* + the highlight pipeline stay in
  0100/0105; this system supplies capture-color **defaults** only.
- Explorer file-color *resolution logic* stays in 0050; this system supplies
  file-color **defaults** + the shared resolver.
- A dedicated picker UI with live preview is a later UX pass; today's runtime
  switching is plain palette commands (see above), persistence is config-only.

The original plan and milestones lived in the former `roadmaps/0110-themes.md` (planning moved to GitHub issues; the file remains in git history).
