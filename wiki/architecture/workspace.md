---
type: concept
title: Workspace
description: Per-project UI state unit (pane registry, split tree, terminal return-focus) behind a Manager — the Roadmap 0370 seam for seamless project switching.
resource: internal/workspace
tags: [architecture, workspace, project-switching, panes, layout]
timestamp: 2026-07-20T00:00:00Z
---

# Workspace

Roadmap 0370 (#776, M1). `internal/workspace` bundles the per-project UI
state the root model owns into one swappable unit:

- **`Workspace`** — `Root` (absolute project root; `""` in M1, where the
  process cwd is the root by convention), `Panes` (the `pane.Registry`
  backing every layout leaf), `Tree` (the pure split-tree layout), and
  `ReturnFocus` (the pane focused before `terminal.toggle` / a tool command
  moved focus).
- **`Manager`** — holds the **active** workspace (`Active`/`SetActive`).
  M1 is single-workspace by design; the type exists so every call site is
  already manager-shaped when M2 (#777) adds the background workspace map,
  the LRU cap and the seamless-switch orchestration.

## Root-model integration

`internal/app`'s `Model` no longer carries `panes`/`tree`/
`terminalReturnFocus` fields — it holds `ws *workspace.Manager` and reaches
the unit exclusively through `m.activeWS()` (`app.go`). Because the model is
copied by value on every bubbletea `Update`, the manager pointer is the seam
that keeps panes, tree and focus one shared unit across copies; a later
project switch swaps the whole workspace atomically instead of rebuilding
fields one by one.

`performSwitch` (Roadmap 0090) still rebuilds the model through the
fresh-start path — the fresh model gets a fresh manager. M2 replaces that
teardown with parking the old workspace in the manager's background set so
terminals, runs and debug sessions keep running.

## Boundaries

Everything not per-project (theme, registry, host, config options, overlay
models) stays on the root model. Per-project state that is *derived* (config
project layer, watcher root, LSP clients) is re-resolved on switch and is
not part of the unit — see the epic spec (#775) for the M2/M3 ownership
audit.
