---
type: concept
title: Project Switching
description: Roadmap 0090 — internal/project owns the recent-projects history data layer today (entry type, path validation, upsert/persist via config); the picker and switch orchestration are upcoming sub-issues.
resource: internal/project
tags: [architecture, project, history, switching]
timestamp: 2026-07-10T00:00:00Z
---

# Project Switching (Roadmap 0090)

`internal/project` will own the "Switch Project" flow (spec: epic #37). Landed
so far is the **data layer** (#2); the palette picker (#12) and the switch
orchestration msgs (#3) build on it.

## Recent-projects history

- **Entry** (`entry.go`): `Path` (absolute, cleaned), `Name` (display name,
  default: base directory name), `LastOpened` (orders the list). Persisted as
  `[[project.history]]` with `last_opened` in RFC3339 UTC — the shape is fixed
  by `config.ProjectHistoryEntry`; the semantics live here.
- **Validation** (`validate.go`): `Validate(path)` expands `~`, resolves to an
  absolute cleaned path and rejects non-existent paths, non-directories and
  unlistable directories with actionable errors. Nothing is mutated on failure —
  a switch never happens partially.
- **Content rules** (`history.go`): on every successful open the root is
  upserted — moved (or added) to the front, deduped by path, capped at
  `project.max_history`. The finished list is handed to config's typed setter
  (`WriteKey`, list semantics: replace). History persists to the **user** layer:
  the list spans projects on this machine, so the project layer `DefaultScope`
  would pick is wrong for it.
- **Record on success only**: `RecordOpen` runs at startup (`cmd/ike/main.go`,
  before the model loads config — the initial open counts as an open) and, once
  #3 lands, after a completed switch. A failed validation returns the error and
  leaves the stored history untouched. `RecordOpenCmd` wraps it as a `tea.Cmd`
  so the Update loop never blocks on the stat or the write.
