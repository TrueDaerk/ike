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

## Later sub-issues

Settings panel framework (#91), core pages (#92), keymap editor (#93),
toolchain page (#94).
