---
type: concept
title: Settings UI & Menu Bar
description: Roadmap 0160 — the menu bar over the command registry; the settings panel (pages, schema-driven forms) lands in later sub-issues.
resource: internal/menu
tags: [architecture, menu, settings, ui, commands]
timestamp: 2026-07-08T00:00:00Z
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
  (skipping disabled entries, wrapping), enter runs, esc closes.
- **Mouse:** clicking a title on the bar row opens/switches that menu; clicking
  an entry runs it; clicking elsewhere closes the dropdown.
- **Rendering:** the dropdown is a plain overlay (`overlay.Place`) below the
  bar, never disturbing the pane layout.

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
  the reload pipeline re-applies it. Bool toggles and Enum cycles apply on
  enter; Int/String/Path open an inline input (int parses + clamps to bounds,
  path validates existence); Chord captures the next key press.
- **Layer indicator + reset.** Each row shows `@default` / `@user` /
  `@project` (`config.Origin`); overridden values are tinted; `r` resets
  (RemoveAndReload — fall back through the layers).
- **Filter.** `/` starts a type-to-filter across *all* pages (titles, keys,
  page names); matches render as `Page › Title`. Esc clears the filter, then
  closes the panel.
- **Keys.** ↑↓/jk navigate, tab switches columns, enter edits, esc
  cancels/closes.
- **Registry seam.** Plugins contribute pages via
  `Capabilities.SettingsPages`; the app appends `reg.SettingsPages()` to the
  built-in `settings.BasePages()` (the toolchain page #94 uses this).

## Page catalog (#92)

`settings.BasePages(themes)` ships the core pages; every entry carries a
description (the panel doubles as settings documentation), and a test fails on
any entry whose key the typed schema does not expose (no dead keys).

- **Editor** — tab width, use spaces, auto indent, trim trailing whitespace,
  insert final newline, line numbers (+relative), scroll offset, soft wrap,
  show whitespace: every key `applyConfig` reads live.
- **Appearance** — theme (enum fed from the registry's theme list; writing
  `theme.name` hot-reloads, so selection previews immediately), menu bar
  on/off, command-palette chord.
- **Files & Session** — restore last project, `files.watch`. Grows with 0140's
  `files.auto_reload` and auto-save (#54) as they land.
- **Notifications** — toast timeout, severity floor.

## Keymap page (#93)

A custom `PageModel` (the framework's seam for self-rendered pages, forwarded
every key while focused — verbatim during chord capture). See
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
shims, PATH; PHP: PATH + common install locations — plus a validated custom
path input. A choice writes the **project** config and triggers `lsp.restart`
so servers respawn against the new interpreter; `r` resets to detection.
