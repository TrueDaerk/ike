---
type: concept
title: Workspace
description: Per-project UI state unit (pane registry, split tree, terminal return-focus) behind a Manager — the Roadmap 0370 seam for seamless project switching.
resource: internal/workspace
tags: [architecture, workspace, project-switching, panes, layout]
timestamp: 2026-09-02T00:00:00Z
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
  app-owned live extras across the park (the debug session state, the popup
  terminal since #1407, and the project-owned floating terminal panels since
  #1793 — panels marked **global** never park: they ride into the fresh model
  with their sessions and survive every workspace teardown, see the terminal
  doc). Parked terminal sessions run in a cheaper
  ingest mode (#1522, `Session.SetParked`): output still lands in the grid
  and the (upstream-capped) scrollback, but no `OutputMsg` is sent — each
  one is a full program Update pass for a grid nobody renders — and the
  feed loop folds the available spool backlog into batched emulator passes
  (`parkedBatchMax`). Un-parking on resume delivers the one owed repaint
  per session. `performSwitch` flips the flag for terminal panes, editor
  terminal tabs and the popup via `setWorkspaceTerminalsParked`.

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
the live workspace (debug state and the popup terminal stashed in `Aux`,
#1407) and rebuilds the model
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
  state); nothing is torn down. Debug adapter events carry their owning
  `*dap.Session` (#1523), so a parked debuggee's events never touch the
  active workspace's session state: output routes into the owning
  workspace's transcript (`<root>/.ike/debug-session.log`) and its parked
  debug area's console (#2190, `parkedDebugConsole`) — or its `pendingOut`
  buffer, capped at
  `maxPendingOut` chunks — while state events (`stopped`, `continued`, …)
  are consumed without effect. The async stop/ended follow-up messages
  are session-guarded the same way. A session's output events also
  coalesce before `host.Send` (#1557, `debugEventCoalescer` in
  `internal/app/debugsession.go`) — parked or active, since #2176: they
  buffer and deliver as one `debugEventBatchMsg` per quiet window
  (`debugOutputQuiet`), so a chatty debuggee costs one Update pass per
  window instead of per event — the debug-side analogue of the terminal's
  batching; state events flush the buffer ahead of themselves
  and deliver individually. The exception is `terminated`/`exited`
  (#1544): a parked debuggee that ends finishes its session in place —
  the parked debug area flips to the finished state (and closes when
  `debug.session_end = close`, #2190), `extras.dbg` clears
  so the workspace stops counting as busy (silent LRU eviction works
  again, the close/quit guards stop reporting a phantom session), and the
  dead session's transport is released instead of parking until resume.

## Quick-peek workspaces (#2136)

A workspace opened via `project.peek` runs through the same switch/park
machinery but is marked on the **model** (`peekState` in
`internal/app/project_peek.go`, not on `workspace.Workspace`): the marker only
ever describes the active workspace and deliberately dies with the process.
`project.peek.return` resumes the origin and then `Drop`s + tears down the
peeked unit (the #820/#825 path) behind the #821-shaped busy guard; the peeked
project's open is never recorded into `project.history`, and its
session/layout are only written back when they changed since peek-enter. A
peek counts toward the #780 cap like any workspace; an origin evicted while
peeking comes back as a cold first visit. Full flow:
[project-switching](project-switching.md).

## Background LSP idle shutdown (#1521)

Language servers are the dominant per-workspace memory block and hold no
user-visible live state, so a parked workspace does not keep them forever:
`Park` stamps `Workspace.ParkedAt`, and `performSwitch` arms a one-shot
timer (`armWorkspaceIdle`, `internal/app/workspace_idle.go`) for
`project.background_lsp_timeout` (Go duration string; default `5m`;
`"off"`/`"0"`/`"false"` disables). Expiry re-validates instead of relying
on cancellation: the workspace must still be parked and its *current* park
must be at least a full timeout old — a resume-and-re-park moves
`ParkedAt`, so the stale timer falls through and the newer park's timer
takes over. A confirmed idle expiry fires `plugin.EventWorkspaceIdle`
(payload: root); the LSP bridge subscribes (`lsp.wsidle`) and releases its
per-path state under that root plus the manager's documents and servers via
`CloseRoot` — unlike `EventWorkspaceClosed` it leaves the global debounce
timers armed, since they belong to the active workspace. Respawn needs no
machinery: on resume, `Init` re-announces every open file
(`EventFileOpened`), and the first document event restarts the server
lazily.

## Cap & eviction (#780)

`project.max_workspaces` (default 3, floor 1) bounds the background set.
After every switch `enforceWorkspaceCap` (`internal/app/workspace_evict.go`)
drops least-recently-used parked workspaces past the cap — **idle ones
only**, silently: no dirty buffers, no running terminal/tool/command
sessions or tabs, no parked debug session, no running popup-terminal session
(#1407) — `workspaceBusy` — tears down (`teardownWorkspace` closes every
terminal session — the parked popup's included — and disconnects a parked
debug session; buffers need no teardown). A **busy** workspace is skipped
and kept even over the cap (#2396): the former "Background workspace limit"
prompt is gone — a switch never asks, live state never dies for a limit, and
every subsequent switch re-runs the sweep so the set shrinks back the moment
the busy workspaces go idle. The quit guard at exit remains the single
prompt about unsaved buffers and running processes.
Per-project layout/session persistence needs
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
a right-pinned **`⏏`** (#1418, `project.CloseAuxGlyph` — distinct from the
removal `✕` on unloaded rows): `shift+delete` **or `cmd+backspace`** (#1418,
the chord that needs no physical forward-delete key) on the selected row, or
a click on the `⏏` zone, emits `project.CloseWorkspaceMsg`, which tears the background workspace
down (`teardownWorkspace`) without switching — the palette stays open and
refreshes, the badge disappears, the history entry remains. The active
project refuses the action with an info toast. Manual close is the explicit
counterpart to LRU eviction.

## Busy-close & quit guards (#821)

Tearing down a workspace with live state asks first
(`internal/app/workspace_guard.go`):

- **Close-from-list** on a busy background workspace — running debug
  session, runs/tools, running shells (popup-terminal sessions included,
  #1407), or dirty buffers
  (`collectActivity`, the detailed sibling of `workspaceBusy`) — opens a
  prompt summarising what is running: `s` saves the workspace's dirty
  buffers then closes (writes work without focus or rendering; a failed
  write cancels), `d` closes discarding, `esc` cancels untouched. `enter`
  confirms the primary option — `s` when buffers are dirty, otherwise `d`
  (#1356).
- **IDE quit** aggregates dirty buffers and running debug/run/tool activity
  across **every** in-memory workspace (active + parked, entries labeled
  with their project root). Idle interactive shells never gate the quit —
  every session has one open. `s` saves everywhere then quits, `d` quits
  discarding, `esc` cancels; `enter` confirms the primary option — `s` when
  anything is dirty, otherwise `d` (#1356).
- **LRU eviction** keeps its own #780 guard (busy workspaces prompt, never
  evict silently).

Once the quit is confirmed, `quit()` (`internal/app/app.go`) tears live
resources down instead of letting them die with the process (#1546): the
active workspace's pane and tab terminal sessions close, the active debug
session gets `Disconnect` (the only `terminateDebuggee: true` call — DAP
adapters start detached via setsid and would survive IKE as orphans,
debuggee included) followed by `Close`, parked workspaces run the full
`teardownWorkspace` (terminals, popup tabs, debug session, DBGp listener),
and the `EventAppQuit` plugin hooks run synchronously before the exit
command — the LSP bridge uses that seam to `Shutdown()` every server
through the spec's shutdown/exit handshake.

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
audit. The switch also severs the parking workspace from the old model's
services (#1549): `detachWorkspaceServices` nils every editor's emitter and
breakpoint/mark/history/MRU hooks, so a parked workspace does not pin the
stopped watcher, the dead nav history or stale store pointers (and a save
while parked cannot write into stores nobody reads); resuming re-wires the
fresh model's services through `wireEditorEmitters`. Running scans stop at
the same point: `performSwitch` cancels the find-in-path and todo-index
`search.Service` scans, so no rg child keeps walking the old tree into the
shared `host.Send`.
