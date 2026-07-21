---
type: concept
title: Custom TUI Tool Panes
description: "#741 — user-configured TUI programs (lazygit, htop, k9s) as first-class panes: [[tools.custom]] config entries become tool.<name> palette commands with toggle-focus semantics, tool chrome (not terminal chrome), exit keeps the pane open with restart/close footer actions (#810), layout restore, and IKE_THEME_* env for theme following."
resource: internal/app/tools.go
tags: [architecture, tools, terminal, panes, lazygit]
timestamp: 2026-07-21T00:00:00Z
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

## Curated tool catalog & setup (#751–#753, #759)

`internal/toolcatalog` holds a curated list of common TUIs — lazygit,
lazydocker, sqlit (Maxteabag/sqlit, binary from the `sqlit-tui` Python
package), k9s, htop, btop — each with the `[[tools.custom]]` entry it maps
to, an optional requirement gate (`Requires`: lazydocker needs `docker`, k9s
needs `kubectl` on PATH to be offered) and ordered install recipes (plain
argvs like the LSP recipes: brew, `go install`, pipx/uv). `InstallArgv` picks
the first recipe whose installer is on PATH; `Install` runs it and
re-verifies the binary resolves afterwards (exit 0 without the binary on
PATH is a failure, the LSP #370 semantics), reporting a
`toolcatalog.InstallResultMsg` that the app toasts.

Two surfaces draw from the catalog:

- **Post-tour setup step** (`internal/app/tools_setup.go`) — the last step of
  the #713 setup flow and the `tools.setup` palette command ("Set Up Tool
  Panes"): a checkbox list of the offered entries not yet configured; enter
  writes the checked ones into `[[tools.custom]]` (user scope, palette
  commands live immediately) and installs missing binaries; esc skips. See
  the welcome-tour doc for the flow details.
- **Settings → Tools suggestions** (#759) — `s` on the Tools page opens the
  same catalog filtered to unconfigured entries with their install state;
  enter adds the entry (write-back + reload) and installs the binary when
  missing, esc returns to the list.

A failed install keeps the written config entry — the tool works as soon as
the binary is installed by hand.

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

When the program exits the pane **stays open** (#810), keeping its layout
slot: the last output remains visible and the footer row offers the two
actions — `[<name> exited (code N)]  [restart (r)]  [close (ctrl+w)]`.
`r` (or clicking `[restart (r)]`) reruns the configured command in place with
the same directory and environment; `ctrl+w` (or clicking `[close (ctrl+w)]`)
removes the pane. Run command sessions keep their existing stay-open
behavior; plain shell terminals still close on exit.

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

IKE never rewrites a tool's own config files; the setup surfaces (#751–#753,
#759) write only `[[tools.custom]]` entries and install binaries — wiring the
variables into e.g. a lazygit theme config stays the user's choice.
