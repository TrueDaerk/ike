---
type: concept
title: Settings UI & Menu Bar
description: Roadmap 0160 — the menu bar over the command registry; the settings panel (pages, schema-driven forms) lands in later sub-issues.
resource: internal/menu
tags: [architecture, menu, settings, ui, commands]
timestamp: 2026-07-21T00:00:00Z
---

# Settings UI & Menu Bar

Roadmap 0160. File-based settings stay the source of truth; this stream adds a
JetBrains-like discovery layer: a **menu bar** and (in later sub-issues) a
**settings panel** whose changes persist through the config
[write-back layer](./config.md) and hot-reload.

## Menu bar (#90)

`internal/menu` renders the top row — File · Edit · View · Navigate · Tools ·
Settings · Help — above the pane tree (the layout's `bodyRect` starts one row
lower; `ui.menu_bar = false` hides it and returns the row).

- **Menus are data.** Every entry references a registered command id
  (`menu.Defaults`). The app resolves each id through the registry: registered
  entries show the same shortcut the cheatsheet shows (`registry.Binding`,
  falling back to the command's doc hint); unregistered ids render **disabled**
  with the blocked-ledger dependency (or "not available yet") as the hint.
  There is no parallel dispatch: selecting an entry emits `menu.RunMsg`, which
  the root model feeds into `RunCommand`.
- **Keyboard:** `f10` (command `menu.open`) toggles the first menu; while a
  dropdown is open the menu owns the keys — ←/→ switch menus, ↑/↓ navigate
  (skipping disabled entries, wrapping), enter runs, esc closes. Pressing a
  title's first letter jumps to (and opens) that menu, case-insensitively
  (duplicate letters cycle forward); while open, the bar underlines each
  title's first letter as the hint.
- **Mouse:** clicking a title on the bar row opens/switches that menu; clicking
  an entry runs it; clicking elsewhere closes the dropdown. Moving the mouse
  over a dropdown entry selects it (hover follows focus; disabled entries are
  skipped, like keyboard navigation).
- **Rendering:** the dropdown is an overlay (`overlay.Place`) below the bar,
  framed by a rounded border so it separates from the content it floats over,
  never disturbing the pane layout. Hit-testing accounts for the border: the
  first entry sits on row 2, one column inside the frame.

## Settings panel framework (#91)

`internal/settings` is a centered **floating panel** (#115): a rounded-border
box capped at ~110×32 cells above the workspace, category list left, form
right, opened via `settings.open` (cmd+, / menu bar / palette).

- **Schema-driven.** A `Page` is a titled list of `Entry` descriptors — config
  key, control type (`Bool`/`Int`/`String`/`Enum`/`Path`/`Chord`), write scope,
  title, description, enum options, int bounds. The form renders from the
  descriptor; there are no hand-built page UIs.
- **Apply-on-change, single source of truth.** The panel never caches values:
  every render reads `config.Get().Flat()`, and every edit returns a
  `config.WriteAndReload` command — the write-back layer persists the key and
  the reload pipeline re-applies it. Bool toggles apply on enter; Enum opens a
  **picker list** on enter (↑↓ move, enter commits, esc cancels) while →/l on
  the row quick-cycles to the next option (wrapping) without opening it
  (#383); ← never cycles — it always returns to the category column, the
  mirror of → (#533); Int/String/Path
  open an inline input (int parses + clamps to bounds, path validates
  existence); Chord captures the next key press. The pinned footer is **two lines** and
  the description (plus its `(key)` suffix) **word-wraps** across them (#549)
  — long help stays readable instead of clipping; an overflow beyond two
  lines is marked with an ellipsis, a validation error takes the first line
  and the wrapped description continues below. Path inputs get shell-style
  **tab completion** (#541) via the shared `internal/pathcomplete` engine:
  matching entries render as a suggestion list under the row (final path
  component only, capped with a `+N more` tail), tab extends the input to the
  longest unambiguous prefix — a single directory match completes with its
  trailing separator so repeated tab descends; `~` notation is preserved and
  matching falls back to case-insensitive (`~/dev` finds `~/Development`). The selected entry's
  description, key and validation error render in a **footer pinned to the
  bottom of the form column** — not inline under the row — so ↑↓ never shifts
  the other rows (#535); only the enum picker expands inline. The custom pages
  (Toolchain, Keymap, Language Servers, Tools, PHP Debug Mappings #832)
  follow the same layout via a shared
  `pinFooter` helper (#537): header line(s) pinned top, the list scrolls to
  follow the selection, and hints / failure details / env status / inline
  override inputs render in a constant-height footer pinned bottom. Custom-
  page footer lines word-wrap to the column width through the shared
  `wrapFooter` helper (#553) — Toolchain: two hint lines + status, Language
  Servers: three lines, Keymap: two — so long key hints stay readable on
  narrow windows instead of clipping mid-word.
- **Layer indicator + reset.** Each row shows `@default` / `@user` /
  `@project` (`config.Origin`); overridden values are tinted; `r` resets
  (RemoveAndReload — fall back through the layers).
- **Filter.** `/` starts a type-to-filter across all schema pages (titles,
  keys, page names); matches render as `Page › Title`, and the result list
  names the custom pages the filter cannot search (`(not searched: Keymap,
  …)`, #383). Esc clears the filter, then closes the panel.
- **Keys.** ↑↓/jk navigate, ←→ (and h/l) or tab switch columns, enter edits,
  esc cancels/closes (#383). On custom pages only arrow-left returns to the
  categories — plain `h` is forwarded to the page (it may be filter text
  there).
- **Scrolling.** Both columns scroll to follow the selection on short windows
  (a shared `follow` offset helper in `view.go`); nothing is hard-truncated
  out of reach (#383).
- **Focus clarity.** The focused column shows the vivid selection bar; the
  unfocused column keeps a dimmed (faint) selection background, so keyboard
  ownership is always visible (#383).
- **Mouse (#127, #673).** Clicking a category selects that page; clicking a
  form entry selects it, and a second click on the selection activates it
  (enter semantics). The wheel scrolls the column under the pointer by moving
  its selection (categories switch pages, form rows move like j/k). While an
  enum picker is open, clicking an option applies it and clicking anywhere
  else closes the picker; while an inline edit is active, a click on the row
  keeps the edit and a click elsewhere commits it (cancelling instead when
  the input does not validate — chord capture just cancels). Clicks outside
  the panel dismiss it (#116). Custom pages take part through optional
  `PageClicker` / `PageWheeler` interfaces on the `PageModel` seam (#674):
  the panel forwards form-column presses page-locally ((0,0) = the page's
  render origin) and wheel deltas via type assertion, so pages without the
  seams stay valid. All five custom pages implement them — click selects a
  row, a click on the selection performs the page's enter-equivalent action
  (Toolchain opens the picker and picker rows are clickable, Keymap starts
  the chord capture and the header row opens the filter, LSP toggles the
  per-server enable, Plugins/Marketplace toggle the detail expansion), and
  the wheel moves the selection (picker highlight / package window in their
  modes); clicks cancel modal captures/inputs instead of being swallowed.
- **Registry seam.** Plugins contribute pages via
  `Capabilities.SettingsPages`; the app appends `reg.SettingsPages()` to the
  built-in `settings.BasePages()` (the toolchain page #94 uses this).

## Resizing (#774)

`ctrl+shift+arrows` resize the open panel (width ±4, height ±1) unless the
panel is capturing keys verbatim (`Model.Capturing()`: an edit/pick/filter
input or a custom page's chord capture). The root model owns the chord: it
adjusts the shared `ui.WinSizes` store (kind `"settings"`, persisted in the
per-project `winsize.json`) and re-derives `settingsSize()`, which clamps
base+delta into the live terminal bounds.

## Page catalog (#92)

`settings.BasePages(themes)` ships the core pages; every entry carries a
description (the panel doubles as settings documentation), and a test fails on
any entry whose key the typed schema does not expose (no dead keys).

- **Editor** — tab width, use spaces, auto indent, auto save (focus|off,
  #174), trim trailing whitespace, insert final newline, line numbers
  (+relative), scroll offset, soft wrap, show whitespace: every key
  `applyConfig` reads live.
- **Appearance** — theme (enum fed from the registry's theme list; writing
  `theme.name` hot-reloads, so selection previews immediately), menu bar
  on/off, command-palette chord.
- **Files & Session** — restore last project, `files.watch`, `files.auto_reload`
  (clean|never, #81), `files.persistent_undo` (undo survives restarts, #148).
- **Backup** — crash recovery on/off (`backup.enable`; disabling purges existing
  snapshots), snapshot debounce (`backup.debounce_ms`), snapshot max age
  (`backup.max_age_days`) (#167, see [crash recovery](./crash-recovery.md)).
- **Notifications** — toast timeout, severity floor.

## Keymap page (#93)

A custom `PageModel` (the framework's seam for self-rendered pages, forwarded
every key while focused — verbatim during chord capture and while the `/`
filter input is open, #531). See
[Keybindings](./keybindings.md) for the full editor behavior: effective-table
listing with layer badges and blocked/fragile flags, capture-based rebinding
with conflict confirmation, unbind and reset-to-preset.

## Toolchain page (#94)

A custom `PageModel` listing every registered language with a server or
toolchain: effective interpreter (`lang.Interpreter` — explicit `[lang.<id>]
interpreter` beats detection), source badge (`@config`/`@detected`) and an
async version probe (`p`, `python --version` / `php -v` as `tea.Cmd`s routed
back via `settings.VersionMsg` → `Model.Deliver`). Enter opens the discovery
picker — Python: active venv, project `.venv`/`venv`, `uv python list`, pyenv
shims, PATH; PHP: PATH + common install locations; every language: the
versioned install directories (#675 — Homebrew `opt/<formula>[@*]/bin`,
pyenv `~/.pyenv/versions/*`, Go `~/sdk/go*`, newest first, deduplicated by
resolved path). The picker opens pre-selected on the currently effective
interpreter and probes every candidate's version eagerly, so versions render
without pressing `p` — plus a validated custom
path input with tab completion and a live suggestion list (#541, same
`internal/pathcomplete` engine as the schema Path entries). A choice writes
the **project** config and triggers `lsp.restart`
so servers respawn against the new interpreter; `r` resets to detection.
Python rows additionally show an environment **provenance** column
(`uv venv`/`venv`/`uv managed`/`pyenv`/`system`), `i` opens an inline
installed-packages listing (name + version, async, j/k scroll) and `n` runs
the guided environment-creation wizard (tool → Python → target directory) —
see [Language Registry](./languages.md) (#569).

## Language Servers page (0180, #130)

A custom `PageModel` contributed by the **LSP plugin** via
`plugin.Capabilities.SettingsPages` (`internal/settings/lsp_page.go`): one row
per registered language carrying a server — live status (`ready` / `idle` /
`crashed` / `missing` / `disabled` / `off (master)`, from language-tagged
`ServerStatusMsg`s the root model forwards via `Model.Deliver` plus the
manager's `RunningLangs`), the effective command line (config overlay over the
plugin baseline, mirroring the launch path) and the layer supplying it
(`@project`/`@user`/`@built-in`). Controls: `E` flips the `lsp.enabled` master
switch, `e` the per-server `lsp.servers.<id>.enabled`, `c`/`a`/`s` edit
command / args / settings (JSON object) overrides inline — written to the
**project** config via write-back, empty input resets the key — `x` clears all
of a server's overrides, `r` restarts one server (`Manager.StopLang`, async
per #123: work inside the returned `tea.Cmd`), `R` restarts all. A missing
binary renders the launch-failure reason; `i` runs the plugin's install
recipe manually and `A` toggles `lsp.auto_install` (#131 — the automatic
install on first use, with the manual action as fallback/retry). `I` toggles
`lsp.inlay_hints` (default off, #523), `S` toggles `lsp.signature_auto`
(the automatic signature popup on trigger characters; the manual
`lsp.parameterInfo` command works regardless) and `C` toggles
`lsp.completion_auto` (the as-you-type completion popup on identifier
characters, #527; server trigger characters and `ctrl+space` work
regardless), all shown in the header row.

## Marketplace page (0310, #446)

A custom `PageModel` (`internal/settings/marketplace_page.go`) over
`internal/market`: browse the plugin catalog, review a plugin's requested
capabilities, install/update/remove. Install (`i`) is only reachable from the
expanded detail (`enter`) where the full capability list renders — the trust
model's review step; `x` removes, `r` re-fetches. Async results arrive as
`MarketCatalogMsg`/`MarketActionMsg` through `Model.Deliver`; opening the
panel prefetches the catalog once. See
[Plugin Marketplace](./marketplace.md).
