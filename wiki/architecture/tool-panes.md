---
type: concept
title: Custom TUI Tool Panes
description: "#741 — user-configured TUI programs (lazygit, htop, k9s) as first-class panes: [[tools.custom]] config entries become tool.<name> palette commands with toggle-focus semantics, tool chrome (not terminal chrome), exit-closes-pane, layout restore, and IKE_THEME_* env for theme following."
resource: internal/app/tools.go
tags: [architecture, tools, terminal, panes, lazygit]
timestamp: 2026-07-20T00:00:00Z
---

# Custom TUI Tool Panes (#741)

Users embed other TUIs as panes: each `[[tools.custom]]` config entry becomes
a palette command that opens a pane running the configured program directly —
no shell in between. The delegation target for the Git workflow surface
(#750: lazygit instead of a native VCS cockpit).

## Configuration

```toml
[[tools.custom]]
name = "lazygit"        # display name; command id becomes tool.lazygit
command = "lazygit"     # program to exec
args = []               # optional arguments
cwd = ""                # working directory; empty = project root
placement = "bottom"    # "bottom" (default) or "right" split
```

Defined in `internal/config/schema.go` (`Tools`/`ToolEntry`). Entries missing
`name` or `command` are skipped.

Editable from the UI via **Settings → Tools** (#755,
`internal/settings/tools_page.go`): `a` adds, enter edits, `d` deletes; the
form validates name/command presence, duplicate names and the placement enum.
Writes go through the write-back layer at user scope (the whole list, the
`project.history` pattern) and reload through the normal pipeline, so the
`tool.<name>` commands re-shape live.

## Commands & toggle semantics

`toolCommands()` (`internal/app/tools.go`) builds one command per entry on
every registry query — Capabilities is lazy, so a config reload re-shapes the
set live. The id is `tool.<slug>` (lower-case, non-alphanumerics collapse to
dashes: "My Tool" → `tool.my-tool`); like any command it is bindable and
palette-reachable.

Invoking mirrors `terminal.toggle`: no pane → spawn split below the active
editor (or right, per `placement`); pane exists unfocused → focus it
(remembering where focus was); focused → return focus. One instance per tool.

## Pane behavior

The pane reuses the terminal machinery (`Registry.AddTool` wraps a command
session, `terminal.Model.SetTool` marks it) but is deliberately **not chromed
as a terminal**: the title is `⚙ NAME` (no shell, directory, OSC title or
interpreter mappings) and the statusline names the tool the same way.

When the program exits the pane closes — quitting lazygit closes the pane
(unlike run command sessions, which stay open to show their output).

## Layout persistence

`saveLayout` persists the identity `{kind: "tool", tool: <name>}`; restore
restarts the configured program in the saved position (`AddToolKey`), like
terminals respawn fresh shells. A tool no longer configured degrades to a
fresh shell in that slot rather than breaking the layout.

## Theme following

The spawned process gets the toolchain env overlay every terminal gets, plus
`IKE_THEME_*` variables so a tool whose config can reference environment
values follows the IDE theme:

`IKE_THEME_NAME`, `IKE_THEME_DARK` (`true`/`false`), and `#rrggbb` values for
`BACKGROUND`, `FOREGROUND`, `ACCENT`, `SELECTION`, `BORDER`, `SUCCESS`,
`WARNING`, `ERROR`, `INFO`.

IKE never rewrites a tool's own config files; wiring the variables into e.g.
a lazygit theme config is the setup-step follow-ups' job (#751–#753).
