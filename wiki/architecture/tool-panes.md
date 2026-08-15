---
type: concept
title: Custom TUI Tool Panes
description: "#741 — user-configured TUI programs (lazygit, htop, k9s) as first-class panes: [[tools.custom]] config entries become tool.<name> palette commands with toggle-focus semantics, configurable home positions (#1889 JetBrains-style docking), global process-wide instances shared across workspaces (#1890), tool chrome (not terminal chrome), exit keeps the pane open with restart/close footer actions (#810), layout restore, and IKE_THEME_* env for theme following."
resource: internal/app/tools.go
tags: [architecture, tools, terminal, panes, lazygit]
timestamp: 2026-08-14T00:00:00Z
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
placement = ""          # home position: left/right/top/bottom; empty = adaptive
multiple = false        # concurrent instances via tool.<slug>.new (#835)
global = false          # one process-wide instance shared across workspaces (#1890)
```

`placement` is the tool's **configured home position** (#1889, JetBrains-style
docking): one of `left`, `right`, `top`, `bottom` names the workspace edge the
tool docks against when it opens; empty (the default) keeps the adaptive
`auxZone` heuristic (#1588: the split direction adapts to the host pane's
shape, below). Any other value — including pre-#1588 legacy values, when the
key briefly meant a split direction — degrades to the adaptive default with a
config diagnostic. Because placement lives in the user config (intent), not in
`.ike/layout.json` (state), normal layout saves never clobber it. See **Home
positions** below for the open semantics.

Defined in `internal/config/schema.go` (`Tools`/`ToolEntry`). Entries missing
`name` or `command` are skipped.

**Preconfigured default (#750):** when `lazygit` is on PATH the default layer
(`internal/config/defaults.go`) ships exactly the entry above, so
`tool.lazygit` works with zero configuration — the delegated home of the git
workflow (staging, commits, branches, log) after the native VCS tool window
slimmed to a read-only changes list. No hard dependency: without the binary
the default is omitted and the setup dialog below offers the install. A
user-defined `[[tools.custom]]` list overrides the default wholesale.

Editable from the UI via **Settings → Tools** (#755,
`internal/settings/tools_page.go`): `a` adds, enter edits, `d` deletes; the
form validates name/command presence, duplicate names, and the placement
values (#1889).
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

Invoking mirrors `terminal.toggle`: no pane → spawn at the configured home
position (#1889, below) or, without one, split off the active editor at the
adaptive placement (`Model.auxZone`, #1588 — below by default, to the right
when the host is wider than 120 cells and wider than tall); pane exists
unfocused → focus it (remembering where focus was); focused → return focus.

## Home positions (#1889)

A tool with a `placement` opens at its home dock slot instead of the adaptive
split (`Model.openToolAtHome`, `internal/app/tools.go`):

- **Free slot** — the tool docks **full-span** against the configured edge
  (`layout.DockNew`, the create counterpart of the #811 `layout.Dock`),
  taking `toolDockShare` (0.3) of the workspace along the dock axis.
- **Occupied slot** — `Model.dockOccupant` probes the edge via
  `layout.EdgeLeaf` (the lone leaf pinned against the workspace edge through
  same-orientation splits; a subdivided or shared edge counts as free). When
  the occupant can host tabs (a terminal/tool pane converts via
  `ConvertToTabHost`, #836), the tool joins its tab list as a **focused tab**
  (`Registry.NewToolSession` + `AddTerminalTab`) instead of forcing another
  split. An editor showing documents at the edge is main content, not a dock
  occupant — the tool docks beside it.
- **Non-tabbable occupant** (explorer, singleton tool windows) — the tool
  stacks into the same dock via a perpendicular split: side docks stack
  vertically (occupant above), top/bottom strips split side by side.

Placement is **intent, not state**: the `Move`/`Dock` drag mechanics stay
untouched and never rewrite it, so a moved tool returns to its configured home
on the next close + reopen. Where the tool currently *is* keeps persisting
through `.ike/layout.json` as before — a restored session resumes the moved
position; the home only applies to fresh opens. Tools without a placement
keep the pre-#1889 behavior exactly.

### Instances (#835)

One instance per tool by default — the toggle finds the tool wherever it
lives, dedicated pane **or** editor-hosted terminal tab (a tool moved into a
tab list via the #708 center drop; focusing a tab-hosted tool activates its
tab), so `tool.<name>` never spawns a duplicate.

`multiple = true` on the entry opts the tool into concurrent instances (e.g.
several embedded `claude` sessions): a second command
**`tool.<slug>.new`** ("Tool: NAME (New Instance)") spawns another pane,
while the plain command keeps its toggle semantics targeting the **most
recently opened/focused instance** (session-local recency). `New` on a
single-instance tool degrades to the plain toggle. Layout persistence
saves each instance as its own `{kind: "tool"}` slot; restore restarts one
process per slot. The `multiple` field is editable in the Settings → Tools
form (validated `true`/`false`, listed with a `· multi` marker).

### Global instances (#1890)

`global = true` marks the tool as **one instance for the whole IKE process**,
shared across every workspace — an SQL client or an embedded agent session
keeps its connections, scrollback and state while projects come and go.
Implemented in `internal/app/tools_global.go`:

- **Owner** — the live session's owner is the **workspace manager**
  (`workspace.Manager`, which survives model rebuilds), never a workspace's
  pane registry. While attached, the pane lives in the active workspace like
  any tool pane; on every project switch `detachGlobalTools` (called by
  `performSwitch` right after the chdir succeeds) removes the pane from the
  departing workspace's tree and registry **without ending the session** and
  parks the terminal model on the manager (`ParkGlobalTool`), flagged parked
  (#1522) so an unrendered session stops requesting repaints.
- **Re-attach** — `tool.<name>` in any workspace first toggles a locally
  attached instance; otherwise it takes the parked session
  (`TakeGlobalTool`) and splices it in as a dedicated pane
  (`attachGlobalTool`) at the home position or adaptive placement. Global
  tools never tab-host into a dock occupant — a dedicated pane detaches
  wholesale on the next switch. Only with no live session anywhere does the
  command spawn a fresh process.
- **Lifecycle** — parked workspaces never contain a global tool, so workspace
  switch, project close (#1355), close-from-list (#820) and LRU eviction
  (#780) cannot end it; it also gates none of those guards. It ends only when
  its pane is closed explicitly (ctrl+w on the exit dialog), when its process
  exits while detached (the `ExitedMsg` reaps the stashed session), or at
  quit — `Model.quit` closes parked global sessions alongside the active
  registry's terminals, so no process outlives IKE.
- **Cwd** — resolves once, at first spawn, against the project active then;
  the shared session keeps its working directory across projects.
- **Persistence** — the workspace currently hosting the pane records it in
  its `.ike/layout.json` as usual. On restore, a saved global tool slot first
  re-attaches a live parked session (`Registry.AdoptToolKey`) — the revisit
  of an evicted workspace — and only spawns a fresh process when none is
  parked (the restart case).

`global` and `multiple` are **mutually exclusive**: a config declaring both
gets a `tools.custom.multiple` diagnostic and `multiple` is ignored
(`internal/config/validate.go`).

The `global` field is editable in the Settings → Tools form (#1895, validated
`true`/`false`, listed with a `· global` marker). There the mutual exclusion
is a **hard rejection** rather than a silent drop: saving an entry with both
flags set fails with `global and multiple are mutually exclusive`.

## Pane behavior

The pane reuses the terminal machinery (`Registry.AddTool` wraps a command
session, `terminal.Model.SetTool` marks it) but is deliberately **not chromed
as a terminal**: the title is `⚙ NAME` (no shell, directory, OSC title or
interpreter mappings) and the statusline names the tool the same way.

When the program exits the pane **stays open** (#810), keeping its layout
slot: the last output remains visible with a **centered exit dialog**
composited on top — `<name> exited (code N)` plus the `[ Restart (r) ]` and
`[ Close (ctrl+w) ]` buttons (accent-styled, prominent even in fullscreen).
`r` or clicking the restart button reruns the configured command in place
with the same directory and environment; `ctrl+w` or the close button
removes the pane. A pane too small for the dialog falls back to a one-line
footer with the same actions. Run command sessions keep their existing
stay-open behavior; plain shell terminals still close on exit.

## Layout persistence

`saveLayout` persists the identity `{kind: "tool", tool: <name>}`; restore
restarts the configured program in the saved position (`AddToolKey`), like
terminals respawn fresh shells. A tool no longer configured degrades to a
fresh shell in that slot rather than breaking the layout.

A tool hosted as an **editor tab** — after a center-drop move (#708/#836) —
persists through the hosting pane's editor identity instead: its name lands
in the identity's `tools` list and restore restarts it as a fresh tab
(`Registry.NewToolSession`); an unconfigured name restores as nothing. A
tool **pane** that became a tab host itself (#836,
`Instance.ConvertToTabHost`) follows the same editor-identity path.

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
