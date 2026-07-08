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

`internal/settings` is the full-window panel: category list left, form right,
opened via `settings.open` (cmd+, / menu bar / palette).

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

## Later sub-issues

Core pages (#92), keymap editor (#93), toolchain page (#94).
