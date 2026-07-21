---
type: concept
title: Workspace
description: Per-project UI state unit (pane registry, split tree, terminal return-focus) behind a Manager — the Roadmap 0370 seam for seamless project switching.
resource: internal/workspace
tags: [architecture, workspace, project-switching, panes, layout]
timestamp: 2026-07-21T00:00:00Z
---

# Workspace

Roadmap 0370 (#776, M1). `internal/workspace` bundles the per-project UI
state the root model owns into one swappable unit:

- **`Workspace`** — `Root` (absolute project root; `""` in M1, where the
  process cwd is the root by convention), `Panes` (the `pane.Registry`
  backing every layout leaf), `Tree` (the pure split-tree layout), and
  `ReturnFocus` (the pane focused before `terminal.toggle` / a tool command
  moved focus).
- **`Manager`** — holds the **active** workspace (`Active`/`SetActive`)
  plus the **background set** (#777): `Park` moves the active workspace into
  a root-keyed map, `Resume(root)` pops it back, `Peek`/`Background` inspect
  it (LRU order, least-recently-used first) and `Drop` is the M4 eviction
  seam. Parked workspaces stay fully alive — PTY readers, run processes and
  debug bridges never depended on being rendered. `Workspace.Aux` carries
  app-owned live extras across the park (the debug session state).

## Root-model integration

`internal/app`'s `Model` no longer carries `panes`/`tree`/
`terminalReturnFocus` fields — it holds `ws *workspace.Manager` and reaches
the unit exclusively through `m.activeWS()` (`app.go`). Because the model is
copied by value on every bubbletea `Update`, the manager pointer is the seam
that keeps panes, tree and focus one shared unit across copies; a later
project switch swaps the whole workspace atomically instead of rebuilding
fields one by one.

## Seamless switching (#777)

`performSwitch` persists the old project's session/layout, chdirs, **parks**
the live workspace (debug state stashed in `Aux`) and rebuilds the model
through the fresh-start path with the manager carried over: a parked
workspace for the target root resumes exactly as left (layout/session
restore from disk is skipped), a first visit builds panes from the saved
layout as before. Consequences:

- **Dirty buffers no longer gate the switch** — they park with the
  workspace and come back unsaved; the unsaved-changes prompt returns as
  the M4 eviction guard (#780).
- **The #96 terminal adoption is retired**: terminals stay with their
  project and keep running in the background instead of following into the
  new workspace. Session routing keys carry a global sequence suffix
  (`internal/terminal`, `sessSeq`) so same-named pane keys in two
  workspaces can never cross-route Output/Exited messages — a background
  exit is simply ignored until the workspace resumes.
- **Background events are not applied**: a debug stop or terminal exit in a
  parked workspace waits until re-attach (the pane then shows its final
  state); nothing is torn down.

## Cap & eviction (#780)

`project.max_workspaces` (default 3, floor 1) bounds the background set.
After every switch `enforceWorkspaceCap` (`internal/app/workspace_evict.go`)
drops least-recently-used parked workspaces past the cap: an **idle** one
(no dirty buffers, no running terminal/tool/command sessions or tabs, no
parked debug session — `workspaceBusy`) tears down silently
(`teardownWorkspace` closes every terminal session and disconnects a parked
debug session; buffers need no teardown), a **busy** one opens the eviction
guard — `e` evicts, `esc` keeps it over the limit until the next switch
re-asks. This is the 0090 unsaved-changes prompt reborn at eviction time;
plain switching never prompts. Per-project layout/session persistence needs
no extra machinery: every workspace's layout is saved at park time, so an
evicted project restores from disk on its next visit like any first visit.

## Teardown & memory release (#825)

Every teardown path (close-from-list, busy-close guard, LRU eviction) runs
`closeWorkspace` (`internal/app/workspace_evict.go`): `teardownWorkspace`
closes every terminal session (PTY goroutines join synchronously in
`Session.Close`), disconnects a parked debug session, and then cuts the
workspace's references loose (`Panes`, `Tree`, `Aux` set to nil) so a
lingering `Workspace` pointer can never pin the registry. It then fires the
**`plugin.EventWorkspaceClosed`** hook (payload: the workspace's absolute
root; WASM ABI name `workspace_closed`). The LSP plugin subscribes
(`lsp.wsclose`): the bridge drops its per-path caches under that root and
`manager.CloseRoot` closes every document whose path lies inside the root
(didClose) and stops every server rooted there — the closed project's
server processes, document texts and semantic-token arrays are released;
the next visit respawns lazily. The recent-projects lists hold only entry
metadata (path/name/timestamp), never live workspace pointers.
Weak-pointer regression tests (`internal/app/workspace_release_test.go`)
assert the dropped `Workspace` and its `pane.Registry` become garbage.

## Marker & close-from-list (#820)

The recent-projects lists (the `project.switch` picker and the Recent Files
dialog's Recent Projects column) mark entries whose workspace is parked in
memory with a **`●` badge** and offer a close-in-place aux action rendered as
a right-pinned `✕`: `shift+delete` on the selected row or a click on the `✕`
zone emits `project.CloseWorkspaceMsg`, which tears the background workspace
down (`teardownWorkspace`) without switching — the palette stays open and
refreshes, the badge disappears, the history entry remains. The active
project refuses the action with an info toast. Manual close is the explicit
counterpart to LRU eviction.

## Busy-close & quit guards (#821)

Tearing down a workspace with live state asks first
(`internal/app/workspace_guard.go`):

- **Close-from-list** on a busy background workspace — running debug
  session, runs/tools, running shells, or dirty buffers
  (`collectActivity`, the detailed sibling of `workspaceBusy`) — opens a
  prompt summarising what is running: `s` saves the workspace's dirty
  buffers then closes (writes work without focus or rendering; a failed
  write cancels), `d` closes discarding, `esc` cancels untouched.
- **IDE quit** aggregates dirty buffers and running debug/run/tool activity
  across **every** in-memory workspace (active + parked, entries labeled
  with their project root). Idle interactive shells never gate the quit —
  every session has one open. `s` saves everywhere then quits, `d` quits
  discarding, `esc` cancels.
- **LRU eviction** keeps its own #780 guard (busy workspaces prompt, never
  evict silently).

## Working-directory invariant (#779)

**The process cwd always equals the active workspace's root.** Everything
root-derived resolves against `"."` (or `cachedGetwd`, invalidated on
switch) *at call time*, never at construction: new terminals, run configs,
the config project layer (`config.Discover(".")` keeps `ProjectRoot`
relative), palette file/dir walks, find-in-path, VCS detection, toolchain
shims and the status line. The audit tests
(`TestSwitchNewTerminalSpawnsInNewRoot`,
`TestResumeNewTerminalSpawnsInResumedRoot`,
`TestSwitchReAnchorsConfigLayer` in `internal/app`) pin the contract.
Existing background terminals are exempt by design: a session pins its
origin dir absolutely at spawn (`internal/terminal.startSession`), so a
parked shell never re-anchors.

## Boundaries

Everything not per-project (theme, registry, host, config options, overlay
models) stays on the root model. Per-project state that is *derived* (config
project layer, watcher root, LSP clients) is re-resolved on switch and is
not part of the unit — see the epic spec (#775) for the M2/M3 ownership
audit.
