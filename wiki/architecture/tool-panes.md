---
type: concept
title: Custom TUI Tool Panes
description: "#741 — user-configured TUI programs (lazygit, htop, k9s) as first-class panes: [[tools.custom]] config entries become tool.<name> palette commands with toggle-focus semantics, configurable home positions (#1889 JetBrains-style docking), named slot templates pinning tools to exact layout positions (#1897), global process-wide instances shared across workspaces (#1890) whose panes follow project switches (#1903), tool chrome (not terminal chrome), exit keeps the pane open with restart/close footer actions (#810), layout restore, and IKE_THEME_* env for theme following."
resource: internal/app/tools.go
tags: [architecture, tools, terminal, panes, lazygit]
timestamp: 2026-08-15T00:00:00Z
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
split (`Model.openToolAtHome`, `internal/app/tools.go`). A slot assignment
(**Slot templates**, below) takes precedence over the placement — the edge
docking only applies to tools without a slot:

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
  `performSwitch` right after the chdir succeeds) removes the session from
  the departing workspace **without ending it** and parks the terminal model
  on the manager (`ParkGlobalTool`), flagged parked (#1522) so an unrendered
  session stops requesting repaints. A dedicated pane leaves the tree and
  the registry whole; a session hosted as a tab (#1901: a shared slot pane,
  or a #708 center drop) detaches **as a tab**, the host keeping its other
  tabs — a host carrying nothing but global tool sessions detaches whole,
  its leaf leaving the tree like a dedicated pane's.
- **Follows the switch (#1903)** — the pane is visible in **every project
  view** while the tool is open: at the end of `performSwitch`, after the
  rebuild, `attachOpenGlobalTools` splices every session still parked into
  the incoming workspace through the normal open path — the slot rule
  (#1897, tab-join when the slot pane is occupied, #1901), else the home
  placement (#1889), else the adaptive split — **without moving focus**.
  This covers both a resumed parked workspace and a first-visit build from
  `layout.json`; a restore that already re-attached the session from the
  saved layout leaves nothing parked, so no duplicate can arise (first one
  wins). A tool the incoming project's config does not declare global stays
  parked.
- **Re-attach on demand** — `tool.<name>` in any workspace first toggles a
  locally attached instance; otherwise it takes the parked session
  (`TakeGlobalTool`) and splices it in (`attachGlobalTool`): a slot
  assignment pins it, an occupied tab-capable slot pane taking the live
  session as a **focused tab** (#1901, the same tab-in-slot rule as a fresh
  open); otherwise a dedicated pane at the home position or adaptive
  placement — outside its slot a global tool never tab-hosts into a dock
  occupant, so the next switch can detach it wholesale. Only with no live
  session anywhere does the command spawn a fresh process.
- **Lifecycle** — parked workspaces never contain a global tool, so workspace
  switch, project close (#1355), close-from-list (#820) and LRU eviction
  (#780) cannot end it; it also gates none of those guards. It ends only when
  its pane is closed explicitly (ctrl+w / the exit dialog's ✕) — which ends
  it **everywhere**: the close is recorded on the manager
  (`MarkGlobalToolClosed`), so another project's stale `layout.json` entry
  restores as nothing instead of resurrecting the tool; any explicit reopen
  (`tool.<name>`, a layout apply) clears the record — or at quit —
  `Model.quit` closes parked global sessions alongside the active registry's
  terminals, so no process outlives IKE.
- **Exit while parked (#1903)** — a process that ends while the session is
  detached is *not* reaped: the dead session stays stashed with its exit
  status, and the next switch-in (or `tool.<name>`) materializes the pane in
  its usual position showing the standard `<name> exited (code N)` overlay
  (#810) with the `Restart`/`Close` footer actions — `Restart` reruns the
  command in place as a still-global session, `Close` removes the pane
  everywhere like any explicit close.
- **Cwd** — resolves once, at first spawn, against the project active then;
  the shared session keeps its working directory across projects.
- **Persistence** — the workspace currently hosting the session records it
  in its `.ike/layout.json` as usual: a dedicated pane as its `tool` leaf, a
  tab-hosted session in the host's editor identity `tools` list (kind-only,
  like any slotted tool tab). On restore, either shape first re-attaches a
  live parked session (`Registry.AdoptToolKey` for panes,
  `restoredToolSession` for tabs) — the revisit of an evicted workspace —
  and only spawns a fresh process when none is parked (the restart case) —
  unless the tool was explicitly closed since the layout was saved (#1903):
  a stale entry then prunes (dedicated leaf) or restores as nothing (tab),
  because the manager, not the per-project `layout.json`, is the authority
  on whether a global tool is open.

`global` and `multiple` are **mutually exclusive**: a config declaring both
gets a `tools.custom.multiple` diagnostic and `multiple` is ignored
(`internal/config/validate.go`).

The `global` field is editable in the Settings → Tools form (#1895, validated
`true`/`false`, listed with a `· global` marker). There the mutual exclusion
is a **hard rejection** rather than a silent drop: saving an entry with both
flags set fails with `global and multiple are mutually exclusive`.

## Slot templates (#1897)

Where #1889's `placement` names an edge and leaves the geometry to dock
heuristics, a **slot template** pins tools to **exact positions**: the user
declares a named layout once (in the spirit of CSS `grid-template-areas` /
i3 layouts) and every assigned tool always opens at its slot, with
predictable proportions — including how two tools share a region when both
are open.

```toml
[tools.layout]
template = ["XEEH", "XEEH", "TTZZ"]
assign   = ["X=explorer", "T=lazygit", "Z=problems", "H=structure"]
```

`template` is an ASCII grid, one string per row: every cell names a slot by
a single rune, the cells of one rune must form a **solid rectangle**, `E` is
the reserved **editor region**, and the arrangement must decompose into
straight full-width/full-height cuts (a slicing layout — a pinwheel of four
slots around a center cannot be expressed as a split tree and is rejected
with a `tools.layout.template` diagnostic, which disables slot placement
wholesale). Row/column counts set the proportions: above, `X` takes a
quarter of the width over the top two thirds, the `T`/`Z` strip the bottom
third. `assign` maps tools onto slots as `SLOT=tool` entries; the tool is a
`[[tools.custom]]` name **or a built-in tool-window id** (`explorer`, `vcs`,
`debug`, `problems`, `structure`, `usages`, `http`, `breakpoints`). Each
tool takes at most one slot; unknown slots, the editor region and duplicates
are dropped with diagnostics (`internal/config/validate.go`). Both keys are
editable via **Settings → Tool Layout** (schema `List` entries, per the
#1895 policy).

The engine (`internal/layout/slots.go`) parses the grid into a **slot tree**
— a binary split tree over slot names, derived by guillotine cuts, every
ratio a cell fraction — so slots materialize through the ordinary
`Split`/`Leaf` machinery:

- **Open** (`Model.openToolAtSlot` / `insertToolPane`,
  `internal/app/tool_slots.go`) — a free slot **rebuilds the outer slot
  structure deterministically** (`materializeSlot`): the currently slotted
  panes peel off the live tree (`layout.RemoveLeaves`), the remaining editor
  region — its own splits untouched — is grafted into the template's `E`
  position, and every resident lands at its slot's template position
  (`Template.BuildTree`). No `dockOccupant`/`auxZone` heuristics apply.
- **Collapse / expand** — slots of closed tools are pruned from the built
  tree, their space absorbed by the surviving sibling region; with the
  template above, opening `T` alone spans the full bottom strip, opening `Z`
  splits the strip into the defined `TT`/`ZZ` halves. Closing needs no slot
  code at all: the ordinary leaf-close collapse hands the space back, and
  the next open re-materializes the defined arrangement.
- **Several tools per slot → tabs** — a slot already held by a tab-capable
  pane adds the next assigned tool as a **focused tab**
  (`ConvertToTabHost`/`AddTerminalTab`, #836) instead of splitting —
  **global tools included** (#1901: the workspace switch extracts a hosted
  global session tab-wise, see the #1890 section). A non-tabbable holder (a
  singleton panel) subdivides the slot along its longer template axis —
  deterministic from the template alone.
- **Residency is derived, never stored** — a slot's occupant is recognized
  by what the pane hosts (a tool assigned to the slot; singleton panels by
  their key; tab hosts carrying only tool sessions). `.ike/layout.json`
  round-trips a slotted arrangement verbatim with no new persisted state,
  and a re-attached global tool lands in its slot (`attachGlobalTool`),
  tab-joining an occupied one (#1901).
- **Saved window layouts cooperate (#1899)** — applying a saved layout
  (#1175) or the default (`window.restoreLayout`) while a template is active
  keeps the slot config **authoritative**: slotted leaves are pruned from
  the snapshot and re-materialize through the slot engine
  (`internal/app/layouts_slots.go`), tabs-per-slot included, and live
  slotted panes survive the apply in their slots instead of grafting into
  the layout's flexible region. Snapshot identities are kind-only, so the
  tool name joins against the *current* `assign` map — a mismatch between
  save-time and apply-time config resolves to "current slot config wins",
  the snapshot position remaining a hint for non-slotted panes only. After
  any apply the derived residency matches the tree by construction, so
  open/close/collapse/expand keep working exactly as specified here.

Built-in tool windows honor their assignment through the shared
`insertToolPane` tail of every panel opener; the explorer participates as a
resident (materializing any slot snaps it to its column), its initial
position still coming from the default/persisted layout. Tools and panels
**without** an assignment — and everything when `template` is empty — keep
the #1889 home-position / adaptive behavior exactly.

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
